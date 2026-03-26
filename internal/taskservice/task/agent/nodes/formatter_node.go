package nodes

import (
	"context"

	"ququchat/internal/taskservice/task/agent"
)

type FormatterNode struct {
	Client agent.ChatClient
}

func (n FormatterNode) Name() string {
	return "formatter"
}

func (n FormatterNode) Run(ctx context.Context, state *agent.State) (next string, err error) {
	return agent.RunFormatterNode(ctx, n.Client, state)
}
