package wikiagent

import (
	"context"
	_ "embed"
	"strings"

	agentmemory "ququchat/internal/taskservice/task/agent/memory"
	taskagent "ququchat/internal/taskservice/task/agent"
)

//go:embed QUERY_SCHEMA.md
var querySchema string

// RunQuery runs a read-only wiki agent that synthesizes relevant context for a goal.
func RunQuery(ctx context.Context, client taskagent.ChatClient, wikiDir, goal string) string {
	if wikiDir == "" {
		return ""
	}
	goalText := querySchema + "\n\n## 查询目标\n\n" + goal
	report, err := taskagent.Execute(ctx, client, taskagent.Input{
		Goal:             goalText,
		MaxSteps:         8,
		WikiDir:          wikiDir,
		WikiReadOnlyMode: true,
	})
	if err != nil {
		return ""
	}
	final, _ := agentmemory.SplitLegacyReport(report)
	return strings.TrimSpace(final)
}
