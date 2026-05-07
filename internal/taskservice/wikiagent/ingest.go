package wikiagent

import (
	"context"
	_ "embed"
	"strings"
	"time"

	taskagent "ququchat/internal/taskservice/task/agent"
)

//go:embed SCHEMA.md
var schema string

const (
	ingestRetryCount = 3
	ingestRetryDelay = 10 * time.Second
)

// RunIngest runs a wiki maintenance agent for the given wiki directory.
func RunIngest(ctx context.Context, client taskagent.ChatClient, wikiDir, conv string) error {
	goal := schema + "\n\n## 本次任务\n\n范围：群聊\n\n对话内容：\n" + conv
	var err error
	for i := 0; i < ingestRetryCount; i++ {
		if i > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(ingestRetryDelay):
			}
		}
		_, err = taskagent.Execute(ctx, client, taskagent.Input{
			Goal:         goal,
			MaxSteps:     12,
			WikiDir:      wikiDir,
			WikiOnlyMode: true,
		})
		if err == nil || isNoAnswerErr(err) {
			return nil
		}
		if !isTimeoutErr(err) {
			return err
		}
	}
	return err
}

func isNoAnswerErr(err error) bool {
	return strings.Contains(err.Error(), "未生成最终答案") || strings.Contains(err.Error(), "no final answer")
}

func isTimeoutErr(err error) bool {
	return strings.Contains(err.Error(), "timeout")
}
