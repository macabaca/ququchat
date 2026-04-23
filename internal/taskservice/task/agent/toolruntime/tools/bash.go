package tools

import (
	"context"
	"errors"
	"os/exec"
	"strings"

	"ququchat/internal/taskservice/task/agent/toolruntime"
)

// Dangerous command patterns that are always blocked
var dangerousPatterns = []string{
	"rm -rf /",
	"rm -rf /*",
	"mkfs",
	"dd if=",
	"> /dev/",
	"curl | sh",
	"wget | sh",
	"curl | bash",
	"wget | bash",
	":(){ :|:& };:",
	"chmod 777",
	"chmod -R 777",
}

type bashTool struct {
	allowedCommands []string // whitelist patterns
}

// NewBashTool creates a bash tool with optional whitelist.
// If allowedCommands is empty, all non-dangerous commands are allowed.
func NewBashTool(allowedCommands []string) toolruntime.Tool {
	return &bashTool{allowedCommands: allowedCommands}
}

func (t *bashTool) Name() string { return "bash" }

func (t *bashTool) Spec() toolruntime.ToolDescriptor {
	return toolruntime.ToolDescriptor{
		Name:        "bash",
		Description: "执行 bash 命令",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{"type": "string", "description": "要执行的命令"},
			},
			"required":             []string{"command"},
			"additionalProperties": false,
		},
	}
}

func (t *bashTool) Validate(input string) *toolruntime.ValidationError {
	args, err := toolruntime.ParseActionInputJSONObject(input)
	if err != nil {
		return &toolruntime.ValidationError{Message: "action.input 必须是 JSON 对象字符串", Detail: err.Error()}
	}
	cmd := toolruntime.ReadStringArg(args, "command")
	if cmd == "" {
		return &toolruntime.ValidationError{Message: "bash 需要 input.command", Detail: "示例: {\"command\":\"ls -la\"}"}
	}
	if err := t.checkSafety(cmd); err != nil {
		return &toolruntime.ValidationError{Message: "命令被安全策略拒绝", Detail: err.Error()}
	}
	return nil
}

func (t *bashTool) Run(ctx context.Context, input string, _ string) (string, error) {
	args, err := toolruntime.ParseActionInputJSONObject(input)
	if err != nil {
		return "", err
	}
	cmd := toolruntime.ReadStringArg(args, "command")
	if cmd == "" {
		return "", errors.New("bash requires input.command")
	}
	if err := t.checkSafety(cmd); err != nil {
		return "", err
	}
	out, err := exec.CommandContext(ctx, "bash", "-c", cmd).CombinedOutput()
	if err != nil {
		return string(out), err
	}
	return string(out), nil
}

func (t *bashTool) checkSafety(cmd string) error {
	lower := strings.ToLower(cmd)
	for _, pattern := range dangerousPatterns {
		if strings.Contains(lower, pattern) {
			return errors.New("dangerous command blocked: " + pattern)
		}
	}
	if len(t.allowedCommands) == 0 {
		return nil
	}
	for _, allowed := range t.allowedCommands {
		if matchPattern(cmd, allowed) {
			return nil
		}
	}
	return errors.New("command not in whitelist")
}

// matchPattern checks if cmd matches the pattern (supports * wildcard).
func matchPattern(cmd, pattern string) bool {
	if pattern == "*" {
		return true
	}
	if !strings.Contains(pattern, "*") {
		return strings.HasPrefix(cmd, pattern)
	}
	parts := strings.Split(pattern, "*")
	idx := 0
	for i, part := range parts {
		if part == "" {
			continue
		}
		pos := strings.Index(cmd[idx:], part)
		if pos < 0 {
			return false
		}
		if i == 0 && pos != 0 {
			return false
		}
		idx += pos + len(part)
	}
	return true
}
