package nodes

import (
	"context"

	"ququchat/internal/taskservice/task/agent"
)

type ExecutorNode struct{}

func (n ExecutorNode) Name() string {
	return "executor"
}

func (n ExecutorNode) Run(ctx context.Context, state *agent.State) (next string, err error) {
	return agent.RunExecutorNode(ctx, state)
}
