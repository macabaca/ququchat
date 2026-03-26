package config

import (
	"strings"
)

type TaskPriorityRule struct {
	Task     string `yaml:"task" json:"task"`
	Priority int    `yaml:"priority" json:"priority"`
}

type TaskPriority struct {
	Rules []TaskPriorityRule `yaml:"rules" json:"rules"`
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
