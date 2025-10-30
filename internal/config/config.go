package config

import (
    "os"
    "path/filepath"

    "gopkg.in/yaml.v3"
)

type Config struct {
    Database Database `yaml:"database" json:"database"`
    Auth     Auth     `yaml:"auth" json:"auth"`
}

type Database struct {
    Driver   string            `yaml:"driver" json:"driver"`
    Host     string            `yaml:"host" json:"host"`
    Port     int               `yaml:"port" json:"port"`
    User     string            `yaml:"user" json:"user"`
    Password string            `yaml:"password" json:"password"`
    Name     string            `yaml:"name" json:"name"`
    Params   map[string]string `yaml:"params" json:"params"`
}

// Auth 认证相关配置
type Auth struct {
    JWTSecret string `yaml:"jwt_secret" json:"jwt_secret"`
}

// LoadFromFile 读取指定路径的 YAML 配置文件
func LoadFromFile(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
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