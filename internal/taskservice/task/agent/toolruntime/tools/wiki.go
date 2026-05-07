package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"ququchat/internal/taskservice/task/agent/toolruntime"
)

// wikiReadTool reads a file inside the wiki directory.
type wikiReadTool struct{ baseDir string }

func NewWikiReadTool(baseDir string) toolruntime.Tool { return &wikiReadTool{baseDir: baseDir} }
func (t *wikiReadTool) Name() string                 { return "wiki_read_file" }
func (t *wikiReadTool) Spec() toolruntime.ToolDescriptor {
	return toolruntime.ToolDescriptor{
		Name:        "wiki_read_file",
		Description: "读取 wiki 目录中的文件内容",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "相对于 wiki 根目录的文件路径，如 index.md 或 entities/欧冠.md"},
			},
			"required":             []string{"path"},
			"additionalProperties": false,
		},
	}
}
func (t *wikiReadTool) Validate(input string) *toolruntime.ValidationError {
	args, err := toolruntime.ParseActionInputJSONObject(input)
	if err != nil {
		return &toolruntime.ValidationError{Message: "input 必须是 JSON 对象", Detail: err.Error()}
	}
	if toolruntime.ReadStringArg(args, "path") == "" {
		return &toolruntime.ValidationError{Message: "需要 path 参数"}
	}
	return nil
}
func (t *wikiReadTool) Run(_ context.Context, input string, _ string) (string, error) {
	args, _ := toolruntime.ParseActionInputJSONObject(input)
	rel := toolruntime.ReadStringArg(args, "path")
	abs, err := safeWikiPath(t.baseDir, rel)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return "(file not found)", nil
		}
		return "", err
	}
	return string(data), nil
}

// wikiWriteTool writes a file inside the wiki directory.
type wikiWriteTool struct{ baseDir string }

func NewWikiWriteTool(baseDir string) toolruntime.Tool { return &wikiWriteTool{baseDir: baseDir} }
func (t *wikiWriteTool) Name() string                  { return "wiki_write_file" }
func (t *wikiWriteTool) Spec() toolruntime.ToolDescriptor {
	return toolruntime.ToolDescriptor{
		Name:        "wiki_write_file",
		Description: "写入或覆盖 wiki 目录中的文件",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string", "description": "相对于 wiki 根目录的文件路径"},
				"content": map[string]any{"type": "string", "description": "文件内容"},
			},
			"required":             []string{"path", "content"},
			"additionalProperties": false,
		},
	}
}
func (t *wikiWriteTool) Validate(input string) *toolruntime.ValidationError {
	args, err := toolruntime.ParseActionInputJSONObject(input)
	if err != nil {
		return &toolruntime.ValidationError{Message: "input 必须是 JSON 对象", Detail: err.Error()}
	}
	if toolruntime.ReadStringArg(args, "path") == "" {
		return &toolruntime.ValidationError{Message: "需要 path 参数"}
	}
	return nil
}
func (t *wikiWriteTool) Run(_ context.Context, input string, _ string) (string, error) {
	args, _ := toolruntime.ParseActionInputJSONObject(input)
	rel := toolruntime.ReadStringArg(args, "path")
	content := toolruntime.ReadStringArg(args, "content")
	abs, err := safeWikiPath(t.baseDir, rel)
	if err != nil {
		return "", err
	}
	cleanBase := filepath.Clean(t.baseDir)
	if abs == cleanBase {
		return "", errors.New("不能写入 wiki 根目录本身，请指定具体文件路径")
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
		return "", err
	}
	if err := os.WriteFile(abs, []byte(content), 0644); err != nil {
		return "", err
	}
	return "ok", nil
}

// wikiListTool lists files in a wiki directory as a tree.
type wikiListTool struct{ baseDir string }

func NewWikiListTool(baseDir string) toolruntime.Tool { return &wikiListTool{baseDir: baseDir} }
func (t *wikiListTool) Name() string                  { return "wiki_list_files" }
func (t *wikiListTool) Spec() toolruntime.ToolDescriptor {
	return toolruntime.ToolDescriptor{
		Name:        "wiki_list_files",
		Description: "列出 wiki 目录结构（树形），展示子目录与 .md 文件",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"dir": map[string]any{"type": "string", "description": "相对于 wiki 根目录的子目录，留空表示查看完整 wiki 结构"},
			},
			"required":             []string{},
			"additionalProperties": false,
		},
	}
}
func (t *wikiListTool) Validate(input string) *toolruntime.ValidationError { return nil }
func (t *wikiListTool) Run(_ context.Context, input string, _ string) (string, error) {
	args, _ := toolruntime.ParseActionInputJSONObject(input)
	rel := toolruntime.ReadStringArg(args, "dir")
	abs, err := safeWikiPath(t.baseDir, rel)
	if err != nil {
		return "", err
	}
	tree := buildWikiTree(abs, t.baseDir)
	if len(tree.children) == 0 && len(tree.files) == 0 {
		return "(empty)", nil
	}
	return formatWikiTree(tree, "", true), nil
}

// wikiNode represents a directory or file in the wiki tree.
type wikiNode struct {
	name     string
	isDir    bool
	children []wikiNode
	files    []string // leaf .md files directly under this node (not in subdirectories)
}

func buildWikiTree(root, baseDir string) wikiNode {
	node := wikiNode{name: filepath.Base(root), isDir: true}
	entries, err := os.ReadDir(root)
	if err != nil {
		return node
	}
	dirs := make([]os.DirEntry, 0)
	files := make([]string, 0)
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if entry.IsDir() {
			dirs = append(dirs, entry)
		} else if strings.HasSuffix(name, ".md") {
			files = append(files, name)
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name() < dirs[j].Name() })
	sort.Strings(files)
	for _, d := range dirs {
		sub := buildWikiTree(filepath.Join(root, d.Name()), baseDir)
		if len(sub.children) > 0 || len(sub.files) > 0 {
			node.children = append(node.children, sub)
		}
	}
	node.files = files
	return node
}

func formatWikiTree(node wikiNode, prefix string, isRoot bool) string {
	var b strings.Builder
	if isRoot {
		b.WriteString("wiki/\n")
	}
	subPrefix := prefix
	dirCount := len(node.children)
	for i, f := range node.files {
		if i == dirCount+len(node.files)-1 {
			b.WriteString(prefix)
			b.WriteString("└── ")
		} else {
			b.WriteString(prefix)
			b.WriteString("├── ")
		}
		b.WriteString(f)
		b.WriteString("\n")
	}
	for i, child := range node.children {
		isLast := i == len(node.children)-1
		var branch string
		if isLast {
			branch = "└── "
			subPrefix = prefix + "    "
		} else {
			branch = "├── "
			subPrefix = prefix + "│   "
		}
		b.WriteString(prefix)
		b.WriteString(branch)
		b.WriteString(child.name)
		b.WriteString("/\n")
		writeWikiTreeChildren(&b, child, subPrefix)
	}
	return strings.TrimRight(b.String(), "\n")
}

func writeWikiTreeChildren(b *strings.Builder, node wikiNode, prefix string) {
	total := len(node.children) + len(node.files)
	n := 0
	for _, f := range node.files {
		n++
		isLast := n == total
		b.WriteString(prefix)
		if isLast {
			b.WriteString("└── ")
		} else {
			b.WriteString("├── ")
		}
		b.WriteString(f)
		b.WriteString("\n")
	}
	for _, child := range node.children {
		n++
		isLast := n == total
		b.WriteString(prefix)
		var branch string
		if isLast {
			branch = "└── "
		} else {
			branch = "├── "
		}
		b.WriteString(branch)
		b.WriteString(child.name)
		b.WriteString("/\n")
		subPrefix := prefix
		if isLast {
			subPrefix = prefix + "    "
		} else {
			subPrefix = prefix + "│   "
		}
		writeWikiTreeChildren(b, child, subPrefix)
	}
}

// safeWikiPath resolves rel inside baseDir and rejects path traversal.
func safeWikiPath(baseDir, rel string) (string, error) {
	cleanBase := filepath.Clean(baseDir)
	rel = filepath.Clean(strings.TrimSpace(rel))
	if rel == "" || rel == "." {
		return cleanBase, nil
	}
	abs := filepath.Join(cleanBase, rel)
	if !strings.HasPrefix(abs, cleanBase+string(os.PathSeparator)) && abs != cleanBase {
		return "", errors.New("path traversal not allowed")
	}
	return abs, nil
}
