package tools

import (
	"context"
	"errors"

	"ququchat/internal/taskservice/task/agent/toolruntime"
)

const maxAgentDepth = 1

// SubAgentRunner is the function signature for running a subagent.
// Injected to avoid circular imports.
type SubAgentRunner func(ctx context.Context, goal string) (string, error)

type subAgentTool struct {
	depth  int
	runner SubAgentRunner
}

// NewSubAgentTool creates a subagent tool. depth is the current agent's depth.
// runner is agent.Execute bound with the parent's client and inherited input.
func NewSubAgentTool(depth int, runner SubAgentRunner) toolruntime.Tool {
	return &subAgentTool{depth: depth, runner: runner}
}

func (t *subAgentTool) Name() string { return "subagent" }

func (t *subAgentTool) Spec() toolruntime.ToolDescriptor {
	return toolruntime.ToolDescriptor{
		Name:        "subagent",
		Description: "启动一个子 agent 完成独立子任务，只关心结果，不关心过程。适合可以并行或独立完成的子任务。",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"goal": map[string]any{"type": "string", "description": "子任务目标，需完整描述，子 agent 没有父 agent 的上下文"},
			},
			"required":             []string{"goal"},
			"additionalProperties": false,
		},
	}
}

func (t *subAgentTool) Validate(input string) *toolruntime.ValidationError {
	if t.depth >= maxAgentDepth {
		return &toolruntime.ValidationError{Message: "subagent 不允许嵌套：当前已是子 agent"}
	}
	args, err := toolruntime.ParseActionInputJSONObject(input)
	if err != nil {
		return &toolruntime.ValidationError{Message: "action.input 必须是 JSON 对象", Detail: err.Error()}
	}
	if toolruntime.ReadStringArg(args, "goal") == "" {
		return &toolruntime.ValidationError{Message: "subagent 需要 input.goal"}
	}
	return nil
}

func (t *subAgentTool) Run(ctx context.Context, input string, _ string) (string, error) {
	if t.depth >= maxAgentDepth {
		return "", errors.New("subagent nesting not allowed")
	}
	args, _ := toolruntime.ParseActionInputJSONObject(input)
	goal := toolruntime.ReadStringArg(args, "goal")
	return t.runner(ctx, goal)
}
