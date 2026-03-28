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

type ToolDescriptor struct {
	Name        string
	Description string
	Parameters  map[string]any
}

type FunctionToolDefinition struct {
	Type     string                 `json:"type"`
	Function FunctionDefinitionBody `json:"function"`
}

type FunctionDefinitionBody struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type defaultRuntime struct {
	cfg  Config
	deps Deps
}

var localFunctionToolDefinitions = map[string]FunctionToolDefinition{
	"search_rag": {
		Type: "function",
		Function: FunctionDefinitionBody{
			Name:        "search_rag",
			Description: "检索历史消息记忆（历史消息）",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "检索关键词",
					},
				},
				"required":             []string{"query"},
				"additionalProperties": false,
			},
		},
	},
	"generate_image": {
		Type: "function",
		Function: FunctionDefinitionBody{
			Name:        "generate_image",
			Description: "根据提示词生成图片并返回 attachment_id",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"prompt": map[string]any{
						"type":        "string",
						"description": "文生图提示词",
					},
				},
				"required":             []string{"prompt"},
				"additionalProperties": false,
			},
		},
	},
	"get_current_time": {
		Type: "function",
		Function: FunctionDefinitionBody{
			Name:        "get_current_time",
			Description: "获取当前系统时间",
			Parameters: map[string]any{
				"type":                 "object",
				"properties":           map[string]any{},
				"additionalProperties": false,
			},
		},
	},
	"replan": {
		Type: "function",
		Function: FunctionDefinitionBody{
			Name:        "replan",
			Description: "重新规划后续小任务",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"reason": map[string]any{
						"type":        "string",
						"description": "重规划原因",
					},
				},
				"additionalProperties": false,
			},
		},
	},
	"finish": {
		Type: "function",
		Function: FunctionDefinitionBody{
			Name:        "finish",
			Description: "结束任务并输出最终答案",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"final": map[string]any{
						"type":        "string",
						"description": "最终答案正文",
					},
				},
				"required":             []string{"final"},
				"additionalProperties": false,
			},
		},
	},
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
		args := ParseMCPToolArguments(input)
		if err := validateKnownMCPToolArgs(toolName, args); err != nil {
			return "", err
		}
		return r.deps.MCPCallByQualifiedName(ctx, toolName, args)
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
		args, err := ParseActionInputJSONObject(input)
		if err != nil {
			return &ValidationError{
				Message: "action.input 必须是 JSON 对象字符串",
				Detail:  err.Error(),
			}
		}
		if knownErr := validateKnownMCPToolArgs(toolName, args); knownErr != nil {
			return &ValidationError{
				Message: knownErr.Error(),
				Detail:  "示例: {\"urls\":[\"https://example.com\"]}",
			}
		}
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

func BuildFunctionToolDefinitions(descriptors []ToolDescriptor) []FunctionToolDefinition {
	tools := make([]FunctionToolDefinition, 0, len(descriptors))
	for _, descriptor := range descriptors {
		name := strings.TrimSpace(descriptor.Name)
		if name == "" {
			continue
		}
		description := strings.TrimSpace(descriptor.Description)
		if description == "" {
			if localDef, ok := localFunctionToolDefinitions[strings.ToLower(name)]; ok {
				description = strings.TrimSpace(localDef.Function.Description)
			}
		}
		if description == "" {
			description = "调用工具 " + name
		}
		tools = append(tools, FunctionToolDefinition{
			Type: "function",
			Function: FunctionDefinitionBody{
				Name:        name,
				Description: description,
				Parameters:  functionParametersSchema(name, descriptor.Parameters),
			},
		})
	}
	return tools
}

func BuildFunctionToolDefinitionsJSON(descriptors []ToolDescriptor) string {
	tools := BuildFunctionToolDefinitions(descriptors)
	if len(tools) == 0 {
		return "[]"
	}
	data, err := json.Marshal(tools)
	if err != nil {
		return "[]"
	}
	return strings.TrimSpace(string(data))
}

func functionParametersSchema(toolName string, configured map[string]any) map[string]any {
	if len(configured) > 0 {
		return cloneSchemaMap(configured)
	}
	normalized := strings.ToLower(strings.TrimSpace(toolName))
	if localDef, ok := localFunctionToolDefinitions[normalized]; ok {
		return cloneSchemaMap(localDef.Function.Parameters)
	}
	if strings.Contains(normalized, ":") {
		return mcpFunctionParametersSchema(normalized)
	}
	return map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": true,
	}
}

func cloneSchemaMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return map[string]any{
			"type":                 "object",
			"properties":           map[string]any{},
			"additionalProperties": true,
		}
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func mcpFunctionParametersSchema(normalizedToolName string) map[string]any {
	if normalizedToolName == "tavily:tavily_extract" || strings.HasSuffix(normalizedToolName, ":tavily_extract") {
		return map[string]any{
			"type": "object",
			"properties": map[string]any{
				"urls": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "string",
					},
					"description": "要抽取内容的 URL 列表",
				},
			},
			"required": []string{"urls"},
		}
	}
	return map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": true,
	}
}

func validateKnownMCPToolArgs(toolName string, args map[string]any) error {
	normalized := strings.ToLower(strings.TrimSpace(toolName))
	if normalized == "tavily:tavily_extract" || strings.HasSuffix(normalized, ":tavily_extract") {
		urls, ok := args["urls"]
		if !ok {
			return errors.New("tavily_extract requires input.urls")
		}
		if !hasNonEmptyURLValues(urls) {
			return errors.New("tavily_extract requires non-empty input.urls")
		}
	}
	return nil
}

func hasNonEmptyURLValues(raw any) bool {
	switch v := raw.(type) {
	case []any:
		for _, item := range v {
			if strings.TrimSpace(fmt.Sprint(item)) != "" {
				return true
			}
		}
	case []string:
		for _, item := range v {
			if strings.TrimSpace(item) != "" {
				return true
			}
		}
	}
	return false
}

func timeNowRFC3339() string {
	return time.Now().Format(time.RFC3339)
}
