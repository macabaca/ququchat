package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type TaskPriorityRule struct {
	Task     string `json:"task"`
	Priority int    `json:"priority"`
}

type TaskPriority struct {
	Rules []TaskPriorityRule `json:"rules"`
}

func LoadTaskPriorityFromFile(path string) (*TaskPriority, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg TaskPriority
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func TaskPriorityDefaultPath() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(cwd, "internal", "config", "task_priority.json"), nil
}

func LoadTaskPriorityDefault() (*TaskPriority, error) {
	path, err := TaskPriorityDefaultPath()
	if err != nil {
		return nil, err
	}
	return LoadTaskPriorityFromFile(path)
}

func (c TaskPriority) NormalizedRules() []TaskPriorityRule {
	if len(c.Rules) == 0 {
		return nil
	}
	rules := make([]TaskPriorityRule, 0, len(c.Rules))
	for _, item := range c.Rules {
		task := strings.TrimSpace(item.Task)
		if task == "" {
			continue
		}
		rules = append(rules, TaskPriorityRule{
			Task:     task,
			Priority: item.Priority,
		})
	}
	return rules
}
