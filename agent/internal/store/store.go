package store

import "ququchat/agent/internal/core/task"

type Store interface {
	Create(t *task.Task) error
	Get(taskID string) (*task.Task, bool)
	MarkRunning(taskID string) (*task.Task, error)
	MarkSucceeded(taskID string, result task.Result) (*task.Task, error)
	MarkFailed(taskID string, message string) (*task.Task, error)
}
