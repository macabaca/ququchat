package config

// Database 数据库连接配置（从 YAML 读取的原始结构）
type Database struct {
    Driver   string            `yaml:"driver" json:"driver"`
    Host     string            `yaml:"host" json:"host"`
    Port     int               `yaml:"port" json:"port"`
    User     string            `yaml:"user" json:"user"`
    Password string            `yaml:"password" json:"password"`
    Name     string            `yaml:"name" json:"name"`
    Params   map[string]string `yaml:"params" json:"params"`
}