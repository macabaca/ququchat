package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Database       Database             `yaml:"database" json:"database"`
	Redis          Redis                `yaml:"redis" json:"redis"`
	Auth           Auth                 `yaml:"auth" json:"auth"`
	Chat           Chat                 `yaml:"chat" json:"chat"`
	Task           Task                 `yaml:"task" json:"task"`
	TaskPriority   TaskPriority         `yaml:"task_priority" json:"task_priority"`
	LLM            LLM                  `yaml:"llm" json:"llm"`
	AIGC           AIGC                 `yaml:"aigc" json:"aigc"`
	Embedding      Embedding            `yaml:"embedding" json:"embedding"`
	Vector         Vector               `yaml:"vector" json:"vector"`
	RAGStopPhrases RAGStopPhrasesConfig `yaml:"rag_stop_phrases" json:"rag_stop_phrases"`
	MCPServers     map[string]MCPServer `yaml:"mcp_servers" json:"mcp_servers"`
	File           File                 `yaml:"file" json:"file"`
	Avatar         Avatar               `yaml:"avatar" json:"avatar"`
	Storage        Storage              `yaml:"storage" json:"storage"`
	Minio          Minio                `yaml:"minio" json:"minio"`
	OSS            OSS                  `yaml:"oss" json:"oss"`
}

type MCPServer struct {
	Endpoint  string            `yaml:"endpoint" json:"endpoint"`
	APIKey    string            `yaml:"api_key" json:"api_key"`
	Headers   map[string]string `yaml:"headers" json:"headers"`
	Name      string            `yaml:"name" json:"name"`
	Version   string            `yaml:"version" json:"version"`
	TimeoutMs int               `yaml:"timeout_ms" json:"timeout_ms"`
}

type RAGStopPhrasesConfig struct {
	StopPhrases []string `yaml:"stop_phrases" json:"stop_phrases"`
}

type Redis struct {
	Addr           string `yaml:"addr" json:"addr"`
	Password       string `yaml:"password" json:"password"`
	DB             int    `yaml:"db" json:"db"`
	KeyPrefix      string `yaml:"key_prefix" json:"key_prefix"`
	DialTimeoutMs  int    `yaml:"dial_timeout_ms" json:"dial_timeout_ms"`
	ReadTimeoutMs  int    `yaml:"read_timeout_ms" json:"read_timeout_ms"`
	WriteTimeoutMs int    `yaml:"write_timeout_ms" json:"write_timeout_ms"`
}

// Database 结构体已拆分至 config_db.go

// LoadFromFile 读取指定路径的 YAML 配置文件
func LoadFromFile(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	expanded := os.ExpandEnv(string(b))
	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// DefaultPath 返回 internal/config/config.yaml 的绝对路径
func DefaultPath() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(cwd, "internal", "config", "config.yaml"), nil
}

// LoadDefault 从 internal/config/config.yaml 加载配置
func LoadDefault() (*Config, error) {
	path, err := DefaultPath()
	if err != nil {
		return nil, err
	}
	return LoadFromFile(path)
}

// 数据库相关方法移动至 internal/server/db。
