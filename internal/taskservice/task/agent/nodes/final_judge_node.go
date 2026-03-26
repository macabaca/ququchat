package nodes

import (
	"context"

	"ququchat/internal/taskservice/task/agent"
)

type FinalJudgeNode struct {
	Client agent.ChatClient
}

func (n FinalJudgeNode) Name() string {
	return "final_judge"
}

func (n FinalJudgeNode) Run(ctx context.Context, state *agent.State) (next string, err error) {
	return agent.RunFinalJudgeNode(ctx, n.Client, state)
}
