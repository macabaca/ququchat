package routing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	agenttypes "ququchat/internal/taskservice/task/agent/types"
)

type Node interface {
	Name() string
	Run(ctx context.Context, state *agenttypes.State) (event string, err error)
}

type NodeFunc func(ctx context.Context, state *agenttypes.State) (event string, err error)

type FuncNode struct {
	name string
	run  NodeFunc
}

func NewFuncNode(name string, run NodeFunc) FuncNode {
	return FuncNode{name: strings.TrimSpace(name), run: run}
}

func (n FuncNode) Name() string {
	return n.name
}

func (n FuncNode) Run(ctx context.Context, state *agenttypes.State) (string, error) {
	if n.run == nil {
		return "", errors.New("node run func is nil")
	}
	return n.run(ctx, state)
}

type Transition struct {
	From  string
	Event string
	To    string
	Guard func(state *agenttypes.State) bool
}

type Router struct {
	nodes       map[string]Node
	transitions []Transition
}

func NewRouter(nodes []Node, transitions []Transition) *Router {
	nodeMap := make(map[string]Node, len(nodes))
	for _, node := range nodes {
		name := strings.TrimSpace(node.Name())
		if name == "" {
			continue
		}
		nodeMap[name] = node
	}
	return &Router{
		nodes:       nodeMap,
		transitions: append([]Transition(nil), transitions...),
	}
}

func (r *Router) Node(name string) (Node, bool) {
	if r == nil {
		return nil, false
	}
	node, ok := r.nodes[strings.TrimSpace(name)]
	return node, ok
}

func (r *Router) Resolve(from string, event string, state *agenttypes.State) (string, bool) {
	if r == nil {
		return "", false
	}
	from = strings.TrimSpace(from)
	event = strings.TrimSpace(event)
	for _, transition := range r.transitions {
		if strings.TrimSpace(transition.From) != from {
			continue
		}
		if strings.TrimSpace(transition.Event) != event {
			continue
		}
		if transition.Guard != nil && !transition.Guard(state) {
			continue
		}
		return strings.TrimSpace(transition.To), true
	}
	return "", false
}

type Policy interface {
	InitialNode() string
	Decide(state *agenttypes.State, from string, event string, runErr error, resolve func(from string, event string, state *agenttypes.State) (string, bool)) (next string, stop bool, err error)
}

type Service struct {
	router *Router
	policy Policy
}

type nodeCallLogRecord struct {
	Timestamp  string         `json:"timestamp"`
	Node       string         `json:"node"`
	Event      string         `json:"event"`
	RunError   string         `json:"run_error,omitempty"`
	PolicyErr  string         `json:"policy_error,omitempty"`
	NextNode   string         `json:"next_node,omitempty"`
	Stop       bool           `json:"stop"`
	DurationMs int64          `json:"duration_ms"`
	Input      map[string]any `json:"input"`
	Output     map[string]any `json:"output"`
}

var nodeCallLogMu sync.Mutex

func NewService(router *Router, policy Policy) *Service {
	return &Service{
		router: router,
		policy: policy,
	}
}

func (s *Service) Run(ctx context.Context, state *agenttypes.State) error {
	if s == nil || s.router == nil || s.policy == nil {
		return errors.New("routing service is not configured")
	}
	if state == nil {
		return errors.New("routing state is nil")
	}
	if strings.TrimSpace(state.CurrentNode) == "" {
		state.CurrentNode = strings.TrimSpace(s.policy.InitialNode())
	}
	if state.Retry == nil {
		state.Retry = make(map[string]int)
	}
	for {
		if state.Done {
			return nil
		}
		if state.Failed {
			reason := strings.TrimSpace(state.FailReason)
			if reason == "" {
				reason = "workflow failed"
			}
			return errors.New(reason)
		}
		current := strings.TrimSpace(state.CurrentNode)
		node, ok := s.router.Node(current)
		if !ok {
			state.Failed = true
			state.FailReason = fmt.Sprintf("node not found: %s", current)
			return errors.New(state.FailReason)
		}
		before := snapshotState(state)
		start := time.Now()
		event, runErr := node.Run(ctx, state)
		next, stop, decisionErr := s.policy.Decide(state, current, event, runErr, s.router.Resolve)
		record := nodeCallLogRecord{
			Timestamp:  time.Now().Format(time.RFC3339Nano),
			Node:       current,
			Event:      strings.TrimSpace(event),
			NextNode:   strings.TrimSpace(next),
			Stop:       stop,
			DurationMs: time.Since(start).Milliseconds(),
			Input:      before,
			Output:     snapshotState(state),
		}
		if runErr != nil {
			record.RunError = runErr.Error()
		}
		if decisionErr != nil {
			record.PolicyErr = decisionErr.Error()
		}
		appendNodeCallLog(record)
		if decisionErr != nil {
			return decisionErr
		}
		if stop {
			if state.Done {
				return nil
			}
			if state.Failed {
				reason := strings.TrimSpace(state.FailReason)
				if reason == "" {
					reason = "workflow failed"
				}
				return errors.New(reason)
			}
			return nil
		}
		state.CurrentNode = strings.TrimSpace(next)
	}
}

func appendNodeCallLog(record nodeCallLogRecord) {
	path := strings.TrimSpace(os.Getenv("AGENT_NODE_LOG_PATH"))
	if path == "" {
		path = defaultNodeLogPath()
	}
	payload, err := json.Marshal(record)
	if err != nil {
		log.Printf("agent node log marshal failed: %v", err)
		return
	}
	nodeCallLogMu.Lock()
	defer nodeCallLogMu.Unlock()
	parentDir := strings.TrimSpace(filepath.Dir(path))
	if parentDir != "" && parentDir != "." {
		if mkdirErr := os.MkdirAll(parentDir, 0755); mkdirErr != nil {
			log.Printf("agent node log mkdir failed path=%s err=%v", parentDir, mkdirErr)
			return
		}
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("agent node log open failed path=%s err=%v", path, err)
		return
	}
	defer file.Close()
	if _, err = file.Write(append(payload, '\n')); err != nil {
		log.Printf("agent node log write failed path=%s err=%v", path, err)
	}
}

func defaultNodeLogPath() string {
	return "/root/lzy/ququchat/agent_node_calls.jsonl"
}

func snapshotState(state *agenttypes.State) map[string]any {
	if state == nil {
		return map[string]any{}
	}
	out := map[string]any{
		"goal":                 state.Goal,
		"room_id":              state.RoomID,
		"recent_messages":      append([]string(nil), state.RecentMessages...),
		"outline":              state.Outline,
		"outline_index":        state.OutlineIndex,
		"current_task":         state.CurrentTask,
		"coordinator_raw":      state.CoordinatorRaw,
		"formatted_raw":        state.FormattedRaw,
		"plan":                 state.Plan,
		"tool_name":            state.ToolName,
		"action_input":         state.ActionInput,
		"tool_output":          state.ToolOutput,
		"tool_error":           state.ToolError,
		"final_answer":         state.FinalAnswer,
		"final_review":         state.FinalReview,
		"feedback":             state.Feedback,
		"available_tool_specs": append([]agenttypes.ToolSpec(nil), state.AvailableToolSpecs...),
		"current_node":         state.CurrentNode,
		"last_event":           state.LastEvent,
		"retry":                cloneRetryMap(state.Retry),
		"step":                 state.Step,
		"max_steps":            state.MaxSteps,
		"done":                 state.Done,
		"failed":               state.Failed,
		"fail_reason":          state.FailReason,
	}
	if state.MemorySession != nil {
		out["memory_feedback"] = state.MemorySession.BuildFeedback()
		out["memory_trace"] = state.MemorySession.Trace()
	}
	return out
}

func cloneRetryMap(in map[string]int) map[string]int {
	if len(in) == 0 {
		return map[string]int{}
	}
	out := make(map[string]int, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
