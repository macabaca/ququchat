package agent

import (
	"context"
	"errors"
	"strings"

	agentmemory "ququchat/internal/taskservice/task/agent/memory"
	agentservices "ququchat/internal/taskservice/task/agent/services"
	"ququchat/internal/taskservice/task/agent/toolruntime"
)

func RunFinalJudgeNode(ctx context.Context, client ChatClient, state *State) (next string, err error) {
	if client == nil {
		return "", errors.New("final judge node client is not configured")
	}
	if state == nil {
		return "", errors.New("final judge node state is nil")
	}
	actionInput := strings.TrimSpace(state.ActionInput)
	if actionInput == "" {
		actionInput = strings.TrimSpace(state.Plan.Action.Input)
	}
	finalAnswer := ""
	args, argsErr := toolruntime.ParseActionInputJSONObject(actionInput)
	if argsErr == nil {
		finalAnswer = toolruntime.ReadStringArg(args, "final")
	}
	finalAnswer = strings.TrimSpace(finalAnswer)
	if finalAnswer == "" {
		return "", errors.New("coordinator选择finish但未提供结果")
	}
	trace := []agentmemory.Observation(nil)
	if state.MemorySession != nil {
		trace = state.MemorySession.Trace()
	}
	review, reviewErr := agentservices.EvaluateFinalAnswer(ctx, client, strings.TrimSpace(state.Goal), state.RecentMessages, trace, finalAnswer)
	state.FinalReview = review
	reviewRecord := agentmemory.Observation{
		Step:   state.Step,
		Role:   "FinalJudge",
		Tool:   "evaluate_final_answer",
		Input:  agentmemory.ShortText(finalAnswer, 220),
		Output: agentmemory.ShortText(agentservices.FormatFinalReviewOutput(review), 220),
	}
	if reviewErr != nil {
		reviewRecord.Status = "failed"
		reviewRecord.Error = reviewErr.Error()
		if state.MemorySession != nil {
			state.MemorySession.AppendObservation(reviewRecord)
		}
		return "final_judge.retry", nil
	}
	if review.Pass && review.Score >= finalScorePass {
		reviewRecord.Status = "succeeded"
		if state.MemorySession != nil {
			state.MemorySession.AppendObservation(reviewRecord)
		}
		finalCandidate := strings.TrimSpace(review.BetterFinal)
		if finalCandidate == "" {
			finalCandidate = finalAnswer
		}
		state.FinalAnswer = finalCandidate
		return "final_judge.done", nil
	}
	reviewRecord.Status = "failed"
	reviewRecord.Error = agentservices.BuildFinalReviewErrorText(review)
	if state.MemorySession != nil {
		state.MemorySession.AppendObservation(reviewRecord)
	}
	if state.Step < state.MaxSteps {
		if state.MemorySession != nil {
			state.MemorySession.AppendObservation(agentmemory.Observation{
				Step:   state.Step,
				Role:   "Coordinator",
				Tool:   "final_retry_feedback",
				Status: "failed",
				Error:  agentservices.BuildFinalRetryFeedback(strings.TrimSpace(state.Goal), finalAnswer, review),
			})
		}
		state.FinalAnswer = ""
		return "final_judge.retry", nil
	}
	synthesized, synthErr := agentservices.SynthesizeFinalAnswer(ctx, client, strings.TrimSpace(state.Goal), state.RecentMessages, trace, finalAnswer)
	synthRecord := agentmemory.Observation{
		Step:   state.Step,
		Role:   "FinalSynthesizer",
		Tool:   "synthesize_final_answer",
		Input:  agentmemory.ShortText(finalAnswer, 220),
		Output: agentmemory.ShortText(synthesized, 220),
	}
	if synthErr != nil {
		synthRecord.Status = "failed"
		synthRecord.Error = synthErr.Error()
		if state.MemorySession != nil {
			state.MemorySession.AppendObservation(synthRecord)
		}
		return "", errors.New("最终答案质量不足且兜底总结失败")
	}
	synthRecord.Status = "succeeded"
	if state.MemorySession != nil {
		state.MemorySession.AppendObservation(synthRecord)
	}
	if strings.TrimSpace(synthesized) == "" {
		return "", errors.New("最终答案质量不足且兜底总结为空")
	}
	state.FinalAnswer = strings.TrimSpace(synthesized)
	return "final_judge.done", nil
}
