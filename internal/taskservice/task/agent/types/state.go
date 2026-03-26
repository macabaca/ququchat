package types

import (
	agentmemory "ququchat/internal/taskservice/task/agent/memory"
	"ququchat/internal/taskservice/task/agent/toolruntime"
)

type DomainState struct {
	Goal               string
	RoomID             string
	RecentMessages     []string
	Outline            PlannerOutline
	OutlineIndex       int
	CurrentTask        string
	CoordinatorRaw     string
	FormattedRaw       string
	Plan               Plan
	ToolName           string
	ActionInput        string
	ToolOutput         string
	ToolError          string
	FinalAnswer        string
	FinalReview        FinalReviewResult
	Feedback           string
	AvailableToolSpecs []ToolSpec
	MemorySession      agentmemory.Session
	ToolRuntime        toolruntime.Runtime
}

type ControlState struct {
	CurrentNode string
	LastEvent   string
	Retry       map[string]int
	Step        int
	MaxSteps    int
	Done        bool
	Failed      bool
	FailReason  string
}

type State struct {
	DomainState
	ControlState
}
