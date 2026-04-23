package tools

import (
	"context"

	"ququchat/internal/taskservice/task/agent/toolruntime"
)

type replanTool struct{}

func NewReplanTool() toolruntime.Tool { return &replanTool{} }

func (t *replanTool) Name() string { return "replan" }

func (t *replanTool) Spec() toolruntime.ToolDescriptor {
	return toolruntime.ToolDescriptor{
		Name:        "replan",
		Description: "重新规划后续小任务",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"reason": map[string]any{"type": "string", "description": "重规划原因"},
			},
			"additionalProperties": false,
		},
	}
}

func (t *replanTool) Validate(input string) *toolruntime.ValidationError {
	if input == "" {
		return nil
	}
	args, err := toolruntime.ParseActionInputJSONObject(input)
	if err != nil {
		return &toolruntime.ValidationError{Message: "action.input 必须是 JSON 对象字符串", Detail: err.Error()}
	}
	if toolruntime.HasKey(args, "reason") {
		if _, ok := args["reason"].(string); !ok {
			return &toolruntime.ValidationError{Message: "replan 的 input.reason 必须是字符串", Detail: "示例: {\"reason\":\"当前方案命中率低\"}"}
		}
	}
	return nil
}

func (t *replanTool) Run(_ context.Context, _ string, _ string) (string, error) { return "", nil }

type finishTool struct{}

func NewFinishTool() toolruntime.Tool { return &finishTool{} }

func (t *finishTool) Name() string { return "finish" }

func (t *finishTool) Spec() toolruntime.ToolDescriptor {
	return toolruntime.ToolDescriptor{
		Name:        "finish",
		Description: "结束任务并输出最终答案",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"final": map[string]any{"type": "string", "description": "最终答案正文"},
			},
			"required":             []string{"final"},
			"additionalProperties": false,
		},
	}
}

func (t *finishTool) Validate(input string) *toolruntime.ValidationError {
	args, err := toolruntime.ParseActionInputJSONObject(input)
	if err != nil {
		return &toolruntime.ValidationError{Message: "action.input 必须是 JSON 对象字符串", Detail: err.Error()}
	}
	if toolruntime.ReadStringArg(args, "final") == "" {
		return &toolruntime.ValidationError{Message: "finish 需要 input.final", Detail: "示例: {\"final\":\"最终答案正文\"}"}
	}
	return nil
}

func (t *finishTool) Run(_ context.Context, _ string, _ string) (string, error) { return "", nil }
