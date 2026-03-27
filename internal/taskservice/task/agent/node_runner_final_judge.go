package agent

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"strconv"
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
		state.FinalAnswer = finalizeAnswerWithURLs(finalCandidate, trace, state)
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
	state.FinalAnswer = finalizeAnswerWithURLs(strings.TrimSpace(synthesized), trace, state)
	return "final_judge.done", nil
}

var urlPattern = regexp.MustCompile(`https?://[^\s<>"'\]\)]+`)

func finalizeAnswerWithURLs(finalAnswer string, trace []agentmemory.Observation, state *State) string {
	trimmedFinal := strings.TrimSpace(finalAnswer)
	if trimmedFinal == "" {
		return ""
	}
	rememberObservedURLsInState(state, trimmedFinal)
	resolved := replaceURLPlaceholdersWithState(trimmedFinal, state)
	return enrichFinalAnswerWithObservedLinks(resolved, trace, state)
}

func enrichFinalAnswerWithObservedLinks(finalAnswer string, trace []agentmemory.Observation, state *State) string {
	trimmedFinal := strings.TrimSpace(finalAnswer)
	if trimmedFinal == "" {
		return trimmedFinal
	}
	rememberObservedURLsInStateFromTrace(state, trace)
	observedURLs := collectObservedURLs(trace)
	observedURLs = mergeUniqueURLs(observedURLs, listURLsFromState(state))
	if len(observedURLs) == 0 {
		return trimmedFinal
	}
	missing := make([]string, 0, len(observedURLs))
	for _, u := range observedURLs {
		if !strings.Contains(trimmedFinal, u) {
			missing = append(missing, u)
		}
	}
	if len(missing) == 0 {
		return trimmedFinal
	}
	builder := strings.Builder{}
	builder.WriteString(trimmedFinal)
	builder.WriteString("\n\n可访问链接：")
	for i, u := range missing {
		builder.WriteString("\n")
		builder.WriteString(strconv.Itoa(i + 1))
		builder.WriteString(". ")
		builder.WriteString(u)
	}
	return strings.TrimSpace(builder.String())
}

func collectObservedURLs(trace []agentmemory.Observation) []string {
	if len(trace) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, 8)
	urls := make([]string, 0, 4)
	for _, obs := range trace {
		matches := urlPattern.FindAllString(strings.TrimSpace(obs.Output), -1)
		for _, raw := range matches {
			url := normalizeObservedURL(raw)
			if url == "" {
				continue
			}
			if _, exists := seen[url]; exists {
				continue
			}
			seen[url] = struct{}{}
			urls = append(urls, url)
		}
	}
	if len(urls) == 0 {
		return nil
	}
	return urls
}

func rememberObservedURLsInStateFromTrace(state *State, trace []agentmemory.Observation) {
	if state == nil || len(trace) == 0 {
		return
	}
	for _, obs := range trace {
		rememberObservedURLsInState(state, obs.Output)
	}
}

func rememberObservedURLsInState(state *State, text string) {
	if state == nil {
		return
	}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return
	}
	matches := urlPattern.FindAllString(trimmed, -1)
	if len(matches) == 0 {
		return
	}
	ensureURLAliasMaps(state)
	for _, raw := range matches {
		url := normalizeObservedURL(raw)
		if url == "" {
			continue
		}
		if _, exists := state.URLValueToAlias[url]; exists {
			continue
		}
		alias := "URL_" + strconv.Itoa(state.NextURLAliasIndex)
		state.NextURLAliasIndex++
		state.URLValueToAlias[url] = alias
		state.URLAliasToValue[alias] = url
		state.URLAliasOrder = append(state.URLAliasOrder, alias)
	}
}

func normalizeObservedURL(raw string) string {
	return strings.TrimSpace(strings.TrimRight(raw, ".,;:!?，。；：！？）]】》」\"'"))
}

func ensureURLAliasMaps(state *State) {
	if state == nil {
		return
	}
	if state.URLAliasToValue == nil {
		state.URLAliasToValue = map[string]string{}
	}
	if state.URLValueToAlias == nil {
		state.URLValueToAlias = map[string]string{}
	}
	if state.URLAliasOrder == nil {
		state.URLAliasOrder = make([]string, 0)
	}
	if state.NextURLAliasIndex <= 0 {
		state.NextURLAliasIndex = 1
	}
}

func listURLsFromState(state *State) []string {
	if state == nil {
		return nil
	}
	if len(state.URLAliasOrder) == 0 || len(state.URLAliasToValue) == 0 {
		return nil
	}
	urls := make([]string, 0, len(state.URLAliasOrder))
	for _, alias := range state.URLAliasOrder {
		url := strings.TrimSpace(state.URLAliasToValue[alias])
		if url == "" {
			continue
		}
		urls = append(urls, url)
	}
	if len(urls) == 0 {
		return nil
	}
	return urls
}

func mergeUniqueURLs(primary []string, secondary []string) []string {
	if len(primary) == 0 && len(secondary) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(primary)+len(secondary))
	merged := make([]string, 0, len(primary)+len(secondary))
	for _, url := range primary {
		u := strings.TrimSpace(url)
		if u == "" {
			continue
		}
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		merged = append(merged, u)
	}
	for _, url := range secondary {
		u := strings.TrimSpace(url)
		if u == "" {
			continue
		}
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		merged = append(merged, u)
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}

func replaceURLPlaceholdersWithState(finalAnswer string, state *State) string {
	trimmedFinal := strings.TrimSpace(finalAnswer)
	if trimmedFinal == "" || state == nil || len(state.URLAliasToValue) == 0 {
		return trimmedFinal
	}
	aliases := make([]string, 0, len(state.URLAliasToValue))
	for alias := range state.URLAliasToValue {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	replacerPairs := make([]string, 0, len(aliases)*6)
	for _, alias := range aliases {
		url := strings.TrimSpace(state.URLAliasToValue[alias])
		if url == "" {
			continue
		}
		replacerPairs = append(replacerPairs, alias, url)
		replacerPairs = append(replacerPairs, "{{"+alias+"}}", url)
		replacerPairs = append(replacerPairs, "[["+alias+"]]", url)
		replacerPairs = append(replacerPairs, "["+alias+"]", url)
	}
	if len(replacerPairs) == 0 {
		return trimmedFinal
	}
	replacer := strings.NewReplacer(replacerPairs...)
	return strings.TrimSpace(replacer.Replace(trimmedFinal))
}

func appendURLAliasFeedback(feedback string, state *State) string {
	base := strings.TrimSpace(feedback)
	if state == nil || len(state.URLAliasOrder) == 0 || len(state.URLAliasToValue) == 0 {
		return base
	}
	builder := strings.Builder{}
	if base != "" {
		builder.WriteString(base)
		builder.WriteString("\n")
	}
	builder.WriteString("已缓存URL键值（输出时可使用键名，系统会自动替换为真实链接）：")
	for _, alias := range state.URLAliasOrder {
		url := strings.TrimSpace(state.URLAliasToValue[alias])
		if url == "" {
			continue
		}
		builder.WriteString("\n- ")
		builder.WriteString(alias)
		builder.WriteString(" => ")
		builder.WriteString(url)
	}
	return strings.TrimSpace(builder.String())
}
