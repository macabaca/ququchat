package nodes

import (
	"context"

	"ququchat/internal/taskservice/task/agent"
)

type ValidatorNode struct{}

func (n ValidatorNode) Name() string {
	return "validator"
}

func (n ValidatorNode) Run(ctx context.Context, state *agent.State) (next string, err error) {
	return agent.RunValidatorNode(ctx, state)
}
