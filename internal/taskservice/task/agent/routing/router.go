package routing

import (
	"context"
	"errors"
	"fmt"
	"strings"

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
		event, runErr := node.Run(ctx, state)
		next, stop, decisionErr := s.policy.Decide(state, current, event, runErr, s.router.Resolve)
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
