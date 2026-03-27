package nodes

import (
	"context"

	"ququchat/internal/taskservice/task/agent"
)

type CoordinatorThinkNode struct {
	Client agent.ChatClient
}

func (n CoordinatorThinkNode) Name() string {
	return "coordinator_think"
}

func (n CoordinatorThinkNode) Run(ctx context.Context, state *agent.State) (next string, err error) {
	return agent.RunCoordinatorThinkNode(ctx, n.Client, state)
}
