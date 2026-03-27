package nodes

import (
	"context"

	"ququchat/internal/taskservice/task/agent"
)

type CoordinatorActNode struct {
	Client agent.ChatClient
}

func (n CoordinatorActNode) Name() string {
	return "coordinator_act"
}

func (n CoordinatorActNode) Run(ctx context.Context, state *agent.State) (next string, err error) {
	return agent.RunCoordinatorActNode(ctx, n.Client, state)
}
