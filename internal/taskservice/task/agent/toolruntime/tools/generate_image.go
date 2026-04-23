package tools

import (
	"context"
	"errors"

	"ququchat/internal/taskservice/task/agent/toolruntime"
)

type generateImageTool struct {
	deps toolruntime.Deps
}

func NewGenerateImageTool(deps toolruntime.Deps) toolruntime.Tool {
	return &generateImageTool{deps: deps}
}

func (t *generateImageTool) Name() string { return "generate_image" }

func (t *generateImageTool) Spec() toolruntime.ToolDescriptor {
	return toolruntime.ToolDescriptor{
		Name:        "generate_image",
		Description: "根据提示词生成图片并返回 attachment_id",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"prompt": map[string]any{"type": "string", "description": "文生图提示词"},
			},
			"required":             []string{"prompt"},
			"additionalProperties": false,
		},
	}
}

func (t *generateImageTool) Validate(input string) *toolruntime.ValidationError {
	args, err := toolruntime.ParseActionInputJSONObject(input)
	if err != nil {
		return &toolruntime.ValidationError{Message: "action.input 必须是 JSON 对象字符串", Detail: err.Error()}
	}
	if toolruntime.ReadStringArg(args, "prompt") == "" {
		return &toolruntime.ValidationError{Message: "generate_image 需要 input.prompt", Detail: "示例: {\"prompt\":\"一只戴墨镜的柴犬\"}"}
	}
	return nil
}

func (t *generateImageTool) Run(ctx context.Context, input string, _ string) (string, error) {
	args, err := toolruntime.ParseActionInputJSONObject(input)
	if err != nil {
		return "", err
	}
	prompt := toolruntime.ReadStringArg(args, "prompt")
	if prompt == "" {
		return "", errors.New("generate_image requires input.prompt")
	}
	if t.deps.AIGCGenerate == nil {
		return "", errors.New("generate_image is not configured")
	}
	return t.deps.AIGCGenerate(ctx, prompt)
}
