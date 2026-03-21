package agent

import (
	"strconv"
	"strings"
)

func buildToolFeedback(toolName string, input string, toolOutput string, specs []ToolSpec) string {
	normalizedTool := normalizeToolFromSpecs(specs, toolName)
	trimmedOutput := strings.TrimSpace(toolOutput)
	switch normalizedTool {
	case "read_recent_messages":
		count := countNumberedLines(trimmedOutput)
		builder := strings.Builder{}
		builder.WriteString("上一轮工具调用 read_recent_messages。")
		if count > 0 {
			builder.WriteString("读取了最近")
			builder.WriteString(strconv.Itoa(count))
			builder.WriteString("条消息。")
		} else if strings.TrimSpace(input) != "" {
			builder.WriteString("按请求读取最近消息。")
		}
		if trimmedOutput == "" {
			builder.WriteString("原始文本为空。")
		} else {
			builder.WriteString("原始文本如下：\n")
			builder.WriteString(trimmedOutput)
		}
		return strings.TrimSpace(builder.String())
	case "search_rag":
		segments := parseSearchRAGSegments(trimmedOutput)
		builder := strings.Builder{}
		builder.WriteString("上一轮工具调用 search_rag。")
		if len(segments) == 0 {
			builder.WriteString("未检索到相关历史消息。")
		} else {
			builder.WriteString("检索到最相关的")
			builder.WriteString(strconv.Itoa(len(segments)))
			builder.WriteString("段消息。")
			for i, seg := range segments {
				builder.WriteString("\n第")
				builder.WriteString(strconv.Itoa(i + 1))
				builder.WriteString("段中：")
				builder.WriteString(seg)
			}
		}
		if trimmedOutput == "" {
			builder.WriteString("\n原始文本为空。")
		} else {
			builder.WriteString("\n原始文本如下：\n")
			builder.WriteString(trimmedOutput)
		}
		return strings.TrimSpace(builder.String())
	case "generate_image":
		builder := strings.Builder{}
		builder.WriteString("上一轮工具调用 generate_image。")
		if trimmedOutput == "" {
			builder.WriteString("生成结果为空。")
		} else {
			builder.WriteString("生成结果如下：\n")
			builder.WriteString(trimmedOutput)
		}
		return strings.TrimSpace(builder.String())
	default:
		if trimmedOutput == "" {
			return "上一轮工具输出为空"
		}
		return "上一轮工具输出：" + trimmedOutput
	}
}

func countNumberedLines(text string) int {
	if strings.TrimSpace(text) == "" {
		return 0
	}
	lines := strings.Split(text, "\n")
	count := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		dot := strings.Index(trimmed, ".")
		if dot <= 0 {
			continue
		}
		if _, err := strconv.Atoi(strings.TrimSpace(trimmed[:dot])); err == nil {
			count++
		}
	}
	return count
}

func parseSearchRAGSegments(output string) []string {
	if strings.TrimSpace(output) == "" {
		return nil
	}
	lines := strings.Split(output, "\n")
	segments := make([]string, 0, len(lines))
	current := ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		dot := strings.Index(trimmed, ".")
		if dot <= 0 {
			if current != "" {
				current = current + "\n" + trimmed
			}
			continue
		}
		if _, err := strconv.Atoi(strings.TrimSpace(trimmed[:dot])); err != nil {
			if current != "" {
				current = current + "\n" + trimmed
			}
			continue
		}
		if current != "" {
			segments = append(segments, current)
		}
		content := strings.TrimSpace(trimmed[dot+1:])
		text := extractKVValue(content, "text=")
		if text == "" {
			text = content
		}
		text = strings.TrimSpace(text)
		if text != "" {
			current = text
		} else {
			current = ""
		}
	}
	if current != "" {
		segments = append(segments, current)
	}
	return segments
}

func extractKVValue(line string, key string) string {
	idx := strings.Index(line, key)
	if idx < 0 {
		return ""
	}
	part := line[idx+len(key):]
	next := strings.Index(part, ", ")
	if next >= 0 {
		part = part[:next]
	}
	return strings.TrimSpace(part)
}
