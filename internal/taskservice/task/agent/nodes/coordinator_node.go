package nodes

import (
	"context"

	"ququchat/internal/taskservice/task/agent"
)

type CoordinatorNode struct {
	Client agent.ChatClient
}

func (n CoordinatorNode) Name() string {
	return "coordinator"
}

func (n CoordinatorNode) Run(ctx context.Context, state *agent.State) (next string, err error) {
	return agent.RunCoordinatorNode(ctx, n.Client, state)
}
