package services

import (
	"strconv"
	"strings"
	"time"

	agenttypes "ququchat/internal/taskservice/task/agent/types"
)

func BuildRealtimePlanningGuidance(specs []agenttypes.ToolSpec, now time.Time) string {
	builder := strings.Builder{}
	builder.WriteString("时间基线：当前系统时间是 ")
	builder.WriteString(now.Format("2006-01-02 15:04:05 -07:00 MST"))
	builder.WriteString("。\n")
	builder.WriteString("认知边界：你仅在 2025 年之前的数据上训练，这不代表当前时间停留在 2025 年前。\n")
	if HasTavilyTool(specs) {
		builder.WriteString("决策要求：若请求涉及即时信息、最新事件或实时数据，先调用 tavily 相关工具检索，再基于检索结果思考与回答。\n")
	} else {
		builder.WriteString("决策要求：若请求涉及即时信息、最新事件或实时数据，先调用可用联网检索工具（如 tavily 相关工具）检索，再基于检索结果思考与回答。\n")
	}
	return builder.String()
}

func HasTavilyTool(specs []agenttypes.ToolSpec) bool {
	for _, spec := range specs {
		if strings.Contains(strings.ToLower(strings.TrimSpace(spec.Name)), "tavily") {
			return true
		}
		for _, alias := range spec.Aliases {
			if strings.Contains(strings.ToLower(strings.TrimSpace(alias)), "tavily") {
				return true
			}
		}
	}
	return false
}

func BuildCoordinatorPrompt(input agenttypes.CoordinatorPromptInput) string {
	builder := strings.Builder{}
	builder.WriteString("你是执行协调器（Coordinator）。请为目标选择下一步动作，只输出JSON。\n")
	builder.WriteString("思考与表达要求：即使工具描述或参数为英文，你也必须始终使用中文进行思考与输出；thought 字段必须是中文。\n")
	builder.WriteString(input.RealtimeGuidance)
	builder.WriteString(input.AgentIdentity)
	builder.WriteString("目标：")
	builder.WriteString(input.Goal)
	builder.WriteString("\n")
	builder.WriteString("可用工具:\n")
	builder.WriteString(input.ToolSection)
	builder.WriteString("规则:\n")
	for _, line := range input.RuleLines {
		builder.WriteString("- ")
		builder.WriteString(strings.TrimSpace(line))
		builder.WriteString("\n")
	}
	builder.WriteString("当前步数：")
	builder.WriteString(strconv.Itoa(input.Step))
	builder.WriteString("/")
	builder.WriteString(strconv.Itoa(input.MaxSteps))
	builder.WriteString("\n")
	if strings.TrimSpace(input.OutlineText) != "" {
		builder.WriteString("规划小任务列表：\n")
		builder.WriteString(strings.TrimSpace(input.OutlineText))
		builder.WriteString("\n")
	}
	if strings.TrimSpace(input.CurrentTask) != "" {
		builder.WriteString("当前优先小任务：")
		builder.WriteString(strings.TrimSpace(input.CurrentTask))
		builder.WriteString("\n")
	}
	if strings.TrimSpace(input.Feedback) != "" {
		builder.WriteString("上一轮反馈：")
		builder.WriteString(strings.TrimSpace(input.Feedback))
		builder.WriteString("\n")
	}
	builder.WriteString("最近消息条数：")
	builder.WriteString(strconv.Itoa(input.RecentMessageCount))
	builder.WriteString("\n")
	builder.WriteString("输出示例1:\n")
	builder.WriteString("{\"thought\":\"先检索相关历史消息\",\"action\":{\"tool\":\"search_rag\",\"input\":\"{\\\"query\\\":\\\"用户当前问题关键词\\\"}\"}}\n")
	builder.WriteString("输出示例2:\n")
	builder.WriteString("{\"thought\":\"信息足够，直接给答案\",\"action\":{\"tool\":\"finish\",\"input\":\"{\\\"final\\\":\\\"这是最终答案\\\"}\"}}\n")
	builder.WriteString("输出示例3:\n")
	builder.WriteString("{\"thought\":\"当前路径效果差，先重规划\",\"action\":{\"tool\":\"replan\",\"input\":\"{\\\"reason\\\":\\\"已有方案命中率低，换检索路径\\\"}\"}}\n")
	builder.WriteString("输出示例4:\n")
	builder.WriteString("{\"thought\":\"用户要文生图，先执行生成\",\"action\":{\"tool\":\"generate_image\",\"input\":\"{\\\"prompt\\\":\\\"一只戴墨镜的柴犬，电影感\\\"}\"}}\n")
	return builder.String()
}

func BuildJSONFormatterPrompt(rawOutput string, schema string) string {
	builder := strings.Builder{}
	builder.WriteString("你是JSONFormatter。你的任务是把输入内容转换成严格JSON对象，只输出JSON。\n")
	builder.WriteString("必须遵循的schema:\n")
	builder.WriteString(strings.TrimSpace(schema))
	builder.WriteString("\n")
	builder.WriteString("要求:\n")
	builder.WriteString("- 只输出一个JSON对象，不允许markdown代码块，不允许解释文字。\n")
	builder.WriteString("- 保留原始语义，不要增加无关信息。\n")
	builder.WriteString("- 如果tool出现别名，归一化到标准工具名。\n")
	builder.WriteString("- action.input 必须是 JSON 对象字符串，本地工具参数放在该 JSON 对象内。\n")
	builder.WriteString("待规范化输入:\n")
	builder.WriteString(strings.TrimSpace(rawOutput))
	builder.WriteString("\n")
	return builder.String()
}

func BuildFinalJudgePrompt(input agenttypes.FinalJudgePromptInput) string {
	builder := strings.Builder{}
	builder.WriteString("你是FinalJudge。请只评估候选final是否跑题，只输出JSON。\n")
	builder.WriteString("目标：")
	builder.WriteString(strings.TrimSpace(input.Goal))
	builder.WriteString("\n")
	builder.WriteString("候选final：")
	builder.WriteString(strings.TrimSpace(input.Candidate))
	builder.WriteString("\n")
	builder.WriteString("最近消息（节选）：\n")
	builder.WriteString(strings.TrimSpace(input.RecentMessagesText))
	builder.WriteString("\n")
	builder.WriteString("执行过程（节选）：\n")
	builder.WriteString(strings.TrimSpace(input.TraceText))
	builder.WriteString("\n")
	builder.WriteString("评估规则:\n")
	builder.WriteString("- 你只能判断“是否跑题”，不能判断事实正确性、年份时间是否准确、信息是否真实、是否满足目标。\n")
	builder.WriteString("- 即使内容可能有事实错误，只要主题相关且不是闲聊/反问/答非所问，就应判定 pass=true。\n")
	builder.WriteString("- 只有明显跑题、泛化寒暄、纯反问用户、与目标主题无关时，才判定 pass=false。\n")
	builder.WriteString("- score范围0-100。\n")
	builder.WriteString("- 分数语义：与主题强相关可给70-100，疑似偏题给40-69，明显跑题给0-39。\n")
	builder.WriteString("- issues只描述“跑题相关问题”，不得包含事实真伪或时间判断。\n")
	builder.WriteString("- better_final 固定输出空字符串。\n")
	builder.WriteString("输出格式:\n")
	builder.WriteString("{\"pass\":true|false,\"score\":0,\"issues\":[\"...\"],\"better_final\":\"...\"}")
	return builder.String()
}

func BuildFinalSynthesizerPrompt(input agenttypes.FinalSynthesizerPromptInput) string {
	builder := strings.Builder{}
	builder.WriteString("你是FinalSynthesizer。请基于目标与上下文直接生成最终答案。\n")
	builder.WriteString("目标：")
	builder.WriteString(strings.TrimSpace(input.Goal))
	builder.WriteString("\n")
	builder.WriteString("当前候选final：")
	builder.WriteString(strings.TrimSpace(input.Candidate))
	builder.WriteString("\n")
	builder.WriteString("最近消息（节选）：\n")
	builder.WriteString(strings.TrimSpace(input.RecentMessagesText))
	builder.WriteString("\n")
	builder.WriteString("执行过程（节选）：\n")
	builder.WriteString(strings.TrimSpace(input.TraceText))
	builder.WriteString("\n")
	builder.WriteString("要求:\n")
	builder.WriteString("- 直接回答目标，不要反问用户。\n")
	builder.WriteString("- 必须使用中文输出最终答案；即使工具返回英文内容，也要用中文总结与表达。\n")
	builder.WriteString("- 语言清晰、具体、可执行。\n")
	builder.WriteString("- 只输出最终答案正文，不要JSON。\n")
	return builder.String()
}
