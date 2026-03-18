package tasksvc

import "context"

type LLMClient interface {
	Chat(ctx context.Context, prompt string) (string, error)
}
