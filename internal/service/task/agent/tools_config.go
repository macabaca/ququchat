package agent

import (
	"strconv"
	"strings"
)

type ToolSpec struct {
	Name           string
	Purpose        string
	Usage          string
	InputGuideline string
	Aliases        []string
}

type SchemaField struct {
	Name     string
	Type     string
	Required bool
}

type PlannerSchemaConfig struct {
	ThoughtField             string
	ActionField              string
	ToolField                string
	InputField               string
	FinalField               string
	TopLevelFields           []SchemaField
	ActionFields             []SchemaField
	DisallowToolCombination  bool
	ToolEnumFromConfig       bool
	RequireFinalWhenToolName string
}

var toolSpecs = []ToolSpec{
	{
		Name:           "read_recent_messages",
		Purpose:        "读取最近消息作为上下文",
		Usage:          "action.tool=read_recent_messages",
		InputGuideline: "action.input 可为空或正整数，表示读取最近N条",
		Aliases:        []string{"read_recent_message", "readrecentmessages"},
	},
	{
		Name:           "finish",
		Purpose:        "结束任务并输出最终答案",
		Usage:          "action.tool=finish",
		InputGuideline: "action.final 必填，action.input 建议为空",
		Aliases:        []string{"done", "final", "finalize"},
	},
}

var plannerSchemaConfig = PlannerSchemaConfig{
	ThoughtField: "thought",
	ActionField:  "action",
	ToolField:    "tool",
	InputField:   "input",
	FinalField:   "final",
	TopLevelFields: []SchemaField{
		{Name: "thought", Type: "string", Required: true},
		{Name: "action", Type: "object", Required: true},
	},
	ActionFields: []SchemaField{
		{Name: "tool", Type: "string", Required: true},
		{Name: "input", Type: "string", Required: true},
		{Name: "final", Type: "string", Required: true},
	},
	DisallowToolCombination:  true,
	ToolEnumFromConfig:       true,
	RequireFinalWhenToolName: "finish",
}

func listToolSpecs() []ToolSpec {
	next := make([]ToolSpec, 0, len(toolSpecs))
	for _, spec := range toolSpecs {
		next = append(next, ToolSpec{
			Name:           strings.TrimSpace(spec.Name),
			Purpose:        strings.TrimSpace(spec.Purpose),
			Usage:          strings.TrimSpace(spec.Usage),
			InputGuideline: strings.TrimSpace(spec.InputGuideline),
			Aliases:        append([]string(nil), spec.Aliases...),
		})
	}
	return next
}

func getPlannerSchemaConfig() PlannerSchemaConfig {
	cfg := plannerSchemaConfig
	cfg.TopLevelFields = append([]SchemaField(nil), plannerSchemaConfig.TopLevelFields...)
	cfg.ActionFields = append([]SchemaField(nil), plannerSchemaConfig.ActionFields...)
	return cfg
}

func buildPlannerToolSection() string {
	specs := listToolSpecs()
	builder := strings.Builder{}
	for i, spec := range specs {
		builder.WriteString(strconv.Itoa(i + 1))
		builder.WriteString(") ")
		builder.WriteString(spec.Name)
		builder.WriteString(": ")
		builder.WriteString(spec.Purpose)
		builder.WriteString("；")
		builder.WriteString(spec.InputGuideline)
		builder.WriteString("；")
		builder.WriteString(spec.Usage)
		builder.WriteString("\n")
	}
	return builder.String()
}

func buildFormatCheckerToolSection() string {
	specs := listToolSpecs()
	builder := strings.Builder{}
	for i, spec := range specs {
		builder.WriteString(strconv.Itoa(i + 1))
		builder.WriteString(") name=")
		builder.WriteString(spec.Name)
		builder.WriteString(", purpose=")
		builder.WriteString(spec.Purpose)
		builder.WriteString(", usage=")
		builder.WriteString(spec.Usage)
		builder.WriteString(", input_rule=")
		builder.WriteString(spec.InputGuideline)
		if len(spec.Aliases) > 0 {
			builder.WriteString(", aliases=")
			builder.WriteString(strings.Join(spec.Aliases, "/"))
		}
		builder.WriteString("\n")
	}
	return builder.String()
}

func plannerPromptRuleLines() []string {
	cfg := getPlannerSchemaConfig()
	lines := make([]string, 0, 6)
	if cfg.ToolEnumFromConfig {
		line := cfg.ActionField + "." + cfg.ToolField + " 只能是 " + allowedToolNamesText()
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
	if strings.TrimSpace(cfg.RequireFinalWhenToolName) != "" {
		lines = append(lines, cfg.ActionField+"."+cfg.ToolField+"="+cfg.RequireFinalWhenToolName+" 时，"+cfg.ActionField+"."+cfg.FinalField+" 必须非空。")
	}
	lines = append(lines, "不符合格式会触发硬校验失败并要求重试。")
	lines = append(lines, "仅输出一个JSON对象，不要输出额外说明。")
	return lines
}

func plannerSchemaTemplateText() string {
	cfg := getPlannerSchemaConfig()
	builder := strings.Builder{}
	builder.WriteString("{\"")
	builder.WriteString(cfg.ThoughtField)
	builder.WriteString("\":\"string\",\"")
	builder.WriteString(cfg.ActionField)
	builder.WriteString("\":{\"")
	builder.WriteString(cfg.ToolField)
	builder.WriteString("\":\"")
	builder.WriteString(allowedToolNamesCSV())
	builder.WriteString("\",\"")
	builder.WriteString(cfg.InputField)
	builder.WriteString("\":\"string\",\"")
	builder.WriteString(cfg.FinalField)
	builder.WriteString("\":\"string\"}}")
	return builder.String()
}

func allowedToolNamesText() string {
	specs := listToolSpecs()
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
	specs := listToolSpecs()
	names := make([]string, 0, len(specs))
	for _, spec := range specs {
		if strings.TrimSpace(spec.Name) != "" {
			names = append(names, strings.TrimSpace(spec.Name))
		}
	}
	return strings.Join(names, ", ")
}

func isKnownToolName(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	if normalized == "" {
		return false
	}
	for _, spec := range listToolSpecs() {
		if normalized == strings.ToLower(strings.TrimSpace(spec.Name)) {
			return true
		}
	}
	return false
}

func normalizeToolFromConfig(raw string) string {
	tool := strings.ToLower(strings.TrimSpace(raw))
	if tool == "" {
		return ""
	}
	for _, spec := range listToolSpecs() {
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
		for _, spec := range listToolSpecs() {
			if normalized == strings.ToLower(strings.TrimSpace(spec.Name)) {
				return strings.TrimSpace(spec.Name)
			}
			for _, alias := range spec.Aliases {
				if normalized == strings.ToLower(strings.TrimSpace(alias)) {
					return strings.TrimSpace(spec.Name)
				}
			}
		}
	}
	for _, spec := range listToolSpecs() {
		for _, alias := range spec.Aliases {
			if tool == strings.ToLower(strings.TrimSpace(alias)) {
				return strings.TrimSpace(spec.Name)
			}
		}
	}
	return ""
}
