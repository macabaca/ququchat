package tools

import (
	"context"
	"time"

	"ququchat/internal/taskservice/task/agent/toolruntime"
)

type getCurrentTimeTool struct{}

func NewGetCurrentTimeTool() toolruntime.Tool { return &getCurrentTimeTool{} }

func (t *getCurrentTimeTool) Name() string { return "get_current_time" }

func (t *getCurrentTimeTool) Spec() toolruntime.ToolDescriptor {
	return toolruntime.ToolDescriptor{
		Name:        "get_current_time",
		Description: "获取当前系统时间",
		Parameters: map[string]any{
			"type":                 "object",
			"properties":           map[string]any{},
			"additionalProperties": false,
		},
	}
}

func (t *getCurrentTimeTool) Validate(_ string) *toolruntime.ValidationError { return nil }

func (t *getCurrentTimeTool) Run(_ context.Context, _ string, _ string) (string, error) {
	return time.Now().Format(time.RFC3339), nil
}
