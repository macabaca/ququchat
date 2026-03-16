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

type criticDecision struct {
	Verdict     string `json:"verdict"`
	Feedback    string `json:"feedback"`
	FinalAnswer string `json:"final_answer"`
	ErrorReport string `json:"error_report"`
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

const (
	maxStepsDefault = 4
	maxStepsLimit   = 10
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
		plannerRaw, err := client.Chat(ctx, buildPlannerPrompt(goal, recentMessages, feedback, step, maxSteps))
		if err != nil {
			return "", errors.New(formatFailureReport(logs, fmt.Sprintf("planner调用失败: %v", err)))
		}
		nextPlan, err := parsePlan(plannerRaw)
		if err != nil {
			return "", errors.New(formatFailureReport(logs, fmt.Sprintf("planner输出无法解析: %v", err)))
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
		criticRaw, err := client.Chat(ctx, buildCriticPrompt(goal, nextPlan, toolOutput, feedback, step, maxSteps))
		if err != nil {
			return "", errors.New(formatFailureReport(logs, fmt.Sprintf("critic调用失败: %v", err)))
		}
		decision, err := parseCriticDecision(criticRaw)
		if err != nil {
			return "", errors.New(formatFailureReport(logs, fmt.Sprintf("critic输出无法解析: %v", err)))
		}
		verdict := strings.ToLower(strings.TrimSpace(decision.Verdict))
		if verdict == "finish" {
			finalAnswer := strings.TrimSpace(decision.FinalAnswer)
			if finalAnswer == "" {
				finalAnswer = strings.TrimSpace(toolOutput)
			}
			if finalAnswer == "" {
				finalAnswer = strings.TrimSpace(decision.Feedback)
			}
			if finalAnswer == "" {
				return "", errors.New(formatFailureReport(logs, "critic判定finish但未提供结果"))
			}
			return formatSuccessReport(logs, finalAnswer), nil
		}
		if verdict == "fail" {
			errorReport := strings.TrimSpace(decision.ErrorReport)
			if errorReport == "" {
				errorReport = strings.TrimSpace(decision.Feedback)
			}
			if errorReport == "" {
				errorReport = "critic判定任务失败"
			}
			return "", errors.New(formatFailureReport(logs, errorReport))
		}
		feedback = strings.TrimSpace(decision.Feedback)
		if feedback == "" {
			feedback = strings.TrimSpace(toolOutput)
		}
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
	builder.WriteString("1) read_recent_messages: 读取最近消息。input可留空或填写数字。\n")
	builder.WriteString("2) finish: 结束任务并给出最终答案。写入action.final。\n")
	builder.WriteString("当前步数：")
	builder.WriteString(strconv.Itoa(step))
	builder.WriteString("/")
	builder.WriteString(strconv.Itoa(maxSteps))
	builder.WriteString("\n")
	if strings.TrimSpace(feedback) != "" {
		builder.WriteString("上一轮Critic反馈：")
		builder.WriteString(strings.TrimSpace(feedback))
		builder.WriteString("\n")
	}
	builder.WriteString("最近消息条数：")
	builder.WriteString(strconv.Itoa(len(recentMessages)))
	builder.WriteString("\n")
	builder.WriteString("输出格式:\n")
	builder.WriteString("{\"thought\":\"...\",\"action\":{\"tool\":\"read_recent_messages|finish\",\"input\":\"...\",\"final\":\"...\"}}\n")
	return builder.String()
}

func buildCriticPrompt(goal string, plan plan, toolOutput string, feedback string, step int, maxSteps int) string {
	builder := strings.Builder{}
	builder.WriteString("你是Critic。基于Planner动作和工具结果做判定，只输出JSON。\n")
	builder.WriteString("目标：")
	builder.WriteString(goal)
	builder.WriteString("\n")
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
	builder.WriteString("Planner动作：")
	builder.WriteString(strings.TrimSpace(plan.Action.Tool))
	builder.WriteString("\n")
	builder.WriteString("工具输出：")
	builder.WriteString(strings.TrimSpace(toolOutput))
	builder.WriteString("\n")
	builder.WriteString("输出格式:\n")
	builder.WriteString("{\"verdict\":\"continue|finish|fail\",\"feedback\":\"...\",\"final_answer\":\"...\",\"error_report\":\"...\"}\n")
	return builder.String()
}

func parsePlan(raw string) (plan, error) {
	var nextPlan plan
	if err := decodeJSONFromText(raw, &nextPlan); err != nil {
		return plan{}, err
	}
	if strings.TrimSpace(nextPlan.Action.Tool) == "" {
		return plan{}, errors.New("missing action.tool")
	}
	return nextPlan, nil
}

func parseCriticDecision(raw string) (criticDecision, error) {
	var decision criticDecision
	if err := decodeJSONFromText(raw, &decision); err != nil {
		return criticDecision{}, err
	}
	if strings.TrimSpace(decision.Verdict) == "" {
		return criticDecision{}, errors.New("missing verdict")
	}
	return decision, nil
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

func runTool(toolName string, input string, recentMessages []string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(toolName)) {
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
		return "", fmt.Errorf("unsupported tool: %s", strings.TrimSpace(toolName))
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
