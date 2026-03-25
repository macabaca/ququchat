package services

import (
	"context"
	"fmt"
	"strings"

	agentmemory "ququchat/internal/taskservice/task/agent/memory"
	agenttypes "ququchat/internal/taskservice/task/agent/types"
)

type FinalAnswerClient interface {
	Chat(ctx context.Context, prompt string) (string, error)
}

func EvaluateFinalAnswer(ctx context.Context, client FinalAnswerClient, goal string, recentMessages []string, trace []agentmemory.Observation, candidate string) (agenttypes.FinalReviewResult, error) {
	raw, err := client.Chat(ctx, BuildFinalJudgePrompt(agenttypes.FinalJudgePromptInput{
		Goal:               goal,
		Candidate:          candidate,
		RecentMessagesText: agentmemory.BuildRecentMessagesSnippet(recentMessages, 10),
		TraceText:          agentmemory.BuildTraceSnippet(trace, 8),
	}))
	if err != nil {
		return agenttypes.FinalReviewResult{}, err
	}
	var review agenttypes.FinalReviewResult
	if err := DecodeJSONFromText(raw, &review); err != nil {
		return agenttypes.FinalReviewResult{}, fmt.Errorf("final评估结果不可解析: %w", err)
	}
	if review.Score < 0 {
		review.Score = 0
	}
	if review.Score > 100 {
		review.Score = 100
	}
	if !review.Pass && len(review.Issues) == 0 {
		review.Issues = []string{"未通过但未提供原因"}
	}
	return review, nil
}

func SynthesizeFinalAnswer(ctx context.Context, client FinalAnswerClient, goal string, recentMessages []string, trace []agentmemory.Observation, candidate string) (string, error) {
	raw, err := client.Chat(ctx, BuildFinalSynthesizerPrompt(agenttypes.FinalSynthesizerPromptInput{
		Goal:               goal,
		Candidate:          candidate,
		RecentMessagesText: agentmemory.BuildRecentMessagesSnippet(recentMessages, 12),
		TraceText:          agentmemory.BuildTraceSnippet(trace, 10),
	}))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(raw), nil
}
