package config

import (
	"strings"
	"time"
)

type Chat struct {
	HistoryLimit int `yaml:"history_limit" json:"history_limit"`
}

type File struct {
	UploadDir    string `yaml:"upload_dir" json:"upload_dir"`
	MaxSizeBytes int64  `yaml:"max_size_bytes" json:"max_size_bytes"`
	Retention    string `yaml:"retention" json:"retention"`
}

func (f File) RetentionDuration() time.Duration {
	const defaultRetention = 30 * 24 * time.Hour
	if strings.TrimSpace(f.Retention) == "" {
		return defaultRetention
	}
	if d, err := time.ParseDuration(strings.TrimSpace(f.Retention)); err == nil && d > 0 {
		return d
	}
	return defaultRetention
}
