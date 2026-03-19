package tasksvc

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	taskagent "ququchat/internal/service/task/agent"
	"ququchat/internal/service/task/aigcmq"
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
		Goal:           goal,
		RecentMessages: append([]string(nil), t.Payload.Agent.RecentMessages...),
		MaxSteps:       t.Payload.Agent.MaxSteps,
		RoomID:         strings.TrimSpace(t.Payload.Agent.RoomID),
		RAGSearch:      e.agentSearchRAG,
		AIGCGenerate:   e.agentGenerateAIGC,
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
			builder.WriteString(shortText(text, 180))
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
