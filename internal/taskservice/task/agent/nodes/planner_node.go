package nodes

import (
	"context"

	"ququchat/internal/taskservice/task/agent"
)

type PlannerNode struct {
	Client agent.ChatClient
}

func (n PlannerNode) Name() string {
	return "planner"
}

func (n PlannerNode) Run(ctx context.Context, state *agent.State) (next string, err error) {
	return agent.RunPlannerNode(ctx, n.Client, state)
}
