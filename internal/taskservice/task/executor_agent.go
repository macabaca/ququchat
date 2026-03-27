package tasksvc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	taskagent "ququchat/internal/taskservice/task/agent"
	"ququchat/internal/taskservice/task/aigcmq"
	"ququchat/internal/taskservice/task/mcpclient"
)

type AIGCClient interface {
	Generate(ctx context.Context, req aigcmq.GenerateRequest) (aigcmq.GenerateResponse, error)
}

func (e *DefaultExecutor) executeAgent(ctx context.Context, t *Task) (Result, error) {
	if t.Payload.Agent == nil {
		return Result{}, errors.New("missing agent payload")
	}
	if e.llmClient == nil {
		return Result{}, errors.New("llm client is not configured")
	}
	goal := strings.TrimSpace(t.Payload.Agent.Goal)
	if goal == "" {
		return Result{}, errors.New("agent goal is required")
	}
	text, err := taskagent.Execute(ctx, e.llmClient, taskagent.Input{
		Goal:                       goal,
		RecentMessages:             append([]string(nil), t.Payload.Agent.RecentMessages...),
		MaxSteps:                   t.Payload.Agent.MaxSteps,
		RoomID:                     strings.TrimSpace(t.Payload.Agent.RoomID),
		RAGSearch:                  e.agentSearchRAG,
		AIGCGenerate:               e.agentGenerateAIGC,
		DynamicToolSpecs:           e.listAgentMCPToolSpecs(ctx),
		MCPCallToolByQualifiedName: e.agentCallMCPToolByQualifiedName,
	})
	if err != nil {
		return Result{}, err
	}
	final, memory := splitAgentFinalAndMemory(text)
	payload := map[string]interface{}{}
	if strings.TrimSpace(memory) != "" {
		payload["memory"] = strings.TrimSpace(memory)
	}
	attachmentIDs := extractAIGCAttachmentIDs(text)
	if len(attachmentIDs) > 0 {
		payload["aigc_attachment_ids"] = attachmentIDs
	}
	return Result{
		Text:    &text,
		Final:   stringPtr(strings.TrimSpace(final)),
		Payload: payload,
	}, nil
}

func extractAIGCAttachmentIDs(text string) []string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	const marker = "attachment_id:"
	lower := strings.ToLower(trimmed)
	seen := map[string]struct{}{}
	ids := make([]string, 0)
	start := 0
	for {
		idx := strings.Index(lower[start:], marker)
		if idx < 0 {
			break
		}
		valueStart := start + idx + len(marker)
		valueEnd := len(trimmed)
		if lineBreak := strings.IndexAny(trimmed[valueStart:], "\n\r"); lineBreak >= 0 {
			valueEnd = valueStart + lineBreak
		}
		raw := strings.TrimSpace(trimmed[valueStart:valueEnd])
		parts := strings.Split(raw, ",")
		for _, part := range parts {
			id := normalizeAttachmentID(part)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
		start = valueStart
	}
	if len(ids) == 0 {
		return nil
	}
	return ids
}

func normalizeAttachmentID(raw string) string {
	id := strings.TrimSpace(raw)
	id = strings.Trim(id, "\"'[](){}<>`，。；;：:")
	if id == "" {
		return ""
	}
	if fields := strings.Fields(id); len(fields) > 0 {
		id = strings.TrimSpace(fields[0])
	}
	id = strings.Trim(id, "\"'[](){}<>`，。；;：:")
	return strings.TrimSpace(id)
}

func splitAgentFinalAndMemory(text string) (string, string) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", ""
	}
	const finalMarker = "最终结果："
	idx := strings.LastIndex(trimmed, finalMarker)
	if idx < 0 {
		return trimmed, ""
	}
	memory := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(trimmed[:idx]), "工具调用记录："))
	final := strings.TrimSpace(trimmed[idx+len(finalMarker):])
	if final == "" {
		final = trimmed
	}
	return final, memory
}

func stringPtr(s string) *string {
	v := strings.TrimSpace(s)
	return &v
}

func (e *DefaultExecutor) agentSearchRAG(ctx context.Context, roomID string, query string, topK int, vector string) (string, error) {
	if e == nil || e.ragHandler == nil {
		return "", errors.New("rag handler is not configured")
	}
	result, err := e.ragHandler.ExecuteRAGSearch(ctx, &RAGSearchPayload{
		RoomID: strings.TrimSpace(roomID),
		Query:  strings.TrimSpace(query),
		TopK:   topK,
		Vector: strings.TrimSpace(vector),
	})
	if err != nil {
		return "", err
	}
	return formatAgentRAGSearchOutput(result), nil
}

func (e *DefaultExecutor) agentGenerateAIGC(ctx context.Context, prompt string) (string, error) {
	if e == nil || e.aigcClient == nil {
		return "", errors.New("aigc client is not configured")
	}
	resp, err := e.aigcClient.Generate(ctx, aigcmq.GenerateRequest{
		Prompt:            strings.TrimSpace(prompt),
		ImageSize:         "1024x1024",
		BatchSize:         1,
		NumInferenceSteps: 20,
		GuidanceScale:     7.5,
	})
	if err != nil {
		return "", err
	}
	ids := make([]string, 0, len(resp.Images))
	for _, image := range resp.Images {
		attachmentID := strings.TrimSpace(image.AttachmentID)
		if attachmentID == "" {
			continue
		}
		ids = append(ids, attachmentID)
	}
	if len(ids) == 0 {
		return "", errors.New("aigc result does not contain attachment ids")
	}
	return "图片生成成功，attachment_id: " + strings.Join(ids, ", "), nil
}

func (e *DefaultExecutor) listAgentMCPToolSpecs(ctx context.Context) []taskagent.ToolSpec {
	if e == nil || e.mcpMultiClient == nil {
		return nil
	}
	tools, err := e.mcpMultiClient.ListTools(ctx)
	if err != nil || len(tools) == 0 {
		return nil
	}
	specs := make([]taskagent.ToolSpec, 0, len(tools))
	for _, routed := range tools {
		spec, ok := routedMCPToolToSpec(routed)
		if !ok {
			continue
		}
		specs = append(specs, spec)
	}
	if len(specs) == 0 {
		return nil
	}
	return specs
}

func routedMCPToolToSpec(routed mcpclient.RoutedTool) (taskagent.ToolSpec, bool) {
	qualifiedName := strings.TrimSpace(routed.QualifiedName)
	if qualifiedName == "" {
		return taskagent.ToolSpec{}, false
	}
	purpose := "远程 MCP 工具"
	if routed.Tool != nil {
		desc := strings.TrimSpace(fmt.Sprint(routed.Tool.Description))
		if desc != "" {
			purpose = desc
		}
	}
	return taskagent.ToolSpec{
		Name:           qualifiedName,
		Purpose:        purpose,
		Usage:          "action.tool=" + qualifiedName,
		InputGuideline: buildMCPInputGuideline(routed),
	}, true
}

func buildMCPInputGuideline(routed mcpclient.RoutedTool) string {
	base := "action.input 必须为 JSON 对象字符串。若输入不是 JSON 对象，将自动包装为 {\"input\":\"原始字符串\"}。"
	if routed.Tool == nil {
		return base
	}
	schema := schemaAsMap(routed.Tool.InputSchema)
	if len(schema) == 0 {
		return base
	}
	properties, _ := schema["properties"].(map[string]any)
	if len(properties) == 0 {
		return base
	}
	requiredSet := make(map[string]struct{})
	if requiredList, ok := schema["required"].([]any); ok {
		for _, item := range requiredList {
			name := strings.TrimSpace(fmt.Sprint(item))
			if name == "" {
				continue
			}
			requiredSet[name] = struct{}{}
		}
	}
	requiredKeys := make([]string, 0, len(requiredSet))
	for key := range requiredSet {
		name := strings.TrimSpace(key)
		if name == "" {
			continue
		}
		if _, ok := properties[name]; !ok {
			continue
		}
		requiredKeys = append(requiredKeys, name)
	}
	sort.Strings(requiredKeys)
	if len(requiredKeys) == 0 {
		return base + " 无必填参数，action.input 传 {} 即可。"
	}
	parts := make([]string, 0, len(requiredKeys)+1)
	parts = append(parts, base, "必填参数：")
	for _, key := range requiredKeys {
		line := buildMCPParamLine(key, properties[key])
		if line == "" {
			continue
		}
		parts = append(parts, line)
	}
	if len(parts) == 2 {
		return base
	}
	return strings.Join(parts, " ")
}

func schemaAsMap(raw any) map[string]any {
	if raw == nil {
		return nil
	}
	if m, ok := raw.(map[string]any); ok {
		return m
	}
	data, err := json.Marshal(raw)
	if err != nil || len(data) == 0 {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}

func buildMCPParamLine(name string, raw any) string {
	prop := schemaAsMap(raw)
	if len(prop) == 0 {
		return ""
	}
	typeText := readSchemaTypeText(prop)
	if typeText == "" {
		typeText = "any"
	}
	segments := []string{name + "（" + typeText + "，必填）"}
	desc := strings.TrimSpace(fmt.Sprint(prop["description"]))
	if desc != "" && desc != "<nil>" {
		segments = append(segments, shortText(desc, 110))
	}
	if enums := readSchemaEnumValues(prop["enum"]); len(enums) > 0 {
		segments = append(segments, "可选值="+strings.Join(enums, "/"))
	}
	if v, ok := prop["minimum"]; ok {
		segments = append(segments, "最小值="+strings.TrimSpace(fmt.Sprint(v)))
	}
	if v, ok := prop["maximum"]; ok {
		segments = append(segments, "最大值="+strings.TrimSpace(fmt.Sprint(v)))
	}
	if items, ok := prop["items"]; ok {
		itemsType := readSchemaTypeText(schemaAsMap(items))
		if itemsType != "" {
			segments = append(segments, "元素类型="+itemsType)
		}
	}
	return strings.Join(segments, "；")
}

func readSchemaTypeText(prop map[string]any) string {
	if len(prop) == 0 {
		return ""
	}
	switch typed := prop["type"].(type) {
	case string:
		return strings.TrimSpace(typed)
	case []any:
		types := make([]string, 0, len(typed))
		for _, item := range typed {
			text := strings.TrimSpace(fmt.Sprint(item))
			if text == "" {
				continue
			}
			types = append(types, text)
		}
		return strings.Join(types, "|")
	}
	if anyOf, ok := prop["anyOf"].([]any); ok {
		types := make([]string, 0, len(anyOf))
		for _, item := range anyOf {
			m := schemaAsMap(item)
			t := strings.TrimSpace(fmt.Sprint(m["type"]))
			if t == "" || t == "<nil>" {
				continue
			}
			types = append(types, t)
		}
		if len(types) > 0 {
			return strings.Join(types, "|")
		}
	}
	return ""
}

func readSchemaEnumValues(raw any) []string {
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return nil
	}
	values := make([]string, 0, len(items))
	for _, item := range items {
		text := strings.TrimSpace(fmt.Sprint(item))
		if text == "" || text == "<nil>" {
			continue
		}
		values = append(values, text)
	}
	return values
}

func formatSchemaValue(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return strings.TrimSpace(fmt.Sprint(v))
	}
	return strings.TrimSpace(string(data))
}

func (e *DefaultExecutor) agentCallMCPToolByQualifiedName(ctx context.Context, qualifiedToolName string, arguments map[string]any) (string, error) {
	if e == nil || e.mcpMultiClient == nil {
		return "", errors.New("mcp client is not configured")
	}
	result, err := e.mcpMultiClient.CallToolByQualifiedName(ctx, qualifiedToolName, arguments)
	if err != nil {
		return "", err
	}
	if result == nil {
		return "", nil
	}
	if businessErr := detectMCPBusinessError(result); businessErr != "" {
		return "", errors.New(businessErr)
	}
	encoded, encodeErr := json.Marshal(result)
	if encodeErr != nil {
		return strings.TrimSpace(fmt.Sprint(result)), nil
	}
	return strings.TrimSpace(string(encoded)), nil
}

func detectMCPBusinessError(result any) string {
	root := schemaAsMap(result)
	if len(root) == 0 {
		return ""
	}
	if readBoolValue(root, "is_error") || readBoolValue(root, "isError") {
		msg := firstNonEmptyText(readStringValue(root, "error"), extractMCPErrorFromContent(root["content"]))
		if msg == "" {
			msg = "mcp tool returned business error"
		}
		return msg
	}
	message := extractMCPErrorFromContent(root["content"])
	if message != "" {
		return message
	}
	return ""
}

func extractMCPErrorFromContent(raw any) string {
	items, ok := raw.([]any)
	if !ok || len(items) == 0 {
		return ""
	}
	for _, item := range items {
		block := schemaAsMap(item)
		if len(block) == 0 {
			continue
		}
		text := strings.TrimSpace(readStringValue(block, "text"))
		if text == "" {
			continue
		}
		direct := parseMCPBusinessErrorText(text)
		if direct != "" {
			return direct
		}
	}
	return ""
}

func parseMCPBusinessErrorText(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil || len(payload) == 0 {
		return ""
	}
	errText := strings.TrimSpace(readStringValue(payload, "error"))
	if errText != "" {
		status := readIntValue(payload, "status")
		if status > 0 {
			return errText + " (status=" + strconv.Itoa(status) + ")"
		}
		return errText
	}
	if failed, ok := payload["failed_results"].([]any); ok && len(failed) > 0 {
		results, _ := payload["results"].([]any)
		if len(results) == 0 {
			firstFailed := schemaAsMap(failed[0])
			failedMsg := strings.TrimSpace(readStringValue(firstFailed, "error"))
			if failedMsg != "" {
				return failedMsg
			}
			return "mcp tool returned failed_results without successful results"
		}
	}
	return ""
}

func readBoolValue(m map[string]any, key string) bool {
	if len(m) == 0 {
		return false
	}
	raw, ok := m[strings.TrimSpace(key)]
	if !ok {
		return false
	}
	switch v := raw.(type) {
	case bool:
		return v
	case string:
		text := strings.ToLower(strings.TrimSpace(v))
		return text == "true" || text == "1" || text == "yes"
	case float64:
		return v != 0
	case int:
		return v != 0
	}
	return false
}

func readStringValue(m map[string]any, key string) string {
	if len(m) == 0 {
		return ""
	}
	raw, ok := m[strings.TrimSpace(key)]
	if !ok {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(raw))
}

func readIntValue(m map[string]any, key string) int {
	if len(m) == 0 {
		return 0
	}
	raw, ok := m[strings.TrimSpace(key)]
	if !ok {
		return 0
	}
	switch v := raw.(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(v))
		if err == nil {
			return parsed
		}
	}
	return 0
}

func firstNonEmptyText(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func formatAgentRAGSearchOutput(result Result) string {
	rows := normalizeAgentRAGRows(result.Payload["results"])
	if len(rows) == 0 {
		return "历史消息检索无命中"
	}
	builder := strings.Builder{}
	builder.WriteString("历史消息检索命中：")
	builder.WriteString(strconv.Itoa(len(rows)))
	builder.WriteString("条")
	for i, row := range rows {
		builder.WriteString("\n")
		builder.WriteString(strconv.Itoa(i + 1))
		builder.WriteString(". score=")
		builder.WriteString(readAgentRAGScoreText(row["score"]))
		text := strings.TrimSpace(fmt.Sprint(row["raw_text"]))
		if text != "" {
			builder.WriteString(", text=")
			builder.WriteString(text)
		} else {
			builder.WriteString(", 无效记录")
		}
		pointID := strings.TrimSpace(fmt.Sprint(row["point_id"]))
		if pointID != "" {
			builder.WriteString(", point_id=")
			builder.WriteString(pointID)
		}
	}
	return strings.TrimSpace(builder.String())
}

func normalizeAgentRAGRows(raw interface{}) []map[string]interface{} {
	switch v := raw.(type) {
	case []map[string]interface{}:
		return v
	case []interface{}:
		rows := make([]map[string]interface{}, 0, len(v))
		for _, item := range v {
			m, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			rows = append(rows, m)
		}
		return rows
	default:
		return nil
	}
}

func readAgentRAGScoreText(raw interface{}) string {
	switch v := raw.(type) {
	case float64:
		return strconv.FormatFloat(v, 'f', 4, 64)
	case float32:
		return strconv.FormatFloat(float64(v), 'f', 4, 64)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	default:
		text := strings.TrimSpace(fmt.Sprint(raw))
		if text == "" {
			return "0"
		}
		return text
	}
}

func shortText(s string, max int) string {
	trimmed := strings.TrimSpace(s)
	if max <= 0 {
		return ""
	}
	runes := []rune(trimmed)
	if len(runes) <= max {
		return trimmed
	}
	return strings.TrimSpace(string(runes[:max])) + "..."
}
