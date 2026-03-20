package mcpclient

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type MCPConfig struct {
	MCPServers map[string]struct {
		Endpoint  string            `json:"endpoint"`
		APIKey    string            `json:"apiKey"`
		Headers   map[string]string `json:"headers"`
		Name      string            `json:"name"`
		Version   string            `json:"version"`
		TimeoutMs int               `json:"timeoutMs"`
	} `json:"mcpServers"`
}

func loadConfig(path string) (*MCPConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var config MCPConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	return &config, nil
}

func TestTavilyListTools(t *testing.T) {
	ctx := context.Background()
	config, err := loadConfig("../../../config/mcp_servers.json")
	assert.NoError(t, err)

	clientOpts := make(map[string]ClientOptions)
	for name, server := range config.MCPServers {
		clientOpts[name] = ClientOptions{
			Endpoint: server.Endpoint,
			APIKey:   server.APIKey,
			Headers:  server.Headers,
			Name:     server.Name,
			Version:  server.Version,
			Timeout:  time.Duration(server.TimeoutMs) * time.Millisecond,
		}
	}

	multiClientOpts := MultiClientOptions{
		Servers: clientOpts,
	}
	multiClient, err := NewMultiClient(ctx, multiClientOpts)
	assert.NoError(t, err)
	defer multiClient.Close()

	tavilyClient, exists := multiClient.clients["tavily"]
	assert.True(t, exists)

	tools, err := tavilyClient.ListTools(ctx)
	assert.NoError(t, err)

	t.Logf("Tavily provides %d tools:", len(tools))
	for _, tool := range tools {
		t.Logf("- Tool Name: %s", tool.Name)
		t.Logf("  Description: %s", tool.Description)

		schema, _ := json.MarshalIndent(tool.InputSchema, "  ", "  ")
		t.Logf("  InputSchema: \n  %s", string(schema))
	}
}
