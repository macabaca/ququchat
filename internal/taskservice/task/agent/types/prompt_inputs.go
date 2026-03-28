package types

type CoordinatorPromptInput struct {
	Goal               string
	RealtimeGuidance   string
	AgentIdentity      string
	ToolSection        string
	FunctionToolsJSON  string
	RuleLines          []string
	Step               int
	MaxSteps           int
	OutlineText        string
	CurrentTask        string
	CurrentThought     string
	Feedback           string
	RecentMessageCount int
}

type FinalJudgePromptInput struct {
	Goal               string
	Candidate          string
	RecentMessagesText string
	TraceText          string
}

type FinalSynthesizerPromptInput struct {
	Goal               string
	Candidate          string
	RecentMessagesText string
	TraceText          string
}

type ToolResponsePromptInput struct {
	Goal             string
	ToolName         string
	ToolInput        string
	ToolRawOutput    string
	RealtimeGuidance string
}

type TerminationGuardPromptInput struct {
	Goal               string
	RecentMessagesText string
	TraceText          string
	ToolOutputRaw      string
	ToolOutputSummary  string
}
