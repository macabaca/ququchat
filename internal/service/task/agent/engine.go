package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type ChatClient interface {
	Chat(ctx context.Context, prompt string) (string, error)
}

type RAGSearchTool func(ctx context.Context, roomID string, query string, topK int, vector string) (string, error)
type AIGCTool func(ctx context.Context, prompt string) (string, error)

type Input struct {
	Goal           string
	RecentMessages []string
	MaxSteps       int
	RoomID         string
	RAGSearch      RAGSearchTool
	AIGCGenerate   AIGCTool
}

type plan struct {
	Thought string `json:"thought"`
	Action  action `json:"action"`
}

type action struct {
	Tool  string `json:"tool"`
	Input string `json:"input"`
	Final string `json:"final"`
}

type toolCallRecord struct {
	Step   int
	Role   string
	Tool   string
	Input  string
	Output string
	Status string
	Error  string
}

type validationIssue struct {
	Code    string
	Field   string
	Message string
	Detail  string
}

type finalReviewResult struct {
	Pass        bool     `json:"pass"`
	Score       int      `json:"score"`
	Issues      []string `json:"issues"`
	BetterFinal string   `json:"better_final"`
}

const (
	maxStepsDefault = 4
	maxStepsLimit   = 10
	roleRetryLimit  = 2
	finalScorePass  = 70
	ragSearchTopK   = 3
	ragSearchVector = "summary"
)

const (
	errCodeJSONEmpty          = "E_JSON_EMPTY"
	errCodeJSONNotFound       = "E_JSON_NOT_FOUND"
	errCodeJSONInvalid        = "E_JSON_INVALID"
	errCodeFieldMissing       = "E_FIELD_MISSING"
	errCodeFieldType          = "E_FIELD_TYPE"
	errCodeToolCombination    = "E_TOOL_COMBINATION"
	errCodeToolNotAllowed     = "E_TOOL_NOT_ALLOWED"
	errCodeFinishFinalMissing = "E_FINISH_FINAL_MISSING"
	errCodeNormalizeFailed    = "E_NORMALIZE_FAILED"
)

const (
	errDescJSONEmpty          = "输出内容为空，必须返回一个完整JSON对象。"
	errDescJSONNotFound       = "输出中没有可解析的JSON对象，请去掉额外说明或markdown。"
	errDescJSONInvalid        = "JSON结构不合法，通常是引号、逗号或括号不匹配。"
	errDescFieldMissing       = "缺少必填字段，请补齐schema要求的字段。"
	errDescFieldType          = "字段类型错误，请按schema要求改成正确类型。"
	errDescToolCombination    = "工具字段只能填一个工具名，不能写多个工具组合。"
	errDescToolNotAllowed     = "工具名不在允许列表中，请改成支持的工具名。"
	errDescFinishFinalMissing = "当tool=finish时，final不能为空，必须给出最终答案文本。"
	errDescNormalizeFailed    = "规范化输出失败，请先保证输出是可解析且字段完整的JSON。"
)

var validationIssueCodeDescMap = map[string]string{
	errCodeJSONEmpty:          errDescJSONEmpty,
	errCodeJSONNotFound:       errDescJSONNotFound,
	errCodeJSONInvalid:        errDescJSONInvalid,
	errCodeFieldMissing:       errDescFieldMissing,
	errCodeFieldType:          errDescFieldType,
	errCodeToolCombination:    errDescToolCombination,
	errCodeToolNotAllowed:     errDescToolNotAllowed,
	errCodeFinishFinalMissing: errDescFinishFinalMissing,
	errCodeNormalizeFailed:    errDescNormalizeFailed,
}

func Execute(ctx context.Context, client ChatClient, input Input) (string, error) {
	if client == nil {
		return "", errors.New("llm client is not configured")
	}
	goal := strings.TrimSpace(input.Goal)
	if goal == "" {
		return "", errors.New("agent goal is required")
	}
	recentMessages := make([]string, 0, len(input.RecentMessages))
	for _, line := range input.RecentMessages {
		if strings.TrimSpace(line) != "" {
			recentMessages = append(recentMessages, strings.TrimSpace(line))
		}
	}
	maxSteps := input.MaxSteps
	if maxSteps <= 0 {
		maxSteps = maxStepsDefault
	}
	if maxSteps > maxStepsLimit {
		maxSteps = maxStepsLimit
	}
	logs := make([]toolCallRecord, 0)
	feedback := ""
	outline, outlineErr := generatePlannerOutline(ctx, client, goal, recentMessages, maxSteps, "", "")
	if outlineErr != nil {
		return "", errors.New(formatFailureReport(logs, fmt.Sprintf("planner生成初始计划失败: %v", outlineErr)))
	}
	logs = append(logs, toolCallRecord{
		Step:   0,
		Role:   "Planner",
		Tool:   "generate_execution_outline",
		Status: "succeeded",
		Output: shortText(formatPlannerOutline(outline, 0), 220),
	})
	outlineIndex := 0
	for step := 1; step <= maxSteps; step++ {
		currentTask := currentPlannerTask(outline, outlineIndex)
		nextPlan := plan{}
		coordinatorFeedback := strings.TrimSpace(feedback)
		coordinatorValidated := false
		for attempt := 1; attempt <= roleRetryLimit; attempt++ {
			currentCoordinatorPrompt := buildCoordinatorPrompt(goal, recentMessages, coordinatorFeedback, step, maxSteps, formatPlannerOutline(outline, outlineIndex), currentTask)
			nextCoordinatorRaw, err := client.Chat(ctx, currentCoordinatorPrompt)
			if err != nil {
				return "", errors.New(formatFailureReport(logs, fmt.Sprintf("coordinator调用失败: %v", err)))
			}
			candidateRaw := strings.TrimSpace(nextCoordinatorRaw)
			formatterPrompt := buildJSONFormatterPrompt(nextCoordinatorRaw)
			formatterRaw, formatterErr := client.Chat(ctx, formatterPrompt)
			formatterRecord := toolCallRecord{
				Step:   step,
				Role:   "Formatter",
				Tool:   "normalize_coordinator_output",
				Input:  shortText(nextCoordinatorRaw, 220),
				Output: shortText(formatterRaw, 220),
			}
			if formatterErr != nil {
				formatterRecord.Status = "failed"
				formatterRecord.Error = formatterErr.Error()
				logs = append(logs, formatterRecord)
			} else {
				formatterRecord.Status = "succeeded"
				logs = append(logs, formatterRecord)
				if strings.TrimSpace(formatterRaw) != "" {
					candidateRaw = strings.TrimSpace(formatterRaw)
				}
			}
			normalizedPlan, normalizedJSON, validationIssues := validateCoordinatorOutput(candidateRaw)
			validateRecord := toolCallRecord{
				Step:   step,
				Role:   "Validator",
				Tool:   "validate_coordinator_output",
				Input:  shortText(candidateRaw, 220),
				Output: shortText(normalizedJSON, 220),
			}
			if len(validationIssues) == 0 {
				validateRecord.Status = "succeeded"
				if strings.TrimSpace(validateRecord.Output) == "" {
					validateRecord.Output = "valid"
				}
				logs = append(logs, validateRecord)
				nextPlan = normalizedPlan
				coordinatorValidated = true
				break
			}
			validateRecord.Status = "failed"
			validateRecord.Error = joinValidationIssueTexts(validationIssues, "；")
			logs = append(logs, validateRecord)
			coordinatorFeedback = buildValidationRetryFeedback(feedback, validationIssues)
			if attempt == roleRetryLimit {
				return "", errors.New(formatFailureReport(logs, "coordinator输出格式连续校验失败"))
			}
		}
		if !coordinatorValidated {
			return "", errors.New(formatFailureReport(logs, "coordinator输出未通过规则校验"))
		}
		toolName := strings.ToLower(strings.TrimSpace(nextPlan.Action.Tool))
		actionInput := strings.TrimSpace(nextPlan.Action.Input)
		if toolName == "replan" {
			reason := strings.TrimSpace(actionInput)
			if reason == "" {
				reason = strings.TrimSpace(nextPlan.Thought)
			}
			nextOutline, planErr := generatePlannerOutline(ctx, client, goal, recentMessages, maxSteps, reason, currentTask)
			record := toolCallRecord{
				Step:  step,
				Role:  "Planner",
				Tool:  "generate_execution_outline",
				Input: reason,
			}
			if planErr != nil {
				record.Status = "failed"
				record.Error = planErr.Error()
				logs = append(logs, record)
				feedback = "上一轮工具调用 replan。重规划失败，继续当前小任务。失败原因：" + strings.TrimSpace(planErr.Error())
				continue
			}
			record.Status = "succeeded"
			record.Output = shortText(formatPlannerOutline(nextOutline, 0), 220)
			logs = append(logs, record)
			outline = nextOutline
			outlineIndex = 0
			feedback = "上一轮工具调用 replan。已生成新计划，后续请按新的小任务列表推进。"
			continue
		}
		if toolName == "finish" {
			finalAnswer := strings.TrimSpace(nextPlan.Action.Final)
			if finalAnswer == "" {
				finalAnswer = actionInput
			}
			if finalAnswer == "" {
				finalAnswer = strings.TrimSpace(nextPlan.Thought)
			}
			if finalAnswer == "" {
				return "", errors.New(formatFailureReport(logs, "coordinator选择finish但未提供结果"))
			}
			review, reviewErr := evaluateFinalAnswer(ctx, client, goal, recentMessages, logs, finalAnswer)
			reviewRecord := toolCallRecord{
				Step:   step,
				Role:   "FinalJudge",
				Tool:   "evaluate_final_answer",
				Input:  shortText(finalAnswer, 220),
				Output: shortText(formatFinalReviewOutput(review), 220),
			}
			if reviewErr != nil {
				reviewRecord.Status = "failed"
				reviewRecord.Error = reviewErr.Error()
				logs = append(logs, reviewRecord)
			} else {
				if review.Pass && review.Score >= finalScorePass {
					reviewRecord.Status = "succeeded"
					logs = append(logs, reviewRecord)
					finalCandidate := strings.TrimSpace(review.BetterFinal)
					if finalCandidate == "" {
						finalCandidate = finalAnswer
					}
					return formatSuccessReport(logs, finalCandidate), nil
				}
				reviewRecord.Status = "failed"
				reviewRecord.Error = buildFinalReviewErrorText(review)
				logs = append(logs, reviewRecord)
			}
			if step < maxSteps {
				feedback = buildFinalRetryFeedback(goal, finalAnswer, review)
				continue
			}
			synthesized, synthErr := synthesizeFinalAnswer(ctx, client, goal, recentMessages, logs, finalAnswer)
			synthRecord := toolCallRecord{
				Step:   step,
				Role:   "FinalSynthesizer",
				Tool:   "synthesize_final_answer",
				Input:  shortText(finalAnswer, 220),
				Output: shortText(synthesized, 220),
			}
			if synthErr != nil {
				synthRecord.Status = "failed"
				synthRecord.Error = synthErr.Error()
				logs = append(logs, synthRecord)
				return "", errors.New(formatFailureReport(logs, "最终答案质量不足且兜底总结失败"))
			}
			synthRecord.Status = "succeeded"
			logs = append(logs, synthRecord)
			if strings.TrimSpace(synthesized) == "" {
				return "", errors.New(formatFailureReport(logs, "最终答案质量不足且兜底总结为空"))
			}
			return formatSuccessReport(logs, strings.TrimSpace(synthesized)), nil
		}
		toolOutput, toolErr := runTool(
			ctx,
			toolName,
			actionInput,
			recentMessages,
			strings.TrimSpace(input.RoomID),
			input.RAGSearch,
			input.AIGCGenerate,
		)
		record := toolCallRecord{
			Step:  step,
			Role:  "Executor",
			Tool:  toolName,
			Input: actionInput,
		}
		if toolErr != nil {
			record.Status = "failed"
			record.Error = toolErr.Error()
			logs = append(logs, record)
			feedback = "上一轮工具调用 " + strings.TrimSpace(toolName) + " 失败。请优先继续当前小任务。错误信息：" + strings.TrimSpace(toolErr.Error())
			continue
		}
		record.Status = "succeeded"
		record.Output = toolOutput
		logs = append(logs, record)
		if outlineIndex < len(outline)-1 {
			outlineIndex++
		}
		feedback = buildToolFeedback(toolName, actionInput, toolOutput)
	}
	return "", errors.New(formatFailureReport(logs, "达到最大循环次数仍未完成"))
}

func buildCoordinatorPrompt(goal string, recentMessages []string, feedback string, step int, maxSteps int, outlineText string, currentTask string) string {
	builder := strings.Builder{}
	builder.WriteString("你是执行协调器（Coordinator）。请为目标选择下一步动作，只输出JSON。\n")
	builder.WriteString(buildAgentIdentityPrompt())
	builder.WriteString("目标：")
	builder.WriteString(goal)
	builder.WriteString("\n")
	builder.WriteString("可用工具:\n")
	builder.WriteString(buildCoordinatorToolSection())
	builder.WriteString("规则:\n")
	for _, line := range coordinatorPromptRuleLines() {
		builder.WriteString("- ")
		builder.WriteString(strings.TrimSpace(line))
		builder.WriteString("\n")
	}
	builder.WriteString("当前步数：")
	builder.WriteString(strconv.Itoa(step))
	builder.WriteString("/")
	builder.WriteString(strconv.Itoa(maxSteps))
	builder.WriteString("\n")
	if strings.TrimSpace(outlineText) != "" {
		builder.WriteString("规划小任务列表：\n")
		builder.WriteString(strings.TrimSpace(outlineText))
		builder.WriteString("\n")
	}
	if strings.TrimSpace(currentTask) != "" {
		builder.WriteString("当前优先小任务：")
		builder.WriteString(strings.TrimSpace(currentTask))
		builder.WriteString("\n")
	}
	if strings.TrimSpace(feedback) != "" {
		builder.WriteString("上一轮反馈：")
		builder.WriteString(strings.TrimSpace(feedback))
		builder.WriteString("\n")
	}
	builder.WriteString("最近消息条数：")
	builder.WriteString(strconv.Itoa(len(recentMessages)))
	builder.WriteString("\n")
	builder.WriteString("输出示例1:\n")
	builder.WriteString("{\"thought\":\"先查看上下文\",\"action\":{\"tool\":\"read_recent_messages\",\"input\":\"8\",\"final\":\"\"}}\n")
	builder.WriteString("输出示例2:\n")
	builder.WriteString("{\"thought\":\"信息足够，直接给答案\",\"action\":{\"tool\":\"finish\",\"input\":\"\",\"final\":\"这是最终答案\"}}\n")
	builder.WriteString("输出示例3:\n")
	builder.WriteString("{\"thought\":\"当前路径效果差，先重规划\",\"action\":{\"tool\":\"replan\",\"input\":\"已有方案命中率低，换检索路径\",\"final\":\"\"}}\n")
	builder.WriteString("输出示例4:\n")
	builder.WriteString("{\"thought\":\"用户要文生图，先执行生成\",\"action\":{\"tool\":\"generate_image\",\"input\":\"一只戴墨镜的柴犬，电影感\",\"final\":\"\"}}\n")
	return builder.String()
}

func buildJSONFormatterPrompt(rawOutput string) string {
	builder := strings.Builder{}
	builder.WriteString("你是JSONFormatter。你的任务是把输入内容转换成严格JSON对象，只输出JSON。\n")
	builder.WriteString("必须遵循的schema:\n")
	builder.WriteString(coordinatorSchemaTemplateText())
	builder.WriteString("\n")
	builder.WriteString("要求:\n")
	builder.WriteString("- 只输出一个JSON对象，不允许markdown代码块，不允许解释文字。\n")
	builder.WriteString("- 保留原始语义，不要增加无关信息。\n")
	builder.WriteString("- 如果tool出现别名，归一化到标准工具名。\n")
	builder.WriteString("- 如果action.tool=finish，action.final 必须非空。\n")
	builder.WriteString("待规范化输入:\n")
	builder.WriteString(strings.TrimSpace(rawOutput))
	builder.WriteString("\n")
	return builder.String()
}

func buildValidationRetryFeedback(baseFeedback string, issues []validationIssue) string {
	builder := strings.Builder{}
	if strings.TrimSpace(baseFeedback) != "" {
		builder.WriteString(strings.TrimSpace(baseFeedback))
		builder.WriteString("\n")
	}
	builder.WriteString("上一轮规则校验未通过，错误如下：")
	if len(issues) == 0 {
		builder.WriteString("未知格式错误")
	} else {
		builder.WriteString("\n")
		for i, issue := range issues {
			builder.WriteString(strconv.Itoa(i + 1))
			builder.WriteString(") ")
			builder.WriteString(validationIssueText(issue))
			builder.WriteString("\n")
		}
		explainLines := buildValidationIssueExplanations(issues)
		if len(explainLines) > 0 {
			builder.WriteString("错误码解释：\n")
			for i, line := range explainLines {
				builder.WriteString(strconv.Itoa(i + 1))
				builder.WriteString(") ")
				builder.WriteString(line)
				builder.WriteString("\n")
			}
		}
	}
	builder.WriteString("请严格按JSON schema重新输出。schema: ")
	builder.WriteString(coordinatorSchemaTemplateText())
	return builder.String()
}

func buildValidationIssueExplanations(issues []validationIssue) []string {
	if len(issues) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(issues))
	lines := make([]string, 0, len(issues))
	for _, issue := range issues {
		code := strings.TrimSpace(issue.Code)
		if code == "" {
			continue
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		desc := validationIssueCodeDescription(code)
		if strings.TrimSpace(desc) == "" {
			continue
		}
		lines = append(lines, "["+code+"] "+desc)
	}
	return lines
}

func validationIssueCodeDescription(code string) string {
	return validationIssueCodeDescMap[strings.TrimSpace(code)]
}

func buildFinalRetryFeedback(goal string, candidate string, review finalReviewResult) string {
	builder := strings.Builder{}
	builder.WriteString("上一轮给出的final与目标对齐度不足，需要重新规划。\n")
	builder.WriteString("目标：")
	builder.WriteString(strings.TrimSpace(goal))
	builder.WriteString("\n")
	builder.WriteString("候选final：")
	builder.WriteString(strings.TrimSpace(candidate))
	builder.WriteString("\n")
	builder.WriteString("质量评估：")
	builder.WriteString(buildFinalReviewErrorText(review))
	builder.WriteString("\n")
	builder.WriteString("请先读取足够上下文，再给出直接满足目标的final，不要给泛化客套回复。")
	return builder.String()
}

func buildFinalJudgePrompt(goal string, recentMessages []string, logs []toolCallRecord, candidate string) string {
	builder := strings.Builder{}
	builder.WriteString("你是FinalJudge。请评估候选final是否真正满足目标，只输出JSON。\n")
	builder.WriteString("目标：")
	builder.WriteString(strings.TrimSpace(goal))
	builder.WriteString("\n")
	builder.WriteString("候选final：")
	builder.WriteString(strings.TrimSpace(candidate))
	builder.WriteString("\n")
	builder.WriteString("最近消息（节选）：\n")
	builder.WriteString(buildRecentMessagesSnippet(recentMessages, 10))
	builder.WriteString("\n")
	builder.WriteString("执行过程（节选）：\n")
	builder.WriteString(buildLogSnippet(logs, 8))
	builder.WriteString("\n")
	builder.WriteString("评估规则:\n")
	builder.WriteString("- 如果final直接响应目标、信息充分、无明显跑题，则pass=true。\n")
	builder.WriteString("- 如果final是泛化寒暄、反问用户、未完成任务，则pass=false。\n")
	builder.WriteString("- score范围0-100。\n")
	builder.WriteString("- issues给出具体问题列表。\n")
	builder.WriteString("- better_final在可改进时给出更好的最终答案文本。\n")
	builder.WriteString("输出格式:\n")
	builder.WriteString("{\"pass\":true|false,\"score\":0,\"issues\":[\"...\"],\"better_final\":\"...\"}")
	return builder.String()
}

func buildFinalSynthesizerPrompt(goal string, recentMessages []string, logs []toolCallRecord, candidate string) string {
	builder := strings.Builder{}
	builder.WriteString("你是FinalSynthesizer。请基于目标与上下文直接生成最终答案。\n")
	builder.WriteString("目标：")
	builder.WriteString(strings.TrimSpace(goal))
	builder.WriteString("\n")
	builder.WriteString("当前候选final：")
	builder.WriteString(strings.TrimSpace(candidate))
	builder.WriteString("\n")
	builder.WriteString("最近消息（节选）：\n")
	builder.WriteString(buildRecentMessagesSnippet(recentMessages, 12))
	builder.WriteString("\n")
	builder.WriteString("执行过程（节选）：\n")
	builder.WriteString(buildLogSnippet(logs, 10))
	builder.WriteString("\n")
	builder.WriteString("要求:\n")
	builder.WriteString("- 直接回答目标，不要反问用户。\n")
	builder.WriteString("- 语言清晰、具体、可执行。\n")
	builder.WriteString("- 只输出最终答案正文，不要JSON。\n")
	return builder.String()
}

func evaluateFinalAnswer(ctx context.Context, client ChatClient, goal string, recentMessages []string, logs []toolCallRecord, candidate string) (finalReviewResult, error) {
	raw, err := client.Chat(ctx, buildFinalJudgePrompt(goal, recentMessages, logs, candidate))
	if err != nil {
		return finalReviewResult{}, err
	}
	var review finalReviewResult
	if err := decodeJSONFromText(raw, &review); err != nil {
		return finalReviewResult{}, fmt.Errorf("final评估结果不可解析: %w", err)
	}
	if review.Score < 0 {
		review.Score = 0
	}
	if review.Score > 100 {
		review.Score = 100
	}
	if !review.Pass && len(review.Issues) == 0 {
		review.Issues = []string{"未通过但未提供原因"}
	}
	return review, nil
}

func synthesizeFinalAnswer(ctx context.Context, client ChatClient, goal string, recentMessages []string, logs []toolCallRecord, candidate string) (string, error) {
	raw, err := client.Chat(ctx, buildFinalSynthesizerPrompt(goal, recentMessages, logs, candidate))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(raw), nil
}

func validateCoordinatorOutput(raw string) (plan, string, []validationIssue) {
	cfg := getCoordinatorSchemaConfig()
	candidate, extractIssue := extractJSONObjectText(raw)
	if extractIssue != nil {
		return plan{}, "", []validationIssue{*extractIssue}
	}
	var root map[string]interface{}
	if err := json.Unmarshal([]byte(candidate), &root); err != nil {
		return plan{}, "", []validationIssue{
			{
				Code:    errCodeJSONInvalid,
				Field:   "$",
				Message: "输出不是合法JSON对象",
				Detail:  err.Error(),
			},
		}
	}
	issues := make([]validationIssue, 0)
	_, topLevelIssues := validateObjectAgainstSchema(root, cfg.TopLevelFields, "")
	issues = append(issues, topLevelIssues...)
	toolRaw := ""
	input := ""
	final := ""
	thought := readStringValue(root, cfg.ThoughtField)
	actionValue, hasAction := root[cfg.ActionField]
	actionMap, actionOK := actionValue.(map[string]interface{})
	if hasAction && actionOK {
		_, actionIssues := validateObjectAgainstSchema(actionMap, cfg.ActionFields, cfg.ActionField)
		issues = append(issues, actionIssues...)
		toolRaw = readStringValue(actionMap, cfg.ToolField)
		input = readStringValue(actionMap, cfg.InputField)
		final = readStringValue(actionMap, cfg.FinalField)
	}
	toolName := normalizeToolFromConfig(toolRaw)
	if strings.TrimSpace(toolRaw) != "" && cfg.DisallowToolCombination && containsToolCombination(toolRaw) {
		issues = append(issues, validationIssue{
			Code:    errCodeToolCombination,
			Field:   cfg.ActionField + "." + cfg.ToolField,
			Message: "工具名不能是组合值",
			Detail:  "仅允许单个工具名，例如 read_recent_messages 或 finish",
		})
	}
	if cfg.ToolEnumFromConfig && (strings.TrimSpace(toolName) == "" || !isKnownToolName(toolName)) {
		issues = append(issues, validationIssue{
			Code:    errCodeToolNotAllowed,
			Field:   cfg.ActionField + "." + cfg.ToolField,
			Message: "工具不在允许列表",
			Detail:  "允许值: " + allowedToolNamesCSV(),
		})
	}
	if strings.TrimSpace(cfg.RequireFinalWhenToolName) != "" &&
		strings.EqualFold(strings.TrimSpace(toolName), strings.TrimSpace(cfg.RequireFinalWhenToolName)) &&
		strings.TrimSpace(final) == "" {
		issues = append(issues, validationIssue{
			Code:    errCodeFinishFinalMissing,
			Field:   cfg.ActionField + "." + cfg.FinalField,
			Message: cfg.ActionField + "." + cfg.ToolField + "=" + cfg.RequireFinalWhenToolName + " 时，" + cfg.ActionField + "." + cfg.FinalField + " 必须非空",
			Detail:  "请提供最终答案文本",
		})
	}
	if len(issues) > 0 {
		return plan{}, "", issues
	}
	normalizedPlan := plan{
		Thought: strings.TrimSpace(thought),
		Action: action{
			Tool:  strings.TrimSpace(toolName),
			Input: strings.TrimSpace(input),
			Final: strings.TrimSpace(final),
		},
	}
	b, marshalErr := json.Marshal(normalizedPlan)
	if marshalErr != nil {
		return plan{}, "", []validationIssue{
			{
				Code:    errCodeNormalizeFailed,
				Field:   "$",
				Message: "输出规范化失败",
				Detail:  marshalErr.Error(),
			},
		}
	}
	return normalizedPlan, string(b), nil
}

func decodeJSONFromText(raw string, v interface{}) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return errors.New("empty output")
	}
	if err := json.Unmarshal([]byte(trimmed), v); err == nil {
		return nil
	}
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start >= 0 && end > start {
		candidate := strings.TrimSpace(trimmed[start : end+1])
		if err := json.Unmarshal([]byte(candidate), v); err == nil {
			return nil
		}
	}
	return errors.New("json not found")
}

func extractJSONObjectText(raw string) (string, *validationIssue) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", &validationIssue{
			Code:    errCodeJSONEmpty,
			Field:   "$",
			Message: "输出为空",
			Detail:  "请输出一个JSON对象",
		}
	}
	if json.Valid([]byte(trimmed)) {
		return trimmed, nil
	}
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start >= 0 && end > start {
		candidate := strings.TrimSpace(trimmed[start : end+1])
		if json.Valid([]byte(candidate)) {
			return candidate, nil
		}
	}
	return "", &validationIssue{
		Code:    errCodeJSONNotFound,
		Field:   "$",
		Message: "未找到合法JSON对象",
		Detail:  "请只输出JSON对象，不要包含markdown代码块或额外解释",
	}
}

func readStringField(m map[string]interface{}, key string) (string, bool) {
	value, ok := m[key]
	if !ok {
		return "", false
	}
	text, ok := value.(string)
	if !ok {
		return "", false
	}
	return text, true
}

func readStringValue(m map[string]interface{}, key string) string {
	text, ok := readStringField(m, key)
	if !ok {
		return ""
	}
	return text
}

func validateObjectAgainstSchema(root map[string]interface{}, fields []SchemaField, parent string) (map[string]interface{}, []validationIssue) {
	values := make(map[string]interface{}, len(fields))
	issues := make([]validationIssue, 0)
	for _, field := range fields {
		name := strings.TrimSpace(field.Name)
		if name == "" {
			continue
		}
		fullName := name
		if strings.TrimSpace(parent) != "" {
			fullName = strings.TrimSpace(parent) + "." + name
		}
		value, exists := root[name]
		if !exists {
			if field.Required {
				issues = append(issues, validationIssue{
					Code:    errCodeFieldMissing,
					Field:   fullName,
					Message: "字段缺失",
					Detail:  "该字段为必填项",
				})
			}
			continue
		}
		if !matchesSchemaType(value, field.Type) {
			issues = append(issues, validationIssue{
				Code:    errCodeFieldType,
				Field:   fullName,
				Message: "字段类型错误，期望 " + strings.ToLower(strings.TrimSpace(field.Type)),
				Detail:  "实际值类型: " + jsonValueTypeName(value),
			})
			continue
		}
		values[name] = value
	}
	return values, issues
}

func validationIssueText(issue validationIssue) string {
	code := strings.TrimSpace(issue.Code)
	if code == "" {
		code = "E_UNKNOWN"
	}
	field := strings.TrimSpace(issue.Field)
	msg := strings.TrimSpace(issue.Message)
	detail := strings.TrimSpace(issue.Detail)
	builder := strings.Builder{}
	builder.WriteString("[")
	builder.WriteString(code)
	builder.WriteString("]")
	if field != "" {
		builder.WriteString(" ")
		builder.WriteString(field)
		builder.WriteString(": ")
	} else {
		builder.WriteString(" ")
	}
	builder.WriteString(msg)
	if detail != "" {
		builder.WriteString("（")
		builder.WriteString(detail)
		builder.WriteString("）")
	}
	return builder.String()
}

func joinValidationIssueTexts(issues []validationIssue, sep string) string {
	if len(issues) == 0 {
		return ""
	}
	parts := make([]string, 0, len(issues))
	for _, issue := range issues {
		parts = append(parts, validationIssueText(issue))
	}
	return strings.Join(parts, sep)
}

func jsonValueTypeName(value interface{}) string {
	switch value.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case bool:
		return "boolean"
	case float64, float32, int, int32, int64:
		return "number"
	case map[string]interface{}:
		return "object"
	case []interface{}:
		return "array"
	default:
		return fmt.Sprintf("%T", value)
	}
}

func matchesSchemaType(value interface{}, expectedType string) bool {
	switch strings.ToLower(strings.TrimSpace(expectedType)) {
	case "string":
		_, ok := value.(string)
		return ok
	case "object":
		_, ok := value.(map[string]interface{})
		return ok
	case "array":
		_, ok := value.([]interface{})
		return ok
	case "number":
		switch value.(type) {
		case float64, float32, int, int32, int64:
			return true
		}
		return false
	case "bool", "boolean":
		_, ok := value.(bool)
		return ok
	default:
		return true
	}
}

func containsToolCombination(s string) bool {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return false
	}
	return strings.Contains(trimmed, "|") || strings.Contains(trimmed, "/") || strings.Contains(trimmed, ",") || strings.Contains(trimmed, "，") || strings.Contains(trimmed, ";") || strings.Contains(trimmed, "；")
}

func runTool(
	ctx context.Context,
	toolName string,
	input string,
	recentMessages []string,
	roomID string,
	ragSearch RAGSearchTool,
	aigcGenerate AIGCTool,
) (string, error) {
	switch normalizeToolFromConfig(toolName) {
	case "read_recent_messages":
		if len(recentMessages) == 0 {
			return "最近消息为空", nil
		}
		selected := recentMessages
		n := parsePositiveInt(input)
		if n > 0 && n < len(recentMessages) {
			selected = recentMessages[len(recentMessages)-n:]
		}
		builder := strings.Builder{}
		for i, line := range selected {
			builder.WriteString(strconv.Itoa(i + 1))
			builder.WriteString(". ")
			builder.WriteString(strings.TrimSpace(line))
			builder.WriteString("\n")
		}
		return strings.TrimSpace(builder.String()), nil
	case "search_rag":
		query := strings.TrimSpace(input)
		if query == "" {
			return "", errors.New("search_rag input is required")
		}
		if strings.TrimSpace(roomID) == "" {
			return "", errors.New("search_rag requires room_id")
		}
		if ragSearch == nil {
			return "", errors.New("search_rag is not configured")
		}
		return ragSearch(ctx, strings.TrimSpace(roomID), query, ragSearchTopK, ragSearchVector)
	case "generate_image":
		prompt := strings.TrimSpace(input)
		if prompt == "" {
			return "", errors.New("generate_image input is required")
		}
		if aigcGenerate == nil {
			return "", errors.New("generate_image is not configured")
		}
		return aigcGenerate(ctx, prompt)
	default:
		return "", fmt.Errorf("unsupported tool: %s (allowed: %s)", strings.TrimSpace(toolName), allowedToolNamesCSV())
	}
}

func parsePositiveInt(s string) int {
	parts := strings.Fields(strings.TrimSpace(s))
	for _, part := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err == nil && n > 0 {
			return n
		}
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err == nil && n > 0 {
		return n
	}
	return 0
}

func buildFinalReviewErrorText(review finalReviewResult) string {
	builder := strings.Builder{}
	builder.WriteString("score=")
	builder.WriteString(strconv.Itoa(review.Score))
	builder.WriteString(", pass=")
	if review.Pass {
		builder.WriteString("true")
	} else {
		builder.WriteString("false")
	}
	if len(review.Issues) > 0 {
		builder.WriteString(", issues=")
		builder.WriteString(strings.Join(review.Issues, "；"))
	}
	return builder.String()
}

func formatFinalReviewOutput(review finalReviewResult) string {
	builder := strings.Builder{}
	builder.WriteString("score=")
	builder.WriteString(strconv.Itoa(review.Score))
	builder.WriteString(", pass=")
	if review.Pass {
		builder.WriteString("true")
	} else {
		builder.WriteString("false")
	}
	if strings.TrimSpace(review.BetterFinal) != "" {
		builder.WriteString(", better_final=")
		builder.WriteString(shortText(strings.TrimSpace(review.BetterFinal), 120))
	}
	return builder.String()
}

func buildRecentMessagesSnippet(messages []string, max int) string {
	if len(messages) == 0 {
		return "无"
	}
	if max <= 0 {
		max = 1
	}
	selected := messages
	if len(selected) > max {
		selected = selected[len(selected)-max:]
	}
	builder := strings.Builder{}
	for i, line := range selected {
		builder.WriteString(strconv.Itoa(i + 1))
		builder.WriteString(". ")
		builder.WriteString(shortText(strings.TrimSpace(line), 140))
		builder.WriteString("\n")
	}
	return strings.TrimSpace(builder.String())
}

func buildLogSnippet(logs []toolCallRecord, max int) string {
	if len(logs) == 0 {
		return "无"
	}
	if max <= 0 {
		max = 1
	}
	selected := logs
	if len(selected) > max {
		selected = selected[len(selected)-max:]
	}
	builder := strings.Builder{}
	for i, log := range selected {
		builder.WriteString(strconv.Itoa(i + 1))
		builder.WriteString(") role=")
		builder.WriteString(strings.TrimSpace(log.Role))
		builder.WriteString(", tool=")
		builder.WriteString(strings.TrimSpace(log.Tool))
		builder.WriteString(", status=")
		builder.WriteString(strings.TrimSpace(log.Status))
		if strings.TrimSpace(log.Output) != "" {
			builder.WriteString(", output=")
			builder.WriteString(shortText(strings.TrimSpace(log.Output), 120))
		}
		if strings.TrimSpace(log.Error) != "" {
			builder.WriteString(", error=")
			builder.WriteString(shortText(strings.TrimSpace(log.Error), 120))
		}
		builder.WriteString("\n")
	}
	return strings.TrimSpace(builder.String())
}

func formatSuccessReport(logs []toolCallRecord, finalAnswer string) string {
	builder := strings.Builder{}
	builder.WriteString("工具调用记录：\n")
	builder.WriteString(formatToolLogs(logs))
	builder.WriteString("\n最终结果：\n")
	builder.WriteString(strings.TrimSpace(finalAnswer))
	return builder.String()
}

func formatFailureReport(logs []toolCallRecord, errorReport string) string {
	builder := strings.Builder{}
	builder.WriteString("工具调用记录：\n")
	builder.WriteString(formatToolLogs(logs))
	builder.WriteString("\n错误报告：\n")
	builder.WriteString(strings.TrimSpace(errorReport))
	return builder.String()
}

func formatToolLogs(logs []toolCallRecord) string {
	if len(logs) == 0 {
		return "1. 无"
	}
	builder := strings.Builder{}
	for i, log := range logs {
		builder.WriteString(strconv.Itoa(i + 1))
		builder.WriteString(". ")
		builder.WriteString("step=")
		builder.WriteString(strconv.Itoa(log.Step))
		builder.WriteString(", role=")
		builder.WriteString(strings.TrimSpace(log.Role))
		builder.WriteString(", tool=")
		builder.WriteString(strings.TrimSpace(log.Tool))
		builder.WriteString(", status=")
		builder.WriteString(strings.TrimSpace(log.Status))
		if strings.TrimSpace(log.Input) != "" {
			builder.WriteString(", input=")
			builder.WriteString(shortText(log.Input, 120))
		}
		if strings.TrimSpace(log.Output) != "" {
			builder.WriteString(", output=")
			builder.WriteString(shortText(log.Output, 160))
		}
		if strings.TrimSpace(log.Error) != "" {
			builder.WriteString(", error=")
			builder.WriteString(shortText(log.Error, 160))
		}
		builder.WriteString("\n")
	}
	return strings.TrimSpace(builder.String())
}

func shortText(s string, max int) string {
	trimmed := strings.TrimSpace(s)
	if max <= 0 {
		return ""
	}
	runes := []rune(trimmed)
	if len(runes) <= max {
		return trimmed
	}
	return strings.TrimSpace(string(runes[:max])) + "..."
}
