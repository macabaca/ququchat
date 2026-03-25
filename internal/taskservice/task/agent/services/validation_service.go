package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"ququchat/internal/taskservice/task/agent/toolruntime"
	agenttypes "ququchat/internal/taskservice/task/agent/types"
)

const (
	ErrCodeJSONEmpty        = "E_JSON_EMPTY"
	ErrCodeJSONNotFound     = "E_JSON_NOT_FOUND"
	ErrCodeJSONInvalid      = "E_JSON_INVALID"
	ErrCodeFieldMissing     = "E_FIELD_MISSING"
	ErrCodeFieldType        = "E_FIELD_TYPE"
	ErrCodeToolCombination  = "E_TOOL_COMBINATION"
	ErrCodeToolNotAllowed   = "E_TOOL_NOT_ALLOWED"
	ErrCodeToolInputInvalid = "E_TOOL_INPUT_INVALID"
	ErrCodeNormalizeFailed  = "E_NORMALIZE_FAILED"
)

var validationIssueCodeDescMap = map[string]string{
	ErrCodeJSONEmpty:        "输出内容为空，必须返回一个完整JSON对象。",
	ErrCodeJSONNotFound:     "输出中没有可解析的JSON对象，请去掉额外说明或markdown。",
	ErrCodeJSONInvalid:      "JSON结构不合法，通常是引号、逗号或括号不匹配。",
	ErrCodeFieldMissing:     "缺少必填字段，请补齐schema要求的字段。",
	ErrCodeFieldType:        "字段类型错误，请按schema要求改成正确类型。",
	ErrCodeToolCombination:  "工具字段只能填一个工具名，不能写多个工具组合。",
	ErrCodeToolNotAllowed:   "工具名不在允许列表中，请改成支持的工具名。",
	ErrCodeToolInputInvalid: "工具输入不符合约定，请按工具的 JSON 参数说明传值。",
	ErrCodeNormalizeFailed:  "规范化输出失败，请先保证输出是可解析且字段完整的JSON。",
}

func BuildValidationRetryFeedback(baseFeedback string, issues []agenttypes.ValidationIssue, schemaText string, title string) string {
	builder := strings.Builder{}
	if strings.TrimSpace(baseFeedback) != "" {
		builder.WriteString(strings.TrimSpace(baseFeedback))
		builder.WriteString("\n")
	}
	if strings.TrimSpace(title) == "" {
		title = "上一轮规则校验未通过，错误如下："
	}
	builder.WriteString(title)
	if len(issues) == 0 {
		builder.WriteString("未知格式错误")
	} else {
		builder.WriteString("\n")
		for i, issue := range issues {
			builder.WriteString(strconv.Itoa(i + 1))
			builder.WriteString(") ")
			builder.WriteString(ValidationIssueText(issue))
			builder.WriteString("\n")
		}
		explainLines := BuildValidationIssueExplanations(issues)
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
	builder.WriteString(strings.TrimSpace(schemaText))
	return builder.String()
}

func BuildValidationIssueExplanations(issues []agenttypes.ValidationIssue) []string {
	if len(issues) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(issues))
	lines := make([]string, 0, len(issues))
	for _, issue := range issues {
		code := strings.TrimSpace(issue.Code)
		if code == "" {
			continue
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		desc := ValidationIssueCodeDescription(code)
		if strings.TrimSpace(desc) == "" {
			continue
		}
		lines = append(lines, "["+code+"] "+desc)
	}
	return lines
}

func ValidationIssueCodeDescription(code string) string {
	return validationIssueCodeDescMap[strings.TrimSpace(code)]
}

func ValidationIssueText(issue agenttypes.ValidationIssue) string {
	code := strings.TrimSpace(issue.Code)
	if code == "" {
		code = "E_UNKNOWN"
	}
	field := strings.TrimSpace(issue.Field)
	msg := strings.TrimSpace(issue.Message)
	detail := strings.TrimSpace(issue.Detail)
	builder := strings.Builder{}
	builder.WriteString("[")
	builder.WriteString(code)
	builder.WriteString("]")
	if field != "" {
		builder.WriteString(" ")
		builder.WriteString(field)
		builder.WriteString(": ")
	} else {
		builder.WriteString(" ")
	}
	builder.WriteString(msg)
	if detail != "" {
		builder.WriteString("（")
		builder.WriteString(detail)
		builder.WriteString("）")
	}
	return builder.String()
}

func JoinValidationIssueTexts(issues []agenttypes.ValidationIssue, sep string) string {
	if len(issues) == 0 {
		return ""
	}
	parts := make([]string, 0, len(issues))
	for _, issue := range issues {
		parts = append(parts, ValidationIssueText(issue))
	}
	return strings.Join(parts, sep)
}

func DecodeJSONFromText(raw string, v interface{}) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return errors.New("empty output")
	}
	if err := json.Unmarshal([]byte(trimmed), v); err == nil {
		return nil
	}
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start >= 0 && end > start {
		candidate := strings.TrimSpace(trimmed[start : end+1])
		if err := json.Unmarshal([]byte(candidate), v); err == nil {
			return nil
		}
	}
	return errors.New("json not found")
}

func ExtractJSONObjectText(raw string) (string, *agenttypes.ValidationIssue) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", &agenttypes.ValidationIssue{
			Code:    ErrCodeJSONEmpty,
			Field:   "$",
			Message: "输出为空",
			Detail:  "请输出一个JSON对象",
		}
	}
	if json.Valid([]byte(trimmed)) {
		return trimmed, nil
	}
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start >= 0 && end > start {
		candidate := strings.TrimSpace(trimmed[start : end+1])
		if json.Valid([]byte(candidate)) {
			return candidate, nil
		}
	}
	return "", &agenttypes.ValidationIssue{
		Code:    ErrCodeJSONNotFound,
		Field:   "$",
		Message: "未找到合法JSON对象",
		Detail:  "请只输出JSON对象，不要包含markdown代码块或额外解释",
	}
}

func ReadStringField(m map[string]interface{}, key string) (string, bool) {
	value, ok := m[key]
	if !ok {
		return "", false
	}
	text, ok := value.(string)
	if !ok {
		return "", false
	}
	return text, true
}

func ReadStringValue(m map[string]interface{}, key string) string {
	text, ok := ReadStringField(m, key)
	if !ok {
		return ""
	}
	return text
}

func ValidateObjectAgainstSchema(root map[string]interface{}, fields []agenttypes.SchemaField, parent string) (map[string]interface{}, []agenttypes.ValidationIssue) {
	values := make(map[string]interface{}, len(fields))
	issues := make([]agenttypes.ValidationIssue, 0)
	for _, field := range fields {
		name := strings.TrimSpace(field.Name)
		if name == "" {
			continue
		}
		fullName := name
		if strings.TrimSpace(parent) != "" {
			fullName = strings.TrimSpace(parent) + "." + name
		}
		value, exists := root[name]
		if !exists {
			if field.Required {
				issues = append(issues, agenttypes.ValidationIssue{
					Code:    ErrCodeFieldMissing,
					Field:   fullName,
					Message: "字段缺失",
					Detail:  "该字段为必填项",
				})
			}
			continue
		}
		if !MatchesSchemaType(value, field.Type) {
			issues = append(issues, agenttypes.ValidationIssue{
				Code:    ErrCodeFieldType,
				Field:   fullName,
				Message: "字段类型错误，期望 " + strings.ToLower(strings.TrimSpace(field.Type)),
				Detail:  "实际值类型: " + JsonValueTypeName(value),
			})
			continue
		}
		values[name] = value
	}
	return values, issues
}

func JsonValueTypeName(value interface{}) string {
	switch value.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case bool:
		return "boolean"
	case float64, float32, int, int32, int64:
		return "number"
	case map[string]interface{}:
		return "object"
	case []interface{}:
		return "array"
	default:
		return fmt.Sprintf("%T", value)
	}
}

func MatchesSchemaType(value interface{}, expectedType string) bool {
	switch strings.ToLower(strings.TrimSpace(expectedType)) {
	case "string":
		_, ok := value.(string)
		return ok
	case "object":
		_, ok := value.(map[string]interface{})
		return ok
	case "array":
		_, ok := value.([]interface{})
		return ok
	case "number":
		switch value.(type) {
		case float64, float32, int, int32, int64:
			return true
		}
		return false
	case "bool", "boolean":
		_, ok := value.(bool)
		return ok
	default:
		return true
	}
}

func ContainsToolCombination(s string) bool {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return false
	}
	return strings.Contains(trimmed, "|") || strings.Contains(trimmed, "/") || strings.Contains(trimmed, ",") || strings.Contains(trimmed, "，") || strings.Contains(trimmed, ";") || strings.Contains(trimmed, "；")
}

func ValidateCoordinatorOutput(raw string, specs []agenttypes.ToolSpec, cfg agenttypes.CoordinatorSchemaConfig) (agenttypes.Plan, string, []agenttypes.ValidationIssue) {
	candidate, extractIssue := ExtractJSONObjectText(raw)
	if extractIssue != nil {
		return agenttypes.Plan{}, "", []agenttypes.ValidationIssue{*extractIssue}
	}
	var root map[string]interface{}
	if err := json.Unmarshal([]byte(candidate), &root); err != nil {
		return agenttypes.Plan{}, "", []agenttypes.ValidationIssue{
			{
				Code:    ErrCodeJSONInvalid,
				Field:   "$",
				Message: "输出不是合法JSON对象",
				Detail:  err.Error(),
			},
		}
	}
	issues := make([]agenttypes.ValidationIssue, 0)
	_, topLevelIssues := ValidateObjectAgainstSchema(root, cfg.TopLevelFields, "")
	issues = append(issues, topLevelIssues...)
	toolRaw := ""
	input := ""
	thought := ReadStringValue(root, cfg.ThoughtField)
	actionValue, hasAction := root[cfg.ActionField]
	actionMap, actionOK := actionValue.(map[string]interface{})
	if hasAction && actionOK {
		_, actionIssues := ValidateObjectAgainstSchema(actionMap, cfg.ActionFields, cfg.ActionField)
		issues = append(issues, actionIssues...)
		toolRaw = ReadStringValue(actionMap, cfg.ToolField)
		input = ReadStringValue(actionMap, cfg.InputField)
	}
	toolName := normalizeToolFromSpecs(specs, toolRaw)
	if strings.TrimSpace(toolRaw) != "" && cfg.DisallowToolCombination && ContainsToolCombination(toolRaw) {
		issues = append(issues, agenttypes.ValidationIssue{
			Code:    ErrCodeToolCombination,
			Field:   cfg.ActionField + "." + cfg.ToolField,
			Message: "工具名不能是组合值",
			Detail:  "仅允许单个工具名，例如 search_rag 或 finish",
		})
	}
	if cfg.ToolEnumFromConfig && (strings.TrimSpace(toolName) == "" || !isKnownToolNameFromSpecs(specs, toolName)) {
		issues = append(issues, agenttypes.ValidationIssue{
			Code:    ErrCodeToolNotAllowed,
			Field:   cfg.ActionField + "." + cfg.ToolField,
			Message: "工具不在允许列表",
			Detail:  "允许值: " + allowedToolNamesCSVFromSpecs(specs),
		})
	}
	toolValidator := toolruntime.New(toolruntime.Config{}, toolruntime.Deps{})
	if issue := toolValidator.Validate(toolName, input); issue != nil {
		issues = append(issues, agenttypes.ValidationIssue{
			Code:    ErrCodeToolInputInvalid,
			Field:   cfg.ActionField + "." + cfg.InputField,
			Message: issue.Message,
			Detail:  issue.Detail,
		})
	}
	if len(issues) > 0 {
		return agenttypes.Plan{}, "", issues
	}
	normalizedPlan := agenttypes.Plan{
		Thought: strings.TrimSpace(thought),
		Action: agenttypes.Action{
			Tool:  strings.TrimSpace(toolName),
			Input: strings.TrimSpace(input),
		},
	}
	b, marshalErr := json.Marshal(normalizedPlan)
	if marshalErr != nil {
		return agenttypes.Plan{}, "", []agenttypes.ValidationIssue{
			{
				Code:    ErrCodeNormalizeFailed,
				Field:   "$",
				Message: "输出规范化失败",
				Detail:  marshalErr.Error(),
			},
		}
	}
	return normalizedPlan, string(b), nil
}

func allowedToolNamesCSVFromSpecs(specs []agenttypes.ToolSpec) string {
	names := make([]string, 0, len(specs))
	for _, spec := range specs {
		if strings.TrimSpace(spec.Name) != "" {
			names = append(names, strings.TrimSpace(spec.Name))
		}
	}
	return strings.Join(names, ", ")
}

func isKnownToolNameFromSpecs(specs []agenttypes.ToolSpec, name string) bool {
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

func normalizeToolFromSpecs(specs []agenttypes.ToolSpec, raw string) string {
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
			for _, alias := range spec.Aliases {
				if normalized == strings.ToLower(strings.TrimSpace(alias)) {
					return strings.TrimSpace(spec.Name)
				}
			}
		}
	}
	for _, spec := range specs {
		for _, alias := range spec.Aliases {
			if tool == strings.ToLower(strings.TrimSpace(alias)) {
				return strings.TrimSpace(spec.Name)
			}
		}
	}
	return ""
}
