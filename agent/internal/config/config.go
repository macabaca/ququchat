package config

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server Server `yaml:"server" json:"server"`
	Worker Worker `yaml:"worker" json:"worker"`
	Kafka  Kafka  `yaml:"kafka" json:"kafka"`
	Queue  Queue  `yaml:"queue" json:"queue"`
}

type Server struct {
	Addr string `yaml:"addr" json:"addr"`
}

type Worker struct {
	Size int `yaml:"size" json:"size"`
}

type Kafka struct {
	Brokers      []string `yaml:"brokers" json:"brokers"`
	IngressTopic string   `yaml:"ingress_topic" json:"ingress_topic"`
	ResultTopic  string   `yaml:"result_topic" json:"result_topic"`
	GroupID      string   `yaml:"group_id" json:"group_id"`
}

type Queue struct {
	HighCapacity   int `yaml:"high_capacity" json:"high_capacity"`
	NormalCapacity int `yaml:"normal_capacity" json:"normal_capacity"`
	LowCapacity    int `yaml:"low_capacity" json:"low_capacity"`
}

type Settings struct {
	Addr           string
	WorkerSize     int
	KafkaBrokers   []string
	KafkaIngress   string
	KafkaResult    string
	KafkaGroupID   string
	QueueHighCap   int
	QueueNormalCap int
	QueueLowCap    int
}

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

func DefaultPath() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(cwd, "agent", "config", "config.yaml"), nil
}

func LoadDefault() (*Config, error) {
	path, err := DefaultPath()
	if err != nil {
		return nil, err
	}
	return LoadFromFile(path)
}

func (c Config) ToSettings() Settings {
	addr := strings.TrimSpace(c.Server.Addr)
	if addr == "" {
		addr = ":50051"
	}
	workerSize := c.Worker.Size
	if workerSize <= 0 {
		workerSize = 4
	}
	brokers := c.Kafka.Brokers
	if len(brokers) == 0 {
		brokers = []string{"127.0.0.1:9092"}
	}
	ingress := strings.TrimSpace(c.Kafka.IngressTopic)
	if ingress == "" {
		ingress = "agent.llm.ingress"
	}
	result := strings.TrimSpace(c.Kafka.ResultTopic)
	if result == "" {
		result = "agent.llm.result"
	}
	groupID := strings.TrimSpace(c.Kafka.GroupID)
	if groupID == "" {
		groupID = "agent-llm-consumer"
	}
	highCap := c.Queue.HighCapacity
	if highCap <= 0 {
		highCap = 256
	}
	normalCap := c.Queue.NormalCapacity
	if normalCap <= 0 {
		normalCap = 512
	}
	lowCap := c.Queue.LowCapacity
	if lowCap <= 0 {
		lowCap = 512
	}
	return Settings{
		Addr:           addr,
		WorkerSize:     workerSize,
		KafkaBrokers:   brokers,
		KafkaIngress:   ingress,
		KafkaResult:    result,
		KafkaGroupID:   groupID,
		QueueHighCap:   highCap,
		QueueNormalCap: normalCap,
		QueueLowCap:    lowCap,
	}
}
