package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	agentmemory "ququchat/internal/taskservice/task/agent/memory"
	"ququchat/internal/taskservice/task/agent/toolruntime"
	"ququchat/internal/taskservice/task/agent/toolruntime/tools"
)

func RunExecutorNode(ctx context.Context, state *State) (next string, err error) {
	if state == nil {
		return "", errors.New("executor node state is nil")
	}
	toolName := strings.ToLower(strings.TrimSpace(state.ToolName))
	if toolName == "" {
		toolName = strings.ToLower(strings.TrimSpace(state.Plan.Action.Tool))
	}
	actionInput := strings.TrimSpace(state.ActionInput)
	if actionInput == "" {
		actionInput = strings.TrimSpace(state.Plan.Action.Input)
	}
	state.ToolName = toolName
	state.ActionInput = actionInput
	if toolName == "" {
		return "", errors.New("executor node tool name is empty")
	}
	if toolName == "finish" {
		return "executor.finish", nil
	}
	if toolName == "replan" {
		return "executor.replan", nil
	}
	if toolName == "bash" {
		return runBashTool(ctx, state, actionInput)
	}
	startAt := time.Now()
	toolOutput, toolErr := state.ToolRuntime.Run(ctx, toolName, actionInput, strings.TrimSpace(state.RoomID))
	durationMs := time.Since(startAt).Milliseconds()
	record := agentmemory.Observation{
		Step:       state.Step,
		Role:       "Executor",
		Tool:       toolName,
		Input:      actionInput,
		DurationMs: durationMs,
	}
	if toolErr != nil {
		record.Status = "failed"
		record.Error = toolErr.Error()
		state.ToolOutput = ""
		state.ToolOutputRaw = ""
		state.ToolError = toolErr.Error()
		if state.MemorySession != nil {
			state.MemorySession.AppendObservation(record)
		}
		return "executor.failed", nil
	}
	record.Status = "succeeded"
	record.Output = toolOutput
	rememberObservedURLsInState(state, toolOutput)
	state.ToolOutputRaw = toolOutput
	state.ToolError = ""
	if state.MemorySession != nil {
		state.MemorySession.AppendObservation(record)
	}
	if state.OutlineIndex < len(state.Outline.Steps)-1 {
		state.OutlineIndex++
	}
	if strings.Contains(toolName, ":") {
		state.ToolOutput = toolOutput
		return "executor.done_mcp", nil
	}
	state.ToolOutput = state.ToolOutputRaw
	return "executor.done_local", nil
}

func runSkillTool(state *State, actionInput string) (string, error) {
	t, ok := state.ToolRuntime.GetTool("skill")
	if !ok {
		return "", errors.New("skill tool is not registered")
	}
	st, ok := t.(tools.SkillToolTyped)
	if !ok {
		return "", errors.New("skill tool type assertion failed")
	}
	args, err := toolruntime.ParseActionInputJSONObject(actionInput)
	if err != nil {
		return "", err
	}
	skillName := toolruntime.ReadStringArg(args, "name")
	if skillName == "" {
		return "", errors.New("skill tool requires input.name")
	}
	skillArgs := toolruntime.ReadStringArg(args, "args")

	sf, err := st.LoadSkillFile(skillName)
	if err != nil {
		return "", err
	}

	switch sf.Mode {
	case tools.SkillModeContext:
		return runSkillContextMode(state, st, skillName, skillArgs)
	default:
		return runSkillOutlineMode(state, st, skillName, actionInput)
	}
}

func runBashTool(ctx context.Context, state *State, actionInput string) (string, error) {
	if state.SkillDir != "" {
		if args, err := toolruntime.ParseActionInputJSONObject(actionInput); err == nil {
			if cmd := toolruntime.ReadStringArg(args, "command"); cmd != "" {
				b, _ := json.Marshal(map[string]string{"command": "cd " + state.SkillDir + " && " + cmd})
				actionInput = string(b)
			}
		}
	}
	bt := tools.NewBashTool(state.SkillAllowedTools)
	if verr := bt.(interface {
		Validate(string) *toolruntime.ValidationError
	}).Validate(actionInput); verr != nil {
		return "executor.failed", errors.New(verr.Message + ": " + verr.Detail)
	}
	startAt := time.Now()
	out, toolErr := bt.Run(ctx, actionInput, strings.TrimSpace(state.RoomID))
	durationMs := time.Since(startAt).Milliseconds()
	record := agentmemory.Observation{
		Step:       state.Step,
		Role:       "Executor",
		Tool:       "bash",
		Input:      actionInput,
		DurationMs: durationMs,
	}
	if toolErr != nil {
		record.Status = "failed"
		record.Error = toolErr.Error()
		state.ToolOutput = ""
		state.ToolOutputRaw = ""
		state.ToolError = toolErr.Error()
		if state.MemorySession != nil {
			state.MemorySession.AppendObservation(record)
		}
		return "executor.failed", nil
	}
	record.Status = "succeeded"
	record.Output = out
	state.ToolOutput = out
	state.ToolOutputRaw = out
	state.ToolError = ""
	if state.MemorySession != nil {
		state.MemorySession.AppendObservation(record)
	}
	return "executor.done_local", nil
}

func runSkillOutlineMode(state *State, st tools.SkillToolTyped, skillName, actionInput string) (string, error) {
	sf, err := st.LoadSkillFile(skillName)
	if err != nil {
		return "", err
	}
	state.SkillAllowedTools = sf.AllowedTools
	state.SkillDir = sf.Dir
	outline, err := st.LoadSkillOutline(skillName)
	if err != nil {
		return "", err
	}
	steps := make([]plannerTask, 0, len(outline.Steps))
	for _, s := range outline.Steps {
		steps = append(steps, plannerTask{Task: s.Task, Tool: s.Tool})
	}
	state.Outline = plannerOutline{Steps: steps}
	state.OutlineIndex = 0
	state.CurrentTask = currentPlannerTask(state.Outline.Steps, 0)
	if state.MemorySession != nil {
		state.MemorySession.AppendObservation(agentmemory.Observation{
			Step:   state.Step,
			Role:   "Executor",
			Tool:   "skill",
			Input:  actionInput,
			Status: "succeeded",
			Output: "skill outline injected: " + skillName,
		})
	}
	return "executor.skill_loaded", nil
}

func runSkillContextMode(state *State, st tools.SkillToolTyped, skillName, skillArgs string) (string, error) {
	sf, err := st.LoadSkillFile(skillName)
	if err != nil {
		return "", err
	}
	state.SkillAllowedTools = sf.AllowedTools
	state.SkillDir = sf.Dir
	body, err := st.LoadSkillContext(skillName, skillArgs)
	if err != nil {
		return "", err
	}
	state.Goal = body + "\n\n目标：" + state.Goal
	if state.MemorySession != nil {
		state.MemorySession.AppendObservation(agentmemory.Observation{
			Step:   state.Step,
			Role:   "Executor",
			Tool:   "skill",
			Input:  skillName,
			Status: "succeeded",
			Output: "skill context injected: " + skillName,
		})
	}
	return "executor.skill_context", nil
}
