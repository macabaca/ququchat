package toolruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type RAGSearchFunc func(ctx context.Context, roomID string, query string, topK int, vector string) (string, error)
type AIGCGenerateFunc func(ctx context.Context, prompt string) (string, error)
type MCPCallFunc func(ctx context.Context, qualifiedToolName string, arguments map[string]any) (string, error)

type Config struct {
	RAGSearchTopK   int
	RAGSearchVector string
}

type Deps struct {
	RAGSearch              RAGSearchFunc
	AIGCGenerate           AIGCGenerateFunc
	MCPCallByQualifiedName MCPCallFunc
}

type ValidationError struct {
	Message string
	Detail  string
}

type Runtime interface {
	Run(ctx context.Context, toolName string, input string, roomID string) (string, error)
	Validate(toolName string, input string) *ValidationError
}

type defaultRuntime struct {
	cfg  Config
	deps Deps
}

func New(cfg Config, deps Deps) Runtime {
	return &defaultRuntime{
		cfg:  cfg,
		deps: deps,
	}
}

func (r *defaultRuntime) Run(ctx context.Context, toolName string, input string, roomID string) (string, error) {
	if strings.Contains(toolName, ":") {
		if r.deps.MCPCallByQualifiedName == nil {
			return "", errors.New("mcp tool is not configured")
		}
		return r.deps.MCPCallByQualifiedName(ctx, toolName, ParseMCPToolArguments(input))
	}
	switch toolName {
	case "search_rag":
		args, err := ParseActionInputJSONObject(input)
		if err != nil {
			return "", err
		}
		query := ReadStringArg(args, "query")
		if query == "" {
			return "", errors.New("search_rag requires input.query")
		}
		if strings.TrimSpace(roomID) == "" {
			return "", errors.New("search_rag requires room_id")
		}
		if r.deps.RAGSearch == nil {
			return "", errors.New("search_rag is not configured")
		}
		return r.deps.RAGSearch(ctx, strings.TrimSpace(roomID), query, r.cfg.RAGSearchTopK, r.cfg.RAGSearchVector)
	case "generate_image":
		args, err := ParseActionInputJSONObject(input)
		if err != nil {
			return "", err
		}
		prompt := ReadStringArg(args, "prompt")
		if prompt == "" {
			return "", errors.New("generate_image requires input.prompt")
		}
		if r.deps.AIGCGenerate == nil {
			return "", errors.New("generate_image is not configured")
		}
		return r.deps.AIGCGenerate(ctx, prompt)
	case "get_current_time":
		return timeNowRFC3339(), nil
	default:
		return "", errors.New("unsupported tool")
	}
}

func (r *defaultRuntime) Validate(toolName string, input string) *ValidationError {
	if toolName == "" {
		return nil
	}
	if strings.Contains(toolName, ":") {
		return nil
	}
	args, err := ParseActionInputJSONObject(input)
	if err != nil {
		return &ValidationError{
			Message: "action.input 必须是 JSON 对象字符串",
			Detail:  err.Error(),
		}
	}
	switch toolName {
	case "search_rag":
		if ReadStringArg(args, "query") == "" {
			return &ValidationError{
				Message: "search_rag 需要 input.query",
				Detail:  "示例: {\"query\":\"检索关键词\"}",
			}
		}
	case "generate_image":
		if ReadStringArg(args, "prompt") == "" {
			return &ValidationError{
				Message: "generate_image 需要 input.prompt",
				Detail:  "示例: {\"prompt\":\"一只戴墨镜的柴犬\"}",
			}
		}
	case "finish":
		if ReadStringArg(args, "final") == "" {
			return &ValidationError{
				Message: "finish 需要 input.final",
				Detail:  "示例: {\"final\":\"最终答案正文\"}",
			}
		}
	case "replan":
		if HasKey(args, "reason") {
			if _, ok := args["reason"].(string); !ok {
				return &ValidationError{
					Message: "replan 的 input.reason 必须是字符串",
					Detail:  "示例: {\"reason\":\"当前方案命中率低\"}",
				}
			}
		}
	}
	return nil
}

func ParseActionInputJSONObject(raw string) (map[string]any, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, errors.New("action.input 不能为空，应传 JSON 对象字符串")
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(trimmed), &out); err != nil {
		return nil, errors.New("action.input 不是合法 JSON 对象")
	}
	if out == nil {
		return nil, errors.New("action.input 不是 JSON 对象")
	}
	return out, nil
}

func ReadStringArg(args map[string]any, key string) string {
	if len(args) == 0 {
		return ""
	}
	raw, ok := args[strings.TrimSpace(key)]
	if !ok {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v)
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func HasKey(args map[string]any, key string) bool {
	if len(args) == 0 {
		return false
	}
	_, ok := args[strings.TrimSpace(key)]
	return ok
}

func ParseMCPToolArguments(raw string) map[string]any {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return map[string]any{}
	}
	var asMap map[string]any
	if err := json.Unmarshal([]byte(trimmed), &asMap); err == nil && asMap != nil {
		return asMap
	}
	return map[string]any{
		"input": trimmed,
	}
}

func timeNowRFC3339() string {
	return time.Now().Format(time.RFC3339)
}
