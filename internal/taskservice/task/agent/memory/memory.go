package memory

import (
	"context"
	"strconv"
	"strings"
)

const (
	defaultRecentLimit = 10
	memoryMarker       = "工具调用记录："
	finalMarker        = "最终结果："
	errorMarker        = "错误报告："
)

type SessionInput struct {
	RoomID                 string
	Goal                   string
	RecentMessages         []string
	MaxRecent              int
	FeedbackOutputMaxChars int
}

type RecallRequest struct {
	NeedRecent  bool
	RecentLimit int
}

type RecallContext struct {
	RecentMessages []string
	CombinedText   string
}

type Observation struct {
	Step   int
	Role   string
	Tool   string
	Input  string
	Output string
	Status string
	Error  string
}

type Result struct {
	FinalAnswer string
	MemoryText  string
	ReportText  string
	Trace       []Observation
	Payload     map[string]any
}

type Facade interface {
	NewSession(input SessionInput) Session
}

type Session interface {
	Recall(ctx context.Context, req RecallRequest) (RecallContext, error)
	AppendObservation(obs Observation)
	BuildFeedback() string
	Trace() []Observation
	Finalize(finalAnswer string) Result
}

type defaultFacade struct{}

type defaultSession struct {
	roomID                 string
	goal                   string
	recent                 []string
	maxRecent              int
	feedbackOutputMaxChars int
	observations           []Observation
}

func NewFacade() Facade {
	return defaultFacade{}
}

func NormalizeRecentMessages(messages []string) []string {
	if len(messages) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(messages))
	for _, line := range messages {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		normalized = append(normalized, trimmed)
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func (d defaultFacade) NewSession(input SessionInput) Session {
	maxRecent := input.MaxRecent
	if maxRecent <= 0 {
		maxRecent = defaultRecentLimit
	}
	recent := NormalizeRecentMessages(input.RecentMessages)
	return &defaultSession{
		roomID:                 strings.TrimSpace(input.RoomID),
		goal:                   strings.TrimSpace(input.Goal),
		recent:                 recent,
		maxRecent:              maxRecent,
		feedbackOutputMaxChars: input.FeedbackOutputMaxChars,
		observations:           make([]Observation, 0),
	}
}

func (s *defaultSession) Recall(_ context.Context, req RecallRequest) (RecallContext, error) {
	if !req.NeedRecent {
		return RecallContext{}, nil
	}
	limit := req.RecentLimit
	if limit <= 0 {
		limit = s.maxRecent
	}
	if limit <= 0 {
		limit = defaultRecentLimit
	}
	selected := s.recent
	if limit < len(selected) {
		selected = selected[len(selected)-limit:]
	}
	copied := append([]string(nil), selected...)
	return RecallContext{
		RecentMessages: copied,
		CombinedText:   formatNumberedLines(copied),
	}, nil
}

func (s *defaultSession) AppendObservation(obs Observation) {
	normalized := Observation{
		Step:   obs.Step,
		Role:   strings.TrimSpace(obs.Role),
		Tool:   strings.TrimSpace(obs.Tool),
		Input:  strings.TrimSpace(obs.Input),
		Output: strings.TrimSpace(obs.Output),
		Status: strings.TrimSpace(obs.Status),
		Error:  strings.TrimSpace(obs.Error),
	}
	if normalized.Status == "" {
		normalized.Status = "succeeded"
	}
	s.observations = append(s.observations, normalized)
}

func (s *defaultSession) BuildFeedback() string {
	if len(s.observations) == 0 {
		return ""
	}
	last := s.observations[len(s.observations)-1]
	builder := strings.Builder{}
	builder.WriteString("上一轮工具调用 ")
	if strings.TrimSpace(last.Tool) == "" {
		builder.WriteString("unknown")
	} else {
		builder.WriteString(strings.TrimSpace(last.Tool))
	}
	if strings.EqualFold(strings.TrimSpace(last.Status), "failed") {
		builder.WriteString(" 失败。")
		if strings.TrimSpace(last.Error) != "" {
			builder.WriteString("错误信息：")
			builder.WriteString(strings.TrimSpace(last.Error))
		}
		return strings.TrimSpace(builder.String())
	}
	builder.WriteString(" 成功。")
	if strings.TrimSpace(last.Output) == "" {
		builder.WriteString("输出为空。")
	} else {
		builder.WriteString("输出内容：")
		output := strings.TrimSpace(last.Output)
		if s.feedbackOutputMaxChars > 0 {
			output = ShortText(output, s.feedbackOutputMaxChars)
		}
		builder.WriteString(output)
	}
	return strings.TrimSpace(builder.String())
}

func (s *defaultSession) Trace() []Observation {
	if len(s.observations) == 0 {
		return nil
	}
	return append([]Observation(nil), s.observations...)
}

func (s *defaultSession) Finalize(finalAnswer string) Result {
	final := strings.TrimSpace(finalAnswer)
	memoryText := formatTrace(s.observations)
	reportText := formatLegacyReport(memoryText, final)
	payload := map[string]any{
		"room_id": strings.TrimSpace(s.roomID),
		"goal":    strings.TrimSpace(s.goal),
	}
	if len(s.observations) > 0 {
		payload["trace"] = append([]Observation(nil), s.observations...)
	}
	if memoryText != "" {
		payload["memory"] = memoryText
	}
	return Result{
		FinalAnswer: final,
		MemoryText:  memoryText,
		ReportText:  reportText,
		Trace:       append([]Observation(nil), s.observations...),
		Payload:     payload,
	}
}

func SplitLegacyReport(text string) (string, string) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", ""
	}
	if idx := strings.LastIndex(trimmed, finalMarker); idx >= 0 {
		memory := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(trimmed[:idx]), memoryMarker))
		final := strings.TrimSpace(trimmed[idx+len(finalMarker):])
		if final == "" {
			final = trimmed
		}
		return final, memory
	}
	if idx := strings.LastIndex(trimmed, errorMarker); idx >= 0 {
		memory := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(trimmed[:idx]), memoryMarker))
		final := strings.TrimSpace(trimmed[idx+len(errorMarker):])
		if final == "" {
			final = trimmed
		}
		return final, memory
	}
	return trimmed, ""
}

func formatLegacyReport(memoryText string, finalAnswer string) string {
	final := strings.TrimSpace(finalAnswer)
	if final == "" {
		final = "未提供最终结果"
	}
	builder := strings.Builder{}
	builder.WriteString(memoryMarker)
	builder.WriteString("\n")
	builder.WriteString(strings.TrimSpace(memoryText))
	builder.WriteString("\n")
	builder.WriteString(finalMarker)
	builder.WriteString("\n")
	builder.WriteString(final)
	return strings.TrimSpace(builder.String())
}

func formatTrace(observations []Observation) string {
	if len(observations) == 0 {
		return "1. 无"
	}
	builder := strings.Builder{}
	for i, obs := range observations {
		builder.WriteString(strconv.Itoa(i + 1))
		builder.WriteString(". ")
		builder.WriteString("step=")
		builder.WriteString(strconv.Itoa(obs.Step))
		builder.WriteString(", role=")
		builder.WriteString(strings.TrimSpace(obs.Role))
		builder.WriteString(", tool=")
		builder.WriteString(strings.TrimSpace(obs.Tool))
		builder.WriteString(", status=")
		builder.WriteString(strings.TrimSpace(obs.Status))
		if strings.TrimSpace(obs.Input) != "" {
			builder.WriteString(", input=")
			builder.WriteString(ShortText(obs.Input, 120))
		}
		if strings.TrimSpace(obs.Output) != "" {
			builder.WriteString(", output=")
			builder.WriteString(ShortText(obs.Output, 160))
		}
		if strings.TrimSpace(obs.Error) != "" {
			builder.WriteString(", error=")
			builder.WriteString(ShortText(obs.Error, 160))
		}
		builder.WriteString("\n")
	}
	return strings.TrimSpace(builder.String())
}

func BuildTraceSnippet(observations []Observation, max int) string {
	if len(observations) == 0 {
		return "无"
	}
	if max <= 0 {
		max = 1
	}
	selected := observations
	if len(selected) > max {
		selected = selected[len(selected)-max:]
	}
	builder := strings.Builder{}
	for i, obs := range selected {
		builder.WriteString(strconv.Itoa(i + 1))
		builder.WriteString(") role=")
		builder.WriteString(strings.TrimSpace(obs.Role))
		builder.WriteString(", tool=")
		builder.WriteString(strings.TrimSpace(obs.Tool))
		builder.WriteString(", status=")
		builder.WriteString(strings.TrimSpace(obs.Status))
		if strings.TrimSpace(obs.Output) != "" {
			builder.WriteString(", output=")
			builder.WriteString(ShortText(strings.TrimSpace(obs.Output), 120))
		}
		if strings.TrimSpace(obs.Error) != "" {
			builder.WriteString(", error=")
			builder.WriteString(ShortText(strings.TrimSpace(obs.Error), 120))
		}
		builder.WriteString("\n")
	}
	return strings.TrimSpace(builder.String())
}

func BuildRecentMessagesSnippet(messages []string, max int) string {
	if len(messages) == 0 {
		return "无"
	}
	if max <= 0 {
		max = 1
	}
	selected := messages
	if len(selected) > max {
		selected = selected[len(selected)-max:]
	}
	builder := strings.Builder{}
	for i, line := range selected {
		builder.WriteString(strconv.Itoa(i + 1))
		builder.WriteString(". ")
		builder.WriteString(ShortText(strings.TrimSpace(line), 140))
		builder.WriteString("\n")
	}
	return strings.TrimSpace(builder.String())
}

func FormatSuccessReport(observations []Observation, finalAnswer string) string {
	builder := strings.Builder{}
	builder.WriteString(memoryMarker)
	builder.WriteString("\n")
	builder.WriteString(formatTrace(observations))
	builder.WriteString("\n")
	builder.WriteString(finalMarker)
	builder.WriteString("\n")
	builder.WriteString(strings.TrimSpace(finalAnswer))
	return strings.TrimSpace(builder.String())
}

func FormatFailureReport(observations []Observation, errorReport string) string {
	builder := strings.Builder{}
	builder.WriteString(memoryMarker)
	builder.WriteString("\n")
	builder.WriteString(formatTrace(observations))
	builder.WriteString("\n")
	builder.WriteString(errorMarker)
	builder.WriteString("\n")
	builder.WriteString(strings.TrimSpace(errorReport))
	return strings.TrimSpace(builder.String())
}

func formatNumberedLines(lines []string) string {
	if len(lines) == 0 {
		return "无"
	}
	builder := strings.Builder{}
	for i, line := range lines {
		builder.WriteString(strconv.Itoa(i + 1))
		builder.WriteString(". ")
		builder.WriteString(strings.TrimSpace(line))
		builder.WriteString("\n")
	}
	return strings.TrimSpace(builder.String())
}

func ShortText(s string, max int) string {
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
