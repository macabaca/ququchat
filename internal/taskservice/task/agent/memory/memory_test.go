package memory

import (
	"context"
	"strings"
	"testing"
)

func TestSessionRecallAndFinalize(t *testing.T) {
	session := NewFacade().NewSession(SessionInput{
		RoomID:         "room-a",
		Goal:           "总结今天讨论",
		RecentMessages: []string{"A: hi", "B: hello", "C: done"},
		MaxRecent:      2,
	})
	recall, err := session.Recall(context.Background(), RecallRequest{NeedRecent: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(recall.RecentMessages) != 2 {
		t.Fatalf("expected 2 recent messages, got %d", len(recall.RecentMessages))
	}
	session.AppendObservation(Observation{
		Step:   1,
		Role:   "Executor",
		Tool:   "search_rag",
		Input:  "{\"query\":\"会议结论\"}",
		Output: "命中2条",
		Status: "succeeded",
	})
	result := session.Finalize("最终答复")
	if strings.TrimSpace(result.FinalAnswer) != "最终答复" {
		t.Fatalf("unexpected final answer: %q", result.FinalAnswer)
	}
	if !strings.Contains(result.ReportText, "工具调用记录：") {
		t.Fatalf("report text missing memory marker: %q", result.ReportText)
	}
	if !strings.Contains(result.ReportText, "最终结果：") {
		t.Fatalf("report text missing final marker: %q", result.ReportText)
	}
}

func TestSplitLegacyReport(t *testing.T) {
	report := "工具调用记录：\n1. step=1, role=Executor\n最终结果：\n这是答案"
	final, memoryText := SplitLegacyReport(report)
	if strings.TrimSpace(final) != "这是答案" {
		t.Fatalf("unexpected final: %q", final)
	}
	if !strings.Contains(memoryText, "step=1") {
		t.Fatalf("unexpected memory text: %q", memoryText)
	}
}

func TestBuildFeedbackUsesFullOutputByDefault(t *testing.T) {
	session := NewFacade().NewSession(SessionInput{})
	session.AppendObservation(Observation{
		Tool:   "search_rag",
		Output: "abcdefghijklmnopqrstuvwxyz",
		Status: "succeeded",
	})
	feedback := session.BuildFeedback()
	if !strings.Contains(feedback, "输出内容：abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("unexpected feedback: %q", feedback)
	}
}

func TestBuildFeedbackCanLimitOutputLength(t *testing.T) {
	session := NewFacade().NewSession(SessionInput{FeedbackOutputMaxChars: 5})
	session.AppendObservation(Observation{
		Tool:   "search_rag",
		Output: "abcdefghijklmnopqrstuvwxyz",
		Status: "succeeded",
	})
	feedback := session.BuildFeedback()
	if !strings.Contains(feedback, "输出内容：abcde...") {
		t.Fatalf("unexpected feedback: %q", feedback)
	}
}

func TestBuildRecentMessagesSnippet(t *testing.T) {
	snippet := BuildRecentMessagesSnippet([]string{
		"A: first",
		"B: second",
		"C: third",
	}, 2)
	if !strings.Contains(snippet, "1. B: second") {
		t.Fatalf("unexpected snippet: %q", snippet)
	}
	if !strings.Contains(snippet, "2. C: third") {
		t.Fatalf("unexpected snippet: %q", snippet)
	}
}

func TestNormalizeRecentMessages(t *testing.T) {
	normalized := NormalizeRecentMessages([]string{"  A: hi  ", "", "   ", "B: ok"})
	if len(normalized) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(normalized))
	}
	if normalized[0] != "A: hi" || normalized[1] != "B: ok" {
		t.Fatalf("unexpected normalized messages: %#v", normalized)
	}
}
