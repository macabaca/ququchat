package services

import (
	"strconv"
	"strings"

	agentmemory "ququchat/internal/taskservice/task/agent/memory"
	agenttypes "ququchat/internal/taskservice/task/agent/types"
)

func BuildFinalRetryFeedback(goal string, candidate string, review agenttypes.FinalReviewResult) string {
	builder := strings.Builder{}
	builder.WriteString("上一轮final疑似跑题，需要重新规划。\n")
	builder.WriteString("目标：")
	builder.WriteString(strings.TrimSpace(goal))
	builder.WriteString("\n")
	builder.WriteString("候选final：")
	builder.WriteString(strings.TrimSpace(candidate))
	builder.WriteString("\n")
	builder.WriteString("质量评估：")
	builder.WriteString(BuildFinalReviewErrorText(review))
	builder.WriteString("\n")
	builder.WriteString("请仅检查是否跑题。若不跑题，下一步直接调用 finish。")
	return builder.String()
}

func BuildFinalReviewErrorText(review agenttypes.FinalReviewResult) string {
	builder := strings.Builder{}
	builder.WriteString("score=")
	builder.WriteString(strconv.Itoa(review.Score))
	builder.WriteString(", pass=")
	if review.Pass {
		builder.WriteString("true")
	} else {
		builder.WriteString("false")
	}
	if len(review.Issues) > 0 {
		builder.WriteString(", issues=")
		builder.WriteString(strings.Join(review.Issues, "；"))
	}
	return builder.String()
}

func FormatFinalReviewOutput(review agenttypes.FinalReviewResult) string {
	builder := strings.Builder{}
	builder.WriteString("score=")
	builder.WriteString(strconv.Itoa(review.Score))
	builder.WriteString(", pass=")
	if review.Pass {
		builder.WriteString("true")
	} else {
		builder.WriteString("false")
	}
	if strings.TrimSpace(review.BetterFinal) != "" {
		builder.WriteString(", better_final=")
		builder.WriteString(agentmemory.ShortText(strings.TrimSpace(review.BetterFinal), 120))
	}
	return builder.String()
}
