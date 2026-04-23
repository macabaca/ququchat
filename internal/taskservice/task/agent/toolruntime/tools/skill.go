package tools

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"ququchat/internal/taskservice/task/agent/toolruntime"
)

type SkillMode string

const (
	SkillModeOutline SkillMode = "outline"
	SkillModeContext SkillMode = "context"
)

type SkillOutline struct {
	Steps []SkillStep
}

type SkillStep struct {
	Task string
	Tool string
}

type SkillFile struct {
	Mode         SkillMode
	Content      string
	AllowedTools []string // from frontmatter allowed_tools
	Dir          string   // absolute path to the skill's directory
}

type skillTool struct {
	skillsDir string
}

func NewSkillTool(skillsDir string) toolruntime.Tool {
	if strings.TrimSpace(skillsDir) == "" {
		home, _ := os.UserHomeDir()
		skillsDir = filepath.Join(home, "lzy", "skills")
	}
	return &skillTool{skillsDir: skillsDir}
}

func (t *skillTool) Name() string { return "skill" }

func (t *skillTool) Spec() toolruntime.ToolDescriptor {
	return toolruntime.ToolDescriptor{
		Name:        "skill",
		Description: "执行本地 skill 文件定义的任务流程",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string", "description": "skill 名称（不含 .md 后缀）"},
				"args": map[string]any{"type": "string", "description": "传给 skill 的参数，替换 $ARGUMENTS"},
			},
			"required":             []string{"name"},
			"additionalProperties": false,
		},
	}
}

func (t *skillTool) Validate(input string) *toolruntime.ValidationError {
	args, err := toolruntime.ParseActionInputJSONObject(input)
	if err != nil {
		return &toolruntime.ValidationError{Message: "action.input 必须是 JSON 对象字符串", Detail: err.Error()}
	}
	if toolruntime.ReadStringArg(args, "name") == "" {
		return &toolruntime.ValidationError{Message: "skill 需要 input.name", Detail: "示例: {\"name\":\"summarize\"}"}
	}
	return nil
}

func (t *skillTool) Run(_ context.Context, input string, _ string) (string, error) {
	args, err := toolruntime.ParseActionInputJSONObject(input)
	if err != nil {
		return "", err
	}
	name := toolruntime.ReadStringArg(args, "name")
	sf, err := t.LoadSkillFile(name)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("skill %q loaded (mode=%s)", name, sf.Mode), nil
}

// LoadSkillFile reads and parses the skill markdown file.
func (t *skillTool) LoadSkillFile(name string) (*SkillFile, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("skill name is empty")
	}
	skillDir := filepath.Join(t.skillsDir, name)
	data, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	if err != nil {
		return nil, fmt.Errorf("skill %q not found: %w", name, err)
	}
	sf, err := parseSkillFile(string(data))
	if err != nil {
		return nil, err
	}
	sf.Dir = skillDir
	return sf, nil
}

// LoadSkillOutline parses outline-mode steps from the skill file.
func (t *skillTool) LoadSkillOutline(name string) (*SkillOutline, error) {
	sf, err := t.LoadSkillFile(name)
	if err != nil {
		return nil, err
	}
	return parseSkillOutline(sf.Content)
}

// LoadSkillContext returns the skill body for context-injection mode,
// with $ARGUMENTS replaced by args.
func (t *skillTool) LoadSkillContext(name, args string) (string, error) {
	sf, err := t.LoadSkillFile(name)
	if err != nil {
		return "", err
	}
	body := sf.Content
	if strings.Contains(body, "$ARGUMENTS") {
		body = strings.ReplaceAll(body, "$ARGUMENTS", strings.TrimSpace(args))
	} else if strings.TrimSpace(args) != "" {
		body = body + "\n\n" + strings.TrimSpace(args)
	}
	return body, nil
}

func parseSkillFile(content string) (*SkillFile, error) {
	mode := SkillModeOutline
	body, fm := splitFrontmatter(content)
	var allowedTools []string
	if v, ok := fm["mode"]; ok {
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "context":
			mode = SkillModeContext
		}
	}
	if v, ok := fm["allowed_tools"]; ok {
		for _, t := range strings.Split(v, ",") {
			if s := strings.TrimSpace(t); s != "" {
				allowedTools = append(allowedTools, s)
			}
		}
	}
	return &SkillFile{Mode: mode, Content: body, AllowedTools: allowedTools}, nil
}

// splitFrontmatter returns (body, frontmatterKV).
func splitFrontmatter(content string) (string, map[string]string) {
	s := strings.TrimSpace(content)
	fm := map[string]string{}
	if !strings.HasPrefix(s, "---") {
		return s, fm
	}
	rest := s[3:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return s, fm
	}
	fmBlock := rest[:idx]
	body := strings.TrimSpace(rest[idx+4:])
	scanner := bufio.NewScanner(strings.NewReader(fmBlock))
	for scanner.Scan() {
		line := scanner.Text()
		if i := strings.IndexByte(line, ':'); i > 0 {
			k := strings.TrimSpace(line[:i])
			v := strings.TrimSpace(line[i+1:])
			fm[k] = v
		}
	}
	return body, fm
}

func parseSkillOutline(body string) (*SkillOutline, error) {
	var steps []SkillStep
	inSteps := false
	var cur SkillStep

	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		trimmed := strings.TrimSpace(scanner.Text())
		if strings.EqualFold(trimmed, "## steps") || strings.HasPrefix(strings.ToLower(trimmed), "## steps") {
			inSteps = true
			continue
		}
		if inSteps && strings.HasPrefix(trimmed, "## ") {
			break
		}
		if !inSteps {
			continue
		}
		if strings.HasPrefix(trimmed, "- task:") {
			if cur.Task != "" && cur.Tool != "" {
				steps = append(steps, cur)
			}
			cur = SkillStep{Task: strings.TrimSpace(strings.TrimPrefix(trimmed, "- task:"))}
		} else if strings.HasPrefix(trimmed, "tool:") {
			cur.Tool = strings.TrimSpace(strings.TrimPrefix(trimmed, "tool:"))
		}
	}
	if cur.Task != "" && cur.Tool != "" {
		steps = append(steps, cur)
	}
	if len(steps) == 0 {
		return nil, errors.New("skill file has no valid steps")
	}
	return &SkillOutline{Steps: steps}, nil
}

// SkillToolTyped exposes skill loading methods for the executor node.
type SkillToolTyped interface {
	toolruntime.Tool
	LoadSkillFile(name string) (*SkillFile, error)
	LoadSkillOutline(name string) (*SkillOutline, error)
	LoadSkillContext(name, args string) (string, error)
}
