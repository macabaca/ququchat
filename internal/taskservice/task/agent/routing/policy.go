package routing

import (
	"errors"
	"fmt"
	"strings"

	agenttypes "ququchat/internal/taskservice/task/agent/types"
)

type AgentPolicy struct {
	entryNode      string
	retryNodeLimit map[string]int
}

func NewAgentPolicy(entryNode string, retryNodeLimit map[string]int) *AgentPolicy {
	limitCopy := make(map[string]int, len(retryNodeLimit))
	for key, value := range retryNodeLimit {
		limitCopy[strings.TrimSpace(key)] = value
	}
	return &AgentPolicy{
		entryNode:      strings.TrimSpace(entryNode),
		retryNodeLimit: limitCopy,
	}
}

func (p *AgentPolicy) InitialNode() string {
	if p == nil {
		return ""
	}
	return p.entryNode
}

func (p *AgentPolicy) Decide(state *agenttypes.State, from string, event string, runErr error, resolve func(from string, event string, state *agenttypes.State) (string, bool)) (string, bool, error) {
	if state == nil {
		return "", true, errors.New("routing state is nil")
	}
	from = strings.TrimSpace(from)
	event = strings.TrimSpace(event)
	state.LastEvent = event
	if state.Retry == nil {
		state.Retry = make(map[string]int)
	}
	if runErr != nil {
		state.Failed = true
		state.FailReason = runErr.Error()
		return "", true, runErr
	}
	if event == "" {
		state.Failed = true
		state.FailReason = fmt.Sprintf("node %s returned empty event", from)
		return "", true, errors.New(state.FailReason)
	}
	if limit, exists := p.retryNodeLimit[from]; exists && limit > 0 {
		if strings.HasSuffix(event, ".retry") {
			state.Retry[from]++
			if state.Retry[from] > limit {
				state.Failed = true
				state.FailReason = fmt.Sprintf("%s event retry exceeds limit(%d)", from, limit)
				return "", true, errors.New(state.FailReason)
			}
		} else {
			state.Retry[from] = 0
		}
	}
	next, ok := resolve(from, event, state)
	if !ok {
		state.Failed = true
		state.FailReason = fmt.Sprintf("no transition: %s + %s", from, event)
		return "", true, errors.New(state.FailReason)
	}
	if strings.EqualFold(next, "end") {
		state.Done = true
		return "", true, nil
	}
	if shouldAdvanceStep(from, event, next) {
		if state.Step <= 0 {
			state.Step = 1
		} else {
			state.Step++
		}
		if state.MaxSteps > 0 && state.Step > state.MaxSteps {
			state.Failed = true
			state.FailReason = "达到最大循环次数仍未完成"
			return "", true, errors.New(state.FailReason)
		}
	}
	return next, false, nil
}

func shouldAdvanceStep(from string, event string, next string) bool {
	from = strings.TrimSpace(from)
	event = strings.TrimSpace(event)
	next = strings.TrimSpace(next)
	if from == "planner" && event == "planner.done" && next == "coordinator" {
		return true
	}
	if from == "executor" && next == "coordinator" {
		return true
	}
	if from == "final_judge" && event == "final_judge.retry" && next == "coordinator" {
		return true
	}
	return false
}
