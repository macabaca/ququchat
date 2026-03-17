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

type Input struct {
	Goal           string
	RecentMessages []string
	MaxSteps       int
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

const (
	maxStepsDefault = 4
	maxStepsLimit   = 10
	roleRetryLimit  = 2
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
	for step := 1; step <= maxSteps; step++ {
		nextPlan := plan{}
		plannerFeedback := strings.TrimSpace(feedback)
		plannerValidated := false
		for attempt := 1; attempt <= roleRetryLimit; attempt++ {
			currentPlannerPrompt := buildPlannerPrompt(goal, recentMessages, plannerFeedback, step, maxSteps)
			nextPlannerRaw, err := client.Chat(ctx, currentPlannerPrompt)
			if err != nil {
				return "", errors.New(formatFailureReport(logs, fmt.Sprintf("planner调用失败: %v", err)))
			}
			candidateRaw := strings.TrimSpace(nextPlannerRaw)
			formatterPrompt := buildJSONFormatterPrompt(nextPlannerRaw)
			formatterRaw, formatterErr := client.Chat(ctx, formatterPrompt)
			formatterRecord := toolCallRecord{
				Step:   step,
				Role:   "Formatter",
				Tool:   "normalize_planner_output",
				Input:  shortText(nextPlannerRaw, 220),
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
			normalizedPlan, normalizedJSON, validationIssues := validatePlannerOutput(candidateRaw)
			validateRecord := toolCallRecord{
				Step:   step,
				Role:   "Validator",
				Tool:   "validate_planner_output",
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
				plannerValidated = true
				break
			}
			validateRecord.Status = "failed"
			validateRecord.Error = joinValidationIssueTexts(validationIssues, "；")
			logs = append(logs, validateRecord)
			plannerFeedback = buildValidationRetryFeedback(feedback, validationIssues)
			if attempt == roleRetryLimit {
				return "", errors.New(formatFailureReport(logs, "planner输出格式连续校验失败"))
			}
		}
		if !plannerValidated {
			return "", errors.New(formatFailureReport(logs, "planner输出未通过规则校验"))
		}
		toolName := strings.ToLower(strings.TrimSpace(nextPlan.Action.Tool))
		actionInput := strings.TrimSpace(nextPlan.Action.Input)
		if toolName == "finish" {
			finalAnswer := strings.TrimSpace(nextPlan.Action.Final)
			if finalAnswer == "" {
				finalAnswer = actionInput
			}
			if finalAnswer == "" {
				finalAnswer = strings.TrimSpace(nextPlan.Thought)
			}
			if finalAnswer == "" {
				return "", errors.New(formatFailureReport(logs, "planner选择finish但未提供结果"))
			}
			return formatSuccessReport(logs, finalAnswer), nil
		}
		toolOutput, toolErr := runTool(toolName, actionInput, recentMessages)
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
			return "", errors.New(formatFailureReport(logs, fmt.Sprintf("工具执行失败: %v", toolErr)))
		}
		record.Status = "succeeded"
		record.Output = toolOutput
		logs = append(logs, record)
		feedback = "上一轮工具输出：" + strings.TrimSpace(toolOutput)
	}
	return "", errors.New(formatFailureReport(logs, "达到最大循环次数仍未完成"))
}

func buildPlannerPrompt(goal string, recentMessages []string, feedback string, step int, maxSteps int) string {
	builder := strings.Builder{}
	builder.WriteString("你是Planner。请为目标选择下一步动作，只输出JSON。\n")
	builder.WriteString("目标：")
	builder.WriteString(goal)
	builder.WriteString("\n")
	builder.WriteString("可用工具:\n")
	builder.WriteString(buildPlannerToolSection())
	builder.WriteString("规则:\n")
	for _, line := range plannerPromptRuleLines() {
		builder.WriteString("- ")
		builder.WriteString(strings.TrimSpace(line))
		builder.WriteString("\n")
	}
	builder.WriteString("当前步数：")
	builder.WriteString(strconv.Itoa(step))
	builder.WriteString("/")
	builder.WriteString(strconv.Itoa(maxSteps))
	builder.WriteString("\n")
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
	return builder.String()
}

func buildJSONFormatterPrompt(rawOutput string) string {
	builder := strings.Builder{}
	builder.WriteString("你是JSONFormatter。你的任务是把输入内容转换成严格JSON对象，只输出JSON。\n")
	builder.WriteString("必须遵循的schema:\n")
	builder.WriteString(plannerSchemaTemplateText())
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
	}
	builder.WriteString("请严格按JSON schema重新输出。schema: ")
	builder.WriteString(plannerSchemaTemplateText())
	return builder.String()
}

func validatePlannerOutput(raw string) (plan, string, []validationIssue) {
	cfg := getPlannerSchemaConfig()
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

func runTool(toolName string, input string, recentMessages []string) (string, error) {
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
