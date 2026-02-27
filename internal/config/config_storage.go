package config

import "strings"

type Storage struct {
	Provider string `yaml:"provider" json:"provider"`
}

func (s Storage) ProviderOrDefault() string {
	p := strings.TrimSpace(s.Provider)
	if p == "" {
		return "minio"
	}
	return p
}

