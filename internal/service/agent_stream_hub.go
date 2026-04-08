package taskservice

import (
	"strings"
	"sync"
	"time"
)

type AgentStreamEvent struct {
	EventID          string                 `json:"event_id"`
	EventType        string                 `json:"event_type"`
	RequestID        string                 `json:"request_id,omitempty"`
	TaskID           string                 `json:"task_id,omitempty"`
	RoomID           string                 `json:"room_id,omitempty"`
	UserID           string                 `json:"user_id,omitempty"`
	Step             int                    `json:"step,omitempty"`
	TS               int64                  `json:"ts"`
	Role             string                 `json:"role,omitempty"`
	Tool             string                 `json:"tool,omitempty"`
	Status           string                 `json:"status,omitempty"`
	Content          string                 `json:"content,omitempty"`
	Error            string                 `json:"error,omitempty"`
	TokenUsage       map[string]int         `json:"token_usage,omitempty"`
	ParentMessageID  string                 `json:"parent_message_id,omitempty"`
	ParentSequenceID int64                  `json:"parent_sequence_id,omitempty"`
	Payload          map[string]interface{} `json:"payload,omitempty"`
}

type AgentStreamHub struct {
	mu          sync.RWMutex
	subscribers map[string]map[chan AgentStreamEvent]struct{}
	recent      map[string][]AgentStreamEvent
	nextID      uint64
}

func NewAgentStreamHub() *AgentStreamHub {
	return &AgentStreamHub{
		subscribers: map[string]map[chan AgentStreamEvent]struct{}{},
		recent:      map[string][]AgentStreamEvent{},
	}
}

func (h *AgentStreamHub) Subscribe(requestID string) (<-chan AgentStreamEvent, []AgentStreamEvent, func()) {
	trimmedRequestID := strings.TrimSpace(requestID)
	ch := make(chan AgentStreamEvent, 64)
	if h == nil || trimmedRequestID == "" {
		return ch, nil, func() { close(ch) }
	}
	h.mu.Lock()
	if _, ok := h.subscribers[trimmedRequestID]; !ok {
		h.subscribers[trimmedRequestID] = map[chan AgentStreamEvent]struct{}{}
	}
	h.subscribers[trimmedRequestID][ch] = struct{}{}
	replay := append([]AgentStreamEvent(nil), h.recent[trimmedRequestID]...)
	h.mu.Unlock()
	unsubscribe := func() {
		h.mu.Lock()
		if subs, ok := h.subscribers[trimmedRequestID]; ok {
			delete(subs, ch)
			if len(subs) == 0 {
				delete(h.subscribers, trimmedRequestID)
			}
		}
		h.mu.Unlock()
		close(ch)
	}
	return ch, replay, unsubscribe
}

func (h *AgentStreamHub) Publish(event AgentStreamEvent) {
	if h == nil {
		return
	}
	trimmedRequestID := strings.TrimSpace(event.RequestID)
	if trimmedRequestID == "" {
		return
	}
	h.mu.Lock()
	h.nextID++
	if strings.TrimSpace(event.EventID) == "" {
		event.EventID = "evt_" + strings.TrimSpace(time.Unix(0, 0).Add(time.Duration(h.nextID)).Format("150405.000000000"))
	}
	event.RequestID = trimmedRequestID
	if event.TS <= 0 {
		event.TS = time.Now().Unix()
	}
	h.recent[trimmedRequestID] = append(h.recent[trimmedRequestID], event)
	if len(h.recent[trimmedRequestID]) > 200 {
		h.recent[trimmedRequestID] = append([]AgentStreamEvent(nil), h.recent[trimmedRequestID][len(h.recent[trimmedRequestID])-200:]...)
	}
	subs := make([]chan AgentStreamEvent, 0, len(h.subscribers[trimmedRequestID]))
	for ch := range h.subscribers[trimmedRequestID] {
		subs = append(subs, ch)
	}
	h.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- event:
		default:
		}
	}
}
