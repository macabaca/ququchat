package mcpclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type MultiClientFileConfig struct {
	MCPServers map[string]ServerConfig `json:"mcpServers"`
}

type ServerConfig struct {
	Endpoint  string            `json:"endpoint"`
	APIKey    string            `json:"apiKey"`
	Headers   map[string]string `json:"headers"`
	Name      string            `json:"name"`
	Version   string            `json:"version"`
	TimeoutMs int               `json:"timeoutMs"`
}

func LoadMultiClientOptionsFromFile(path string) (MultiClientOptions, error) {
	trimmedPath := strings.TrimSpace(path)
	if trimmedPath == "" {
		return MultiClientOptions{}, errors.New("mcp config path is empty")
	}
	raw, err := os.ReadFile(trimmedPath)
	if err != nil {
		return MultiClientOptions{}, err
	}
	var cfg MultiClientFileConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return MultiClientOptions{}, err
	}
	return BuildMultiClientOptions(cfg)
}

func BuildMultiClientOptions(cfg MultiClientFileConfig) (MultiClientOptions, error) {
	if len(cfg.MCPServers) == 0 {
		return MultiClientOptions{}, errors.New("mcpServers is empty")
	}
	servers := make(map[string]ClientOptions, len(cfg.MCPServers))
	for rawServerName, serverCfg := range cfg.MCPServers {
		serverName := strings.TrimSpace(rawServerName)
		if serverName == "" {
			return MultiClientOptions{}, errors.New("mcp server name is empty")
		}
		if _, exists := servers[serverName]; exists {
			return MultiClientOptions{}, fmt.Errorf("duplicate mcp server name: %s", serverName)
		}
		endpoint := strings.TrimSpace(serverCfg.Endpoint)
		if endpoint == "" {
			return MultiClientOptions{}, fmt.Errorf("mcp server endpoint is empty: %s", serverName)
		}
		timeout := 60 * time.Second
		if serverCfg.TimeoutMs > 0 {
			timeout = time.Duration(serverCfg.TimeoutMs) * time.Millisecond
		}
		headers := map[string]string{}
		for key, value := range serverCfg.Headers {
			trimmedKey := strings.TrimSpace(key)
			trimmedValue := strings.TrimSpace(value)
			if trimmedKey == "" || trimmedValue == "" {
				continue
			}
			headers[trimmedKey] = trimmedValue
		}
		servers[serverName] = ClientOptions{
			Endpoint: endpoint,
			APIKey:   strings.TrimSpace(serverCfg.APIKey),
			Headers:  headers,
			Name:     strings.TrimSpace(serverCfg.Name),
			Version:  strings.TrimSpace(serverCfg.Version),
			Timeout:  timeout,
		}
	}
	return MultiClientOptions{
		Servers: servers,
	}, nil
}

func NewMultiClientFromConfigFile(ctx context.Context, path string) (*MultiClient, error) {
	opts, err := LoadMultiClientOptionsFromFile(path)
	if err != nil {
		return nil, err
	}
	return NewMultiClient(ctx, opts)
}

func DefaultConfigPath() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(cwd, "internal", "config", "mcp_servers.json"), nil
}

func NewMultiClientFromDefaultConfig(ctx context.Context) (*MultiClient, error) {
	path, err := DefaultConfigPath()
	if err != nil {
		return nil, err
	}
	return NewMultiClientFromConfigFile(ctx, path)
}
