package types

type PlannerTask struct {
	Task string `json:"task"`
	Tool string `json:"tool"`
}

type PlannerOutline struct {
	Steps []PlannerTask `json:"steps"`
}

type Action struct {
	Tool  string `json:"tool"`
	Input string `json:"input"`
}

type Plan struct {
	Thought string `json:"thought"`
	Action  Action `json:"action"`
}
