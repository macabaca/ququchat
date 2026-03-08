package config

import (
	"strings"
	"time"
)

type Chat struct {
	HistoryLimit int `yaml:"history_limit" json:"history_limit"`
}

type File struct {
	UploadDir    string    `yaml:"upload_dir" json:"upload_dir"`
	MaxSizeBytes int64     `yaml:"max_size_bytes" json:"max_size_bytes"`
	Retention    string    `yaml:"retention" json:"retention"`
	Thumbnail    Thumbnail `yaml:"thumbnail" json:"thumbnail"`
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

type Thumbnail struct {
	MaxDimension   int    `yaml:"max_dimension" json:"max_dimension"`
	JPEGQuality    int    `yaml:"jpeg_quality" json:"jpeg_quality"`
	RetryCount     int    `yaml:"retry_count" json:"retry_count"`
	RetryDelay     string `yaml:"retry_delay" json:"retry_delay"`
	MaxSourceBytes int64  `yaml:"max_source_bytes" json:"max_source_bytes"`
}

func (t Thumbnail) MaxDimensionOrDefault() int {
	if t.MaxDimension > 0 {
		return t.MaxDimension
	}
	return 320
}

func (t Thumbnail) JPEGQualityOrDefault() int {
	if t.JPEGQuality > 0 && t.JPEGQuality <= 100 {
		return t.JPEGQuality
	}
	return 80
}

func (t Thumbnail) RetryCountOrDefault() int {
	if t.RetryCount > 0 {
		return t.RetryCount
	}
	return 3
}

func (t Thumbnail) RetryDelayDuration() time.Duration {
	if strings.TrimSpace(t.RetryDelay) == "" {
		return 200 * time.Millisecond
	}
	if d, err := time.ParseDuration(strings.TrimSpace(t.RetryDelay)); err == nil && d > 0 {
		return d
	}
	return 200 * time.Millisecond
}

func (t Thumbnail) MaxSourceBytesOrDefault() int64 {
	if t.MaxSourceBytes > 0 {
		return t.MaxSourceBytes
	}
	return int64(10 * 1024 * 1024)
}

type Avatar struct {
	MaxSizeBytes int64  `yaml:"max_size_bytes" json:"max_size_bytes"`
	Retention    string `yaml:"retention" json:"retention"`
	Permanent    *bool  `yaml:"permanent" json:"permanent"`
}

func (a Avatar) MaxSizeOrDefault() int64 {
	const defaultMax = int64(5 * 1024 * 1024)
	if a.MaxSizeBytes > 0 {
		return a.MaxSizeBytes
	}
	return defaultMax
}

func (a Avatar) PermanentOrDefault() bool {
	if a.Permanent != nil {
		return *a.Permanent
	}
	if strings.TrimSpace(a.Retention) == "" {
		return true
	}
	return false
}

func (a Avatar) RetentionDuration() time.Duration {
	if a.PermanentOrDefault() {
		return 0
	}
	const defaultRetention = 10 * 365 * 24 * time.Hour
	if strings.TrimSpace(a.Retention) == "" {
		return defaultRetention
	}
	if d, err := time.ParseDuration(strings.TrimSpace(a.Retention)); err == nil && d > 0 {
		return d
	}
	return defaultRetention
}
