package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type plannerTask struct {
	Task string `json:"task"`
	Tool string `json:"tool"`
}

type plannerOutline struct {
	Steps []plannerTask `json:"steps"`
}

func generatePlannerOutline(ctx context.Context, client ChatClient, goal string, recentMessages []string, maxSteps int, reason string, currentTask string) ([]plannerTask, error) {
	feedback := ""
	for attempt := 1; attempt <= roleRetryLimit; attempt++ {
		raw, err := client.Chat(ctx, buildPlannerPrompt(goal, recentMessages, maxSteps, reason, currentTask, feedback))
		if err != nil {
			return nil, err
		}
		candidateRaw := strings.TrimSpace(raw)
		formatterPrompt := buildPlannerJSONFormatterPrompt(raw)
		formatterRaw, formatterErr := client.Chat(ctx, formatterPrompt)
		if formatterErr == nil && strings.TrimSpace(formatterRaw) != "" {
			candidateRaw = strings.TrimSpace(formatterRaw)
		}
		steps, _, issues := validatePlannerOutlineOutput(candidateRaw, maxSteps)
		if len(issues) == 0 {
			return steps, nil
		}
		feedback = buildPlannerValidationRetryFeedback(feedback, issues)
		if attempt == roleRetryLimit {
			return nil, fmt.Errorf("planner输出格式连续校验失败: %s", joinValidationIssueTexts(issues, "；"))
		}
	}
	return nil, errors.New("planner输出未通过规则校验")
}

func buildPlannerPrompt(goal string, recentMessages []string, maxSteps int, reason string, currentTask string, feedback string) string {
	builder := strings.Builder{}
	builder.WriteString("你是初始规划器（Planner）。请先规划一组可执行的小任务步骤，只输出JSON。\n")
	builder.WriteString("目标：")
	builder.WriteString(strings.TrimSpace(goal))
	builder.WriteString("\n")
	if strings.TrimSpace(reason) != "" {
		builder.WriteString("重规划原因：")
		builder.WriteString(strings.TrimSpace(reason))
		builder.WriteString("\n")
	}
	if strings.TrimSpace(currentTask) != "" {
		builder.WriteString("当前卡住的小任务：")
		builder.WriteString(strings.TrimSpace(currentTask))
		builder.WriteString("\n")
	}
	builder.WriteString("最近消息（节选）：\n")
	builder.WriteString(buildRecentMessagesSnippet(recentMessages, 8))
	builder.WriteString("\n")
	builder.WriteString("约束：\n")
	builder.WriteString("- 仅使用允许工具：")
	builder.WriteString(allowedToolNamesCSV())
	builder.WriteString("。\n")
	builder.WriteString("- steps 至少1步，最多")
	builder.WriteString(strconv.Itoa(maxSteps))
	builder.WriteString("步。\n")
	builder.WriteString("- 每步都要有task和tool。\n")
	if strings.TrimSpace(feedback) != "" {
		builder.WriteString("上一轮反馈：")
		builder.WriteString(strings.TrimSpace(feedback))
		builder.WriteString("\n")
	}
	builder.WriteString("输出格式：\n")
	builder.WriteString("{\"steps\":[{\"task\":\"读取最近上下文\",\"tool\":\"read_recent_messages\"},{\"task\":\"检索相关历史片段\",\"tool\":\"search_rag\"}]}\n")
	return builder.String()
}

func buildPlannerJSONFormatterPrompt(rawOutput string) string {
	builder := strings.Builder{}
	builder.WriteString("你是JSONFormatter。你的任务是把输入内容转换成严格JSON对象，只输出JSON。\n")
	builder.WriteString("必须遵循的schema:\n")
	builder.WriteString(plannerOutlineSchemaTemplateText())
	builder.WriteString("\n")
	builder.WriteString("要求:\n")
	builder.WriteString("- 只输出一个JSON对象，不允许markdown代码块，不允许解释文字。\n")
	builder.WriteString("- 保留原始语义，不要增加无关信息。\n")
	builder.WriteString("- steps 是数组，数组元素包含 task/tool 两个字符串字段。\n")
	builder.WriteString("- tool 必须是允许工具名之一。\n")
	builder.WriteString("待规范化输入:\n")
	builder.WriteString(strings.TrimSpace(rawOutput))
	builder.WriteString("\n")
	return builder.String()
}

func plannerOutlineSchemaTemplateText() string {
	builder := strings.Builder{}
	builder.WriteString("{\"steps\":[{\"task\":\"string\",\"tool\":\"")
	builder.WriteString(allowedToolNamesCSV())
	builder.WriteString("\"}]}")
	return builder.String()
}

func buildPlannerValidationRetryFeedback(baseFeedback string, issues []validationIssue) string {
	builder := strings.Builder{}
	if strings.TrimSpace(baseFeedback) != "" {
		builder.WriteString(strings.TrimSpace(baseFeedback))
		builder.WriteString("\n")
	}
	builder.WriteString("上一轮Planner规则校验未通过，错误如下：")
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
	builder.WriteString(plannerOutlineSchemaTemplateText())
	return builder.String()
}

func validatePlannerOutlineOutput(raw string, maxSteps int) ([]plannerTask, string, []validationIssue) {
	objectText, objectIssue := extractJSONObjectText(raw)
	if objectIssue != nil {
		return nil, "", []validationIssue{*objectIssue}
	}
	var root map[string]interface{}
	if err := json.Unmarshal([]byte(objectText), &root); err != nil {
		return nil, "", []validationIssue{
			{
				Code:    errCodeJSONInvalid,
				Field:   "$",
				Message: "JSON解析失败",
				Detail:  err.Error(),
			},
		}
	}
	rawSteps, ok := root["steps"]
	if !ok {
		return nil, "", []validationIssue{
			{
				Code:    errCodeFieldMissing,
				Field:   "steps",
				Message: "缺少必填字段",
				Detail:  "steps",
			},
		}
	}
	stepArr, ok := rawSteps.([]interface{})
	if !ok {
		return nil, "", []validationIssue{
			{
				Code:    errCodeFieldType,
				Field:   "steps",
				Message: "字段类型错误",
				Detail:  "期望array",
			},
		}
	}
	if len(stepArr) == 0 {
		return nil, "", []validationIssue{
			{
				Code:    errCodeFieldMissing,
				Field:   "steps",
				Message: "有效步骤为空",
				Detail:  "请至少提供一条有效步骤",
			},
		}
	}
	issues := make([]validationIssue, 0)
	steps := make([]plannerTask, 0, len(stepArr))
	for i, item := range stepArr {
		stepObj, ok := item.(map[string]interface{})
		if !ok {
			issues = append(issues, validationIssue{
				Code:    errCodeFieldType,
				Field:   "steps[" + strconv.Itoa(i) + "]",
				Message: "字段类型错误",
				Detail:  "期望object",
			})
			continue
		}
		task, taskOK := stepObj["task"].(string)
		if !taskOK || strings.TrimSpace(task) == "" {
			issues = append(issues, validationIssue{
				Code:    errCodeFieldMissing,
				Field:   "steps[" + strconv.Itoa(i) + "].task",
				Message: "缺少必填字段",
				Detail:  "task",
			})
		}
		toolRaw, toolOK := stepObj["tool"].(string)
		if !toolOK || strings.TrimSpace(toolRaw) == "" {
			issues = append(issues, validationIssue{
				Code:    errCodeFieldMissing,
				Field:   "steps[" + strconv.Itoa(i) + "].tool",
				Message: "缺少必填字段",
				Detail:  "tool",
			})
			continue
		}
		tool := normalizeToolFromConfig(toolRaw)
		if strings.TrimSpace(tool) == "" || !isKnownToolName(tool) {
			issues = append(issues, validationIssue{
				Code:    errCodeToolNotAllowed,
				Field:   "steps[" + strconv.Itoa(i) + "].tool",
				Message: "工具不在允许列表",
				Detail:  "允许值: " + allowedToolNamesCSV(),
			})
			continue
		}
		if strings.TrimSpace(task) != "" {
			steps = append(steps, plannerTask{
				Task: strings.TrimSpace(task),
				Tool: strings.TrimSpace(tool),
			})
		}
	}
	normalized := normalizePlannerOutline(steps, maxSteps)
	if len(normalized) == 0 {
		issues = append(issues, validationIssue{
			Code:    errCodeFieldMissing,
			Field:   "steps",
			Message: "有效步骤为空",
			Detail:  "请至少提供一条有效步骤",
		})
	}
	if len(issues) > 0 {
		return nil, "", issues
	}
	outline := plannerOutline{Steps: normalized}
	b, marshalErr := json.Marshal(outline)
	if marshalErr != nil {
		return nil, "", []validationIssue{
			{
				Code:    errCodeNormalizeFailed,
				Field:   "$",
				Message: "输出规范化失败",
				Detail:  marshalErr.Error(),
			},
		}
	}
	return normalized, string(b), nil
}

func normalizePlannerOutline(raw []plannerTask, maxSteps int) []plannerTask {
	if maxSteps <= 0 {
		maxSteps = maxStepsDefault
	}
	steps := make([]plannerTask, 0, len(raw))
	for _, item := range raw {
		task := strings.TrimSpace(item.Task)
		tool := normalizeToolFromConfig(item.Tool)
		if task == "" || tool == "" || !isKnownToolName(tool) {
			continue
		}
		steps = append(steps, plannerTask{
			Task: task,
			Tool: tool,
		})
		if len(steps) >= maxSteps {
			break
		}
	}
	if len(steps) == 0 {
		return []plannerTask{
			{
				Task: "读取最近消息补充上下文",
				Tool: "read_recent_messages",
			},
		}
	}
	return steps
}

func currentPlannerTask(outline []plannerTask, idx int) string {
	if len(outline) == 0 {
		return ""
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= len(outline) {
		idx = len(outline) - 1
	}
	item := outline[idx]
	if strings.TrimSpace(item.Task) == "" {
		return ""
	}
	builder := strings.Builder{}
	builder.WriteString(strings.TrimSpace(item.Task))
	if strings.TrimSpace(item.Tool) != "" {
		builder.WriteString("（建议工具：")
		builder.WriteString(strings.TrimSpace(item.Tool))
		builder.WriteString("）")
	}
	return builder.String()
}

func formatPlannerOutline(outline []plannerTask, currentIdx int) string {
	if len(outline) == 0 {
		return "1. （空）"
	}
	builder := strings.Builder{}
	for i, item := range outline {
		builder.WriteString(strconv.Itoa(i + 1))
		builder.WriteString(". ")
		if i == currentIdx {
			builder.WriteString("[当前] ")
		}
		builder.WriteString(strings.TrimSpace(item.Task))
		if strings.TrimSpace(item.Tool) != "" {
			builder.WriteString("（建议工具：")
			builder.WriteString(strings.TrimSpace(item.Tool))
			builder.WriteString("）")
		}
		builder.WriteString("\n")
	}
	return strings.TrimSpace(builder.String())
}
