package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	agentmemory "ququchat/internal/taskservice/task/agent/memory"
	"ququchat/internal/taskservice/task/agent/toolruntime"
)

type ChatClient interface {
	Chat(ctx context.Context, prompt string) (string, error)
}

type RAGSearchTool func(ctx context.Context, roomID string, query string, topK int, vector string) (string, error)
type AIGCTool func(ctx context.Context, prompt string) (string, error)
type MCPCallToolByQualifiedName func(ctx context.Context, qualifiedToolName string, arguments map[string]any) (string, error)

type Input struct {
	Goal                       string
	RecentMessages             []string
	MaxSteps                   int
	RoomID                     string
	RAGSearch                  RAGSearchTool
	AIGCGenerate               AIGCTool
	DynamicToolSpecs           []ToolSpec
	MCPCallToolByQualifiedName MCPCallToolByQualifiedName
}

type plan struct {
	Thought string `json:"thought"`
	Action  action `json:"action"`
}

type action struct {
	Tool  string `json:"tool"`
	Input string `json:"input"`
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
	maxStepsDefault        = 4
	maxStepsLimit          = 10
	roleRetryLimit         = 2
	finalScorePass         = 50
	readRecentDefaultLimit = 10
	feedbackOutputMaxChars = 4000
	ragSearchTopK          = 3
	ragSearchVector        = "summary"
)

const (
	errCodeJSONEmpty        = "E_JSON_EMPTY"
	errCodeJSONNotFound     = "E_JSON_NOT_FOUND"
	errCodeJSONInvalid      = "E_JSON_INVALID"
	errCodeFieldMissing     = "E_FIELD_MISSING"
	errCodeFieldType        = "E_FIELD_TYPE"
	errCodeToolCombination  = "E_TOOL_COMBINATION"
	errCodeToolNotAllowed   = "E_TOOL_NOT_ALLOWED"
	errCodeToolInputInvalid = "E_TOOL_INPUT_INVALID"
	errCodeNormalizeFailed  = "E_NORMALIZE_FAILED"
)

const (
	errDescJSONEmpty        = "输出内容为空，必须返回一个完整JSON对象。"
	errDescJSONNotFound     = "输出中没有可解析的JSON对象，请去掉额外说明或markdown。"
	errDescJSONInvalid      = "JSON结构不合法，通常是引号、逗号或括号不匹配。"
	errDescFieldMissing     = "缺少必填字段，请补齐schema要求的字段。"
	errDescFieldType        = "字段类型错误，请按schema要求改成正确类型。"
	errDescToolCombination  = "工具字段只能填一个工具名，不能写多个工具组合。"
	errDescToolNotAllowed   = "工具名不在允许列表中，请改成支持的工具名。"
	errDescToolInputInvalid = "工具输入不符合约定，请按工具的 JSON 参数说明传值。"
	errDescNormalizeFailed  = "规范化输出失败，请先保证输出是可解析且字段完整的JSON。"
)

var validationIssueCodeDescMap = map[string]string{
	errCodeJSONEmpty:        errDescJSONEmpty,
	errCodeJSONNotFound:     errDescJSONNotFound,
	errCodeJSONInvalid:      errDescJSONInvalid,
	errCodeFieldMissing:     errDescFieldMissing,
	errCodeFieldType:        errDescFieldType,
	errCodeToolCombination:  errDescToolCombination,
	errCodeToolNotAllowed:   errDescToolNotAllowed,
	errCodeToolInputInvalid: errDescToolInputInvalid,
	errCodeNormalizeFailed:  errDescNormalizeFailed,
}

func Execute(ctx context.Context, client ChatClient, input Input) (string, error) {
	if client == nil {
		return "", errors.New("llm client is not configured")
	}
	goal := strings.TrimSpace(input.Goal)
	if goal == "" {
		return "", errors.New("agent goal is required")
	}
	recentMessages := agentmemory.NormalizeRecentMessages(input.RecentMessages)
	maxSteps := input.MaxSteps
	if maxSteps <= 0 {
		maxSteps = maxStepsDefault
	}
	if maxSteps > maxStepsLimit {
		maxSteps = maxStepsLimit
	}
	availableToolSpecs := listToolSpecsWithDynamic(input.DynamicToolSpecs)
	toolRunner := newToolRuntime(input)
	memorySession := agentmemory.NewFacade().NewSession(agentmemory.SessionInput{
		RoomID:                 strings.TrimSpace(input.RoomID),
		Goal:                   goal,
		RecentMessages:         append([]string(nil), recentMessages...),
		MaxRecent:              readRecentDefaultLimit,
		FeedbackOutputMaxChars: feedbackOutputMaxChars,
	})
	outline, outlineErr := generatePlannerOutline(ctx, client, goal, recentMessages, maxSteps, "", "", availableToolSpecs)
	if outlineErr != nil {
		return "", errors.New(agentmemory.FormatFailureReport(memorySession.Trace(), fmt.Sprintf("planner生成初始计划失败: %v", outlineErr)))
	}
	memorySession.AppendObservation(agentmemory.Observation{
		Step:   0,
		Role:   "Planner",
		Tool:   "generate_execution_outline",
		Status: "succeeded",
		Output: agentmemory.ShortText(formatPlannerOutline(outline, 0), 220),
	})
	outlineIndex := 0
	for step := 1; step <= maxSteps; step++ {
		currentTask := currentPlannerTask(outline, outlineIndex)
		nextPlan := plan{}
		coordinatorFeedback := strings.TrimSpace(memorySession.BuildFeedback())
		coordinatorValidated := false
		for attempt := 1; attempt <= roleRetryLimit; attempt++ {
			currentCoordinatorPrompt := buildCoordinatorPrompt(goal, recentMessages, coordinatorFeedback, step, maxSteps, formatPlannerOutline(outline, outlineIndex), currentTask, availableToolSpecs)
			nextCoordinatorRaw, err := client.Chat(ctx, currentCoordinatorPrompt)
			if err != nil {
				return "", errors.New(agentmemory.FormatFailureReport(memorySession.Trace(), fmt.Sprintf("coordinator调用失败: %v", err)))
			}
			candidateRaw := strings.TrimSpace(nextCoordinatorRaw)
			formatterPrompt := buildJSONFormatterPrompt(nextCoordinatorRaw, availableToolSpecs)
			formatterRaw, formatterErr := client.Chat(ctx, formatterPrompt)
			formatterRecord := agentmemory.Observation{
				Step:   step,
				Role:   "Formatter",
				Tool:   "normalize_coordinator_output",
				Input:  agentmemory.ShortText(nextCoordinatorRaw, 220),
				Output: agentmemory.ShortText(formatterRaw, 220),
			}
			if formatterErr != nil {
				formatterRecord.Status = "failed"
				formatterRecord.Error = formatterErr.Error()
				memorySession.AppendObservation(formatterRecord)
			} else {
				formatterRecord.Status = "succeeded"
				memorySession.AppendObservation(formatterRecord)
				if strings.TrimSpace(formatterRaw) != "" {
					candidateRaw = strings.TrimSpace(formatterRaw)
				}
			}
			normalizedPlan, normalizedJSON, validationIssues := validateCoordinatorOutput(candidateRaw, availableToolSpecs)
			validateRecord := agentmemory.Observation{
				Step:   step,
				Role:   "Validator",
				Tool:   "validate_coordinator_output",
				Input:  agentmemory.ShortText(candidateRaw, 220),
				Output: agentmemory.ShortText(normalizedJSON, 220),
			}
			if len(validationIssues) == 0 {
				validateRecord.Status = "succeeded"
				if strings.TrimSpace(validateRecord.Output) == "" {
					validateRecord.Output = "valid"
				}
				memorySession.AppendObservation(validateRecord)
				nextPlan = normalizedPlan
				coordinatorValidated = true
				break
			}
			validateRecord.Status = "failed"
			validateRecord.Error = joinValidationIssueTexts(validationIssues, "；")
			memorySession.AppendObservation(validateRecord)
			coordinatorFeedback = buildValidationRetryFeedback(memorySession.BuildFeedback(), validationIssues, availableToolSpecs)
			if attempt == roleRetryLimit {
				return "", errors.New(agentmemory.FormatFailureReport(memorySession.Trace(), "coordinator输出格式连续校验失败"))
			}
		}
		if !coordinatorValidated {
			return "", errors.New(agentmemory.FormatFailureReport(memorySession.Trace(), "coordinator输出未通过规则校验"))
		}
		toolName := strings.ToLower(strings.TrimSpace(nextPlan.Action.Tool))
		actionInput := strings.TrimSpace(nextPlan.Action.Input)
		if toolName == "replan" {
			reason := ""
			if args, argsErr := toolruntime.ParseActionInputJSONObject(actionInput); argsErr == nil {
				reason = toolruntime.ReadStringArg(args, "reason")
			}
			if reason == "" {
				reason = strings.TrimSpace(nextPlan.Thought)
			}
			nextOutline, planErr := generatePlannerOutline(ctx, client, goal, recentMessages, maxSteps, reason, currentTask, availableToolSpecs)
			record := agentmemory.Observation{
				Step:  step,
				Role:  "Planner",
				Tool:  "generate_execution_outline",
				Input: reason,
			}
			if planErr != nil {
				record.Status = "failed"
				record.Error = planErr.Error()
				memorySession.AppendObservation(record)
				continue
			}
			record.Status = "succeeded"
			record.Output = agentmemory.ShortText(formatPlannerOutline(nextOutline, 0), 220)
			memorySession.AppendObservation(record)
			outline = nextOutline
			outlineIndex = 0
			continue
		}
		if toolName == "finish" {
			finalAnswer := ""
			args, argsErr := toolruntime.ParseActionInputJSONObject(actionInput)
			if argsErr == nil {
				finalAnswer = toolruntime.ReadStringArg(args, "final")
			}
			if finalAnswer == "" {
				return "", errors.New(agentmemory.FormatFailureReport(memorySession.Trace(), "coordinator选择finish但未提供结果"))
			}
			review, reviewErr := evaluateFinalAnswer(ctx, client, goal, recentMessages, memorySession.Trace(), finalAnswer)
			reviewRecord := agentmemory.Observation{
				Step:   step,
				Role:   "FinalJudge",
				Tool:   "evaluate_final_answer",
				Input:  agentmemory.ShortText(finalAnswer, 220),
				Output: agentmemory.ShortText(formatFinalReviewOutput(review), 220),
			}
			if reviewErr != nil {
				reviewRecord.Status = "failed"
				reviewRecord.Error = reviewErr.Error()
				memorySession.AppendObservation(reviewRecord)
			} else {
				if review.Pass && review.Score >= finalScorePass {
					reviewRecord.Status = "succeeded"
					memorySession.AppendObservation(reviewRecord)
					finalCandidate := strings.TrimSpace(review.BetterFinal)
					if finalCandidate == "" {
						finalCandidate = finalAnswer
					}
					return agentmemory.FormatSuccessReport(memorySession.Trace(), strings.TrimSpace(finalCandidate)), nil
				}
				reviewRecord.Status = "failed"
				reviewRecord.Error = buildFinalReviewErrorText(review)
				memorySession.AppendObservation(reviewRecord)
			}
			if step < maxSteps {
				memorySession.AppendObservation(agentmemory.Observation{
					Step:   step,
					Role:   "Coordinator",
					Tool:   "final_retry_feedback",
					Status: "failed",
					Error:  buildFinalRetryFeedback(goal, finalAnswer, review),
				})
				continue
			}
			synthesized, synthErr := synthesizeFinalAnswer(ctx, client, goal, recentMessages, memorySession.Trace(), finalAnswer)
			synthRecord := agentmemory.Observation{
				Step:   step,
				Role:   "FinalSynthesizer",
				Tool:   "synthesize_final_answer",
				Input:  agentmemory.ShortText(finalAnswer, 220),
				Output: agentmemory.ShortText(synthesized, 220),
			}
			if synthErr != nil {
				synthRecord.Status = "failed"
				synthRecord.Error = synthErr.Error()
				memorySession.AppendObservation(synthRecord)
				return "", errors.New(agentmemory.FormatFailureReport(memorySession.Trace(), "最终答案质量不足且兜底总结失败"))
			}
			synthRecord.Status = "succeeded"
			memorySession.AppendObservation(synthRecord)
			if strings.TrimSpace(synthesized) == "" {
				return "", errors.New(agentmemory.FormatFailureReport(memorySession.Trace(), "最终答案质量不足且兜底总结为空"))
			}
			return agentmemory.FormatSuccessReport(memorySession.Trace(), strings.TrimSpace(synthesized)), nil
		}
		toolOutput, toolErr := toolRunner.Run(ctx, toolName, actionInput, strings.TrimSpace(input.RoomID))
		record := agentmemory.Observation{
			Step:  step,
			Role:  "Executor",
			Tool:  toolName,
			Input: actionInput,
		}
		if toolErr != nil {
			record.Status = "failed"
			record.Error = toolErr.Error()
			memorySession.AppendObservation(record)
			continue
		}
		record.Status = "succeeded"
		record.Output = toolOutput
		memorySession.AppendObservation(record)
		if outlineIndex < len(outline)-1 {
			outlineIndex++
		}
	}
	return "", errors.New(agentmemory.FormatFailureReport(memorySession.Trace(), "达到最大循环次数仍未完成"))
}

func buildCoordinatorPrompt(goal string, recentMessages []string, feedback string, step int, maxSteps int, outlineText string, currentTask string, specs []ToolSpec) string {
	builder := strings.Builder{}
	builder.WriteString("你是执行协调器（Coordinator）。请为目标选择下一步动作，只输出JSON。\n")
	builder.WriteString("思考与表达要求：即使工具描述或参数为英文，你也必须始终使用中文进行思考与输出；thought 字段必须是中文。\n")
	builder.WriteString(buildRealtimePlanningGuidance(specs))
	builder.WriteString(buildAgentIdentityPrompt())
	builder.WriteString("目标：")
	builder.WriteString(goal)
	builder.WriteString("\n")
	builder.WriteString("可用工具:\n")
	builder.WriteString(buildCoordinatorToolSectionFromSpecs(specs))
	builder.WriteString("规则:\n")
	for _, line := range coordinatorPromptRuleLinesFromSpecs(specs) {
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
	builder.WriteString("{\"thought\":\"先检索相关历史消息\",\"action\":{\"tool\":\"search_rag\",\"input\":\"{\\\"query\\\":\\\"用户当前问题关键词\\\"}\"}}\n")
	builder.WriteString("输出示例2:\n")
	builder.WriteString("{\"thought\":\"信息足够，直接给答案\",\"action\":{\"tool\":\"finish\",\"input\":\"{\\\"final\\\":\\\"这是最终答案\\\"}\"}}\n")
	builder.WriteString("输出示例3:\n")
	builder.WriteString("{\"thought\":\"当前路径效果差，先重规划\",\"action\":{\"tool\":\"replan\",\"input\":\"{\\\"reason\\\":\\\"已有方案命中率低，换检索路径\\\"}\"}}\n")
	builder.WriteString("输出示例4:\n")
	builder.WriteString("{\"thought\":\"用户要文生图，先执行生成\",\"action\":{\"tool\":\"generate_image\",\"input\":\"{\\\"prompt\\\":\\\"一只戴墨镜的柴犬，电影感\\\"}\"}}\n")
	return builder.String()
}

func newToolRuntime(input Input) toolruntime.Runtime {
	return toolruntime.New(
		toolruntime.Config{
			RAGSearchTopK:   ragSearchTopK,
			RAGSearchVector: ragSearchVector,
		},
		toolruntime.Deps{
			RAGSearch:              toolruntime.RAGSearchFunc(input.RAGSearch),
			AIGCGenerate:           toolruntime.AIGCGenerateFunc(input.AIGCGenerate),
			MCPCallByQualifiedName: toolruntime.MCPCallFunc(input.MCPCallToolByQualifiedName),
		},
	)
}

func buildRealtimePlanningGuidance(specs []ToolSpec) string {
	now := time.Now().Format("2006-01-02 15:04:05 -07:00 MST")
	builder := strings.Builder{}
	builder.WriteString("时间基线：当前系统时间是 ")
	builder.WriteString(now)
	builder.WriteString("。\n")
	builder.WriteString("认知边界：你仅在 2025 年之前的数据上训练，这不代表当前时间停留在 2025 年前。\n")
	if hasTavilyTool(specs) {
		builder.WriteString("决策要求：若请求涉及即时信息、最新事件或实时数据，先调用 tavily 相关工具检索，再基于检索结果思考与回答。\n")
	} else {
		builder.WriteString("决策要求：若请求涉及即时信息、最新事件或实时数据，先调用可用联网检索工具（如 tavily 相关工具）检索，再基于检索结果思考与回答。\n")
	}
	return builder.String()
}

func hasTavilyTool(specs []ToolSpec) bool {
	for _, spec := range specs {
		if strings.Contains(strings.ToLower(strings.TrimSpace(spec.Name)), "tavily") {
			return true
		}
		for _, alias := range spec.Aliases {
			if strings.Contains(strings.ToLower(strings.TrimSpace(alias)), "tavily") {
				return true
			}
		}
	}
	return false
}

func buildJSONFormatterPrompt(rawOutput string, specs []ToolSpec) string {
	builder := strings.Builder{}
	builder.WriteString("你是JSONFormatter。你的任务是把输入内容转换成严格JSON对象，只输出JSON。\n")
	builder.WriteString("必须遵循的schema:\n")
	builder.WriteString(coordinatorSchemaTemplateTextFromSpecs(specs))
	builder.WriteString("\n")
	builder.WriteString("要求:\n")
	builder.WriteString("- 只输出一个JSON对象，不允许markdown代码块，不允许解释文字。\n")
	builder.WriteString("- 保留原始语义，不要增加无关信息。\n")
	builder.WriteString("- 如果tool出现别名，归一化到标准工具名。\n")
	builder.WriteString("- action.input 必须是 JSON 对象字符串，本地工具参数放在该 JSON 对象内。\n")
	builder.WriteString("待规范化输入:\n")
	builder.WriteString(strings.TrimSpace(rawOutput))
	builder.WriteString("\n")
	return builder.String()
}

func buildValidationRetryFeedback(baseFeedback string, issues []validationIssue, specs []ToolSpec) string {
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
	builder.WriteString(coordinatorSchemaTemplateTextFromSpecs(specs))
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
	builder.WriteString("上一轮final疑似跑题，需要重新规划。\n")
	builder.WriteString("目标：")
	builder.WriteString(strings.TrimSpace(goal))
	builder.WriteString("\n")
	builder.WriteString("候选final：")
	builder.WriteString(strings.TrimSpace(candidate))
	builder.WriteString("\n")
	builder.WriteString("质量评估：")
	builder.WriteString(buildFinalReviewErrorText(review))
	builder.WriteString("\n")
	builder.WriteString("请仅检查是否跑题。若不跑题，下一步直接调用 finish。")
	return builder.String()
}

func buildFinalJudgePrompt(goal string, recentMessages []string, trace []agentmemory.Observation, candidate string) string {
	builder := strings.Builder{}
	builder.WriteString("你是FinalJudge。请只评估候选final是否跑题，只输出JSON。\n")
	builder.WriteString("目标：")
	builder.WriteString(strings.TrimSpace(goal))
	builder.WriteString("\n")
	builder.WriteString("候选final：")
	builder.WriteString(strings.TrimSpace(candidate))
	builder.WriteString("\n")
	builder.WriteString("最近消息（节选）：\n")
	builder.WriteString(agentmemory.BuildRecentMessagesSnippet(recentMessages, 10))
	builder.WriteString("\n")
	builder.WriteString("执行过程（节选）：\n")
	builder.WriteString(agentmemory.BuildTraceSnippet(trace, 8))
	builder.WriteString("\n")
	builder.WriteString("评估规则:\n")
	builder.WriteString("- 你只能判断“是否跑题”，不能判断事实正确性、年份时间是否准确、信息是否真实、是否满足目标。\n")
	builder.WriteString("- 即使内容可能有事实错误，只要主题相关且不是闲聊/反问/答非所问，就应判定 pass=true。\n")
	builder.WriteString("- 只有明显跑题、泛化寒暄、纯反问用户、与目标主题无关时，才判定 pass=false。\n")
	builder.WriteString("- score范围0-100。\n")
	builder.WriteString("- 分数语义：与主题强相关可给70-100，疑似偏题给40-69，明显跑题给0-39。\n")
	builder.WriteString("- issues只描述“跑题相关问题”，不得包含事实真伪或时间判断。\n")
	builder.WriteString("- better_final 固定输出空字符串。\n")
	builder.WriteString("输出格式:\n")
	builder.WriteString("{\"pass\":true|false,\"score\":0,\"issues\":[\"...\"],\"better_final\":\"...\"}")
	return builder.String()
}

func buildFinalSynthesizerPrompt(goal string, recentMessages []string, trace []agentmemory.Observation, candidate string) string {
	builder := strings.Builder{}
	builder.WriteString("你是FinalSynthesizer。请基于目标与上下文直接生成最终答案。\n")
	builder.WriteString("目标：")
	builder.WriteString(strings.TrimSpace(goal))
	builder.WriteString("\n")
	builder.WriteString("当前候选final：")
	builder.WriteString(strings.TrimSpace(candidate))
	builder.WriteString("\n")
	builder.WriteString("最近消息（节选）：\n")
	builder.WriteString(agentmemory.BuildRecentMessagesSnippet(recentMessages, 12))
	builder.WriteString("\n")
	builder.WriteString("执行过程（节选）：\n")
	builder.WriteString(agentmemory.BuildTraceSnippet(trace, 10))
	builder.WriteString("\n")
	builder.WriteString("要求:\n")
	builder.WriteString("- 直接回答目标，不要反问用户。\n")
	builder.WriteString("- 必须使用中文输出最终答案；即使工具返回英文内容，也要用中文总结与表达。\n")
	builder.WriteString("- 语言清晰、具体、可执行。\n")
	builder.WriteString("- 只输出最终答案正文，不要JSON。\n")
	return builder.String()
}

func evaluateFinalAnswer(ctx context.Context, client ChatClient, goal string, recentMessages []string, trace []agentmemory.Observation, candidate string) (finalReviewResult, error) {
	raw, err := client.Chat(ctx, buildFinalJudgePrompt(goal, recentMessages, trace, candidate))
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

func synthesizeFinalAnswer(ctx context.Context, client ChatClient, goal string, recentMessages []string, trace []agentmemory.Observation, candidate string) (string, error) {
	raw, err := client.Chat(ctx, buildFinalSynthesizerPrompt(goal, recentMessages, trace, candidate))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(raw), nil
}

func validateCoordinatorOutput(raw string, specs []ToolSpec) (plan, string, []validationIssue) {
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
	thought := readStringValue(root, cfg.ThoughtField)
	actionValue, hasAction := root[cfg.ActionField]
	actionMap, actionOK := actionValue.(map[string]interface{})
	if hasAction && actionOK {
		_, actionIssues := validateObjectAgainstSchema(actionMap, cfg.ActionFields, cfg.ActionField)
		issues = append(issues, actionIssues...)
		toolRaw = readStringValue(actionMap, cfg.ToolField)
		input = readStringValue(actionMap, cfg.InputField)
	}
	toolName := normalizeToolFromSpecs(specs, toolRaw)
	if strings.TrimSpace(toolRaw) != "" && cfg.DisallowToolCombination && containsToolCombination(toolRaw) {
		issues = append(issues, validationIssue{
			Code:    errCodeToolCombination,
			Field:   cfg.ActionField + "." + cfg.ToolField,
			Message: "工具名不能是组合值",
			Detail:  "仅允许单个工具名，例如 search_rag 或 finish",
		})
	}
	if cfg.ToolEnumFromConfig && (strings.TrimSpace(toolName) == "" || !isKnownToolNameFromSpecs(specs, toolName)) {
		issues = append(issues, validationIssue{
			Code:    errCodeToolNotAllowed,
			Field:   cfg.ActionField + "." + cfg.ToolField,
			Message: "工具不在允许列表",
			Detail:  "允许值: " + allowedToolNamesCSVFromSpecs(specs),
		})
	}
	toolValidator := toolruntime.New(toolruntime.Config{}, toolruntime.Deps{})
	if issue := toolValidator.Validate(toolName, input); issue != nil {
		issues = append(issues, validationIssue{
			Code:    errCodeToolInputInvalid,
			Field:   cfg.ActionField + "." + cfg.InputField,
			Message: issue.Message,
			Detail:  issue.Detail,
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
		builder.WriteString(agentmemory.ShortText(strings.TrimSpace(review.BetterFinal), 120))
	}
	return builder.String()
}
