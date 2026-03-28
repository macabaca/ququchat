package agent

import (
	"encoding/json"
	"strconv"
	"strings"
)

var toolSpecs = []ToolSpec{
	{
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
	{
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
	{
		Name:        "get_current_time",
		Description: "获取当前系统时间",
		Parameters: map[string]any{
			"type":                 "object",
			"properties":           map[string]any{},
			"additionalProperties": false,
		},
	},
	{
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
	{
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
}

var agentIdentityConfig = AgentIdentityConfig{
	Name:    "QuQuChat 群聊助手",
	Role:    "群聊机器人",
	Mission: "围绕群聊上下文为群友答疑、总结讨论内容，并在工具能力范围内执行群友提出的任务",
	Capabilities: []string{
		"可检索群聊历史消息来理解上下文",
		"可基于上下文生成清晰、可执行的回复",
		"可在任务已完成时给出最终答案",
	},
	Principles: []string{
		"优先基于群聊事实回答，不凭空编造",
		"当上下文不足时优先使用 search_rag 获取相关历史消息",
		"输出需遵守系统定义的JSON格式与工具约束",
	},
}

var coordinatorSchemaConfig = CoordinatorSchemaConfig{
	ThoughtField: "thought",
	ActionField:  "action",
	ToolField:    "tool",
	InputField:   "input",
	TopLevelFields: []SchemaField{
		{Name: "thought", Type: "string", Required: true},
		{Name: "action", Type: "object", Required: true},
	},
	ActionFields: []SchemaField{
		{Name: "tool", Type: "string", Required: true},
		{Name: "input", Type: "string", Required: true},
	},
	DisallowToolCombination: true,
	ToolEnumFromConfig:      true,
}

func getAgentIdentityConfig() AgentIdentityConfig {
	cfg := agentIdentityConfig
	cfg.Capabilities = append([]string(nil), agentIdentityConfig.Capabilities...)
	cfg.Principles = append([]string(nil), agentIdentityConfig.Principles...)
	return cfg
}

func listToolSpecs() []ToolSpec {
	return normalizeToolSpecs(toolSpecs)
}

func listToolSpecsWithDynamic(dynamic []ToolSpec) []ToolSpec {
	base := listToolSpecs()
	if len(dynamic) == 0 {
		return base
	}
	merged := make([]ToolSpec, 0, len(base)+len(dynamic))
	seen := make(map[string]struct{}, len(base)+len(dynamic))
	for _, spec := range base {
		name := strings.ToLower(strings.TrimSpace(spec.Name))
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		merged = append(merged, spec)
	}
	for _, spec := range normalizeToolSpecs(dynamic) {
		name := strings.ToLower(strings.TrimSpace(spec.Name))
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		merged = append(merged, spec)
	}
	return merged
}

func normalizeToolSpecs(specs []ToolSpec) []ToolSpec {
	next := make([]ToolSpec, 0, len(specs))
	for _, spec := range specs {
		name := strings.TrimSpace(spec.Name)
		if name == "" {
			continue
		}
		next = append(next, ToolSpec{
			Name:        name,
			Description: strings.TrimSpace(spec.Description),
			Parameters:  cloneToolParameters(spec.Parameters),
		})
	}
	return next
}

func cloneToolParameters(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func getCoordinatorSchemaConfig() CoordinatorSchemaConfig {
	cfg := coordinatorSchemaConfig
	cfg.TopLevelFields = append([]SchemaField(nil), coordinatorSchemaConfig.TopLevelFields...)
	cfg.ActionFields = append([]SchemaField(nil), coordinatorSchemaConfig.ActionFields...)
	return cfg
}

func buildCoordinatorToolSection() string {
	return buildCoordinatorToolSectionFromSpecs(listToolSpecs())
}

func buildCoordinatorToolSectionFromSpecs(specs []ToolSpec) string {
	builder := strings.Builder{}
	for i, spec := range specs {
		builder.WriteString(strconv.Itoa(i + 1))
		builder.WriteString(") ")
		builder.WriteString(formatFunctionToolSpec(spec))
		builder.WriteString("\n")
	}
	return builder.String()
}

func buildFormatCheckerToolSection() string {
	return buildFormatCheckerToolSectionFromSpecs(listToolSpecs())
}

func buildFormatCheckerToolSectionFromSpecs(specs []ToolSpec) string {
	builder := strings.Builder{}
	for i, spec := range specs {
		builder.WriteString(strconv.Itoa(i + 1))
		builder.WriteString(") ")
		builder.WriteString(formatFunctionToolSpec(spec))
		builder.WriteString("\n")
	}
	return builder.String()
}

func coordinatorPromptRuleLines() []string {
	return coordinatorPromptRuleLinesFromSpecs(listToolSpecs())
}

func coordinatorPromptRuleLinesFromSpecs(specs []ToolSpec) []string {
	cfg := getCoordinatorSchemaConfig()
	lines := make([]string, 0, 6)
	if cfg.ToolEnumFromConfig {
		line := cfg.ActionField + "." + cfg.ToolField + " 只能是 " + allowedToolNamesTextFromSpecs(specs)
		if cfg.DisallowToolCombination {
			line += "，不能写组合值"
		}
		lines = append(lines, line+"。")
	}
	actionFieldRules := make([]string, 0, len(cfg.ActionFields))
	for _, field := range cfg.ActionFields {
		if strings.TrimSpace(field.Name) == "" || strings.ToLower(strings.TrimSpace(field.Type)) != "string" || !field.Required {
			continue
		}
		actionFieldRules = append(actionFieldRules, strings.TrimSpace(field.Name))
	}
	if len(actionFieldRules) > 0 {
		lines = append(lines, cfg.ActionField+" 必须包含 "+strings.Join(actionFieldRules, "/")+" 且类型必须是 string。")
	}
	lines = append(lines, "不符合格式会触发硬校验失败并要求重试。")
	lines = append(lines, "当需要获取曾经聊天消息时使用 search_rag。")
	lines = append(lines, "仅输出一个JSON对象，不要输出额外说明。")
	return lines
}

func buildAgentIdentityPrompt() string {
	cfg := getAgentIdentityConfig()
	builder := strings.Builder{}
	builder.WriteString("身份设定:\n")
	builder.WriteString("- 名称：")
	builder.WriteString(strings.TrimSpace(cfg.Name))
	builder.WriteString("\n")
	builder.WriteString("- 角色：")
	builder.WriteString(strings.TrimSpace(cfg.Role))
	builder.WriteString("\n")
	builder.WriteString("- 任务目标：")
	builder.WriteString(strings.TrimSpace(cfg.Mission))
	builder.WriteString("\n")
	if len(cfg.Capabilities) > 0 {
		builder.WriteString("- 能力边界：\n")
		for i, item := range cfg.Capabilities {
			builder.WriteString("  ")
			builder.WriteString(strconv.Itoa(i + 1))
			builder.WriteString(") ")
			builder.WriteString(strings.TrimSpace(item))
			builder.WriteString("\n")
		}
	}
	if len(cfg.Principles) > 0 {
		builder.WriteString("- 行为原则：\n")
		for i, item := range cfg.Principles {
			builder.WriteString("  ")
			builder.WriteString(strconv.Itoa(i + 1))
			builder.WriteString(") ")
			builder.WriteString(strings.TrimSpace(item))
			builder.WriteString("\n")
		}
	}
	return builder.String()
}

func coordinatorSchemaTemplateText() string {
	return coordinatorSchemaTemplateTextFromSpecs(listToolSpecs())
}

func coordinatorSchemaTemplateTextFromSpecs(specs []ToolSpec) string {
	cfg := getCoordinatorSchemaConfig()
	builder := strings.Builder{}
	builder.WriteString("{\"")
	builder.WriteString(cfg.ThoughtField)
	builder.WriteString("\":\"string\",\"")
	builder.WriteString(cfg.ActionField)
	builder.WriteString("\":{\"")
	builder.WriteString(cfg.ToolField)
	builder.WriteString("\":\"")
	builder.WriteString(allowedToolNamesCSVFromSpecs(specs))
	builder.WriteString("\",\"")
	builder.WriteString(cfg.InputField)
	builder.WriteString("\":\"string\"}}")
	return builder.String()
}

func allowedToolNamesText() string {
	return allowedToolNamesTextFromSpecs(listToolSpecs())
}

func allowedToolNamesTextFromSpecs(specs []ToolSpec) string {
	names := make([]string, 0, len(specs))
	for _, spec := range specs {
		if strings.TrimSpace(spec.Name) != "" {
			names = append(names, strings.TrimSpace(spec.Name))
		}
	}
	if len(names) == 0 {
		return ""
	}
	return strings.Join(names, " 或 ")
}

func allowedToolNamesCSV() string {
	return allowedToolNamesCSVFromSpecs(listToolSpecs())
}

func allowedToolNamesCSVFromSpecs(specs []ToolSpec) string {
	names := make([]string, 0, len(specs))
	for _, spec := range specs {
		if strings.TrimSpace(spec.Name) != "" {
			names = append(names, strings.TrimSpace(spec.Name))
		}
	}
	return strings.Join(names, ", ")
}

func isKnownToolName(name string) bool {
	return isKnownToolNameFromSpecs(listToolSpecs(), name)
}

func isKnownToolNameFromSpecs(specs []ToolSpec, name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		return false
	}
	for _, spec := range specs {
		if normalized == strings.ToLower(strings.TrimSpace(spec.Name)) {
			return true
		}
	}
	return false
}

func normalizeToolFromConfig(raw string) string {
	return normalizeToolFromSpecs(listToolSpecs(), raw)
}

func normalizeToolFromSpecs(specs []ToolSpec, raw string) string {
	tool := strings.ToLower(strings.TrimSpace(raw))
	if tool == "" {
		return ""
	}
	for _, spec := range specs {
		name := strings.ToLower(strings.TrimSpace(spec.Name))
		if tool == name {
			return strings.TrimSpace(spec.Name)
		}
	}
	parts := strings.FieldsFunc(tool, func(r rune) bool {
		return r == '|' || r == '/' || r == ',' || r == '，' || r == ';' || r == '；' || r == ' '
	})
	for _, part := range parts {
		normalized := strings.TrimSpace(part)
		if normalized == "" {
			continue
		}
		for _, spec := range specs {
			if normalized == strings.ToLower(strings.TrimSpace(spec.Name)) {
				return strings.TrimSpace(spec.Name)
			}
		}
	}
	return ""
}

func formatFunctionToolSpec(spec ToolSpec) string {
	payload := map[string]any{
		"name":        strings.TrimSpace(spec.Name),
		"description": strings.TrimSpace(spec.Description),
		"parameters":  cloneToolParameters(spec.Parameters),
	}
	if len(payload["parameters"].(map[string]any)) == 0 {
		payload["parameters"] = map[string]any{
			"type":                 "object",
			"properties":           map[string]any{},
			"additionalProperties": true,
		}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return strings.TrimSpace(spec.Name)
	}
	return strings.TrimSpace(string(data))
}
