package tools

import (
	"context"
	"errors"
	"strings"

	"ququchat/internal/taskservice/task/agent/toolruntime"
)

type ragSearchTool struct {
	cfg  toolruntime.Config
	deps toolruntime.Deps
}

func NewRAGSearchTool(cfg toolruntime.Config, deps toolruntime.Deps) toolruntime.Tool {
	return &ragSearchTool{cfg: cfg, deps: deps}
}

func (t *ragSearchTool) Name() string { return "search_rag" }

func (t *ragSearchTool) Spec() toolruntime.ToolDescriptor {
	return toolruntime.ToolDescriptor{
		Name:        "search_rag",
		Description: "检索历史消息记忆（历史消息）",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string", "description": "检索关键词"},
			},
			"required":             []string{"query"},
			"additionalProperties": false,
		},
	}
}

func (t *ragSearchTool) Validate(input string) *toolruntime.ValidationError {
	args, err := toolruntime.ParseActionInputJSONObject(input)
	if err != nil {
		return &toolruntime.ValidationError{Message: "action.input 必须是 JSON 对象字符串", Detail: err.Error()}
	}
	if toolruntime.ReadStringArg(args, "query") == "" {
		return &toolruntime.ValidationError{Message: "search_rag 需要 input.query", Detail: "示例: {\"query\":\"检索关键词\"}"}
	}
	return nil
}

func (t *ragSearchTool) Run(ctx context.Context, input string, roomID string) (string, error) {
	args, err := toolruntime.ParseActionInputJSONObject(input)
	if err != nil {
		return "", err
	}
	query := toolruntime.ReadStringArg(args, "query")
	if query == "" {
		return "", errors.New("search_rag requires input.query")
	}
	if strings.TrimSpace(roomID) == "" {
		return "", errors.New("search_rag requires room_id")
	}
	if t.deps.RAGSearch == nil {
		return "", errors.New("search_rag is not configured")
	}
	return t.deps.RAGSearch(ctx, strings.TrimSpace(roomID), query, t.cfg.RAGSearchTopK, t.cfg.RAGSearchVector)
}
