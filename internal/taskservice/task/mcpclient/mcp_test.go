package mcpclient

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"ququchat/internal/config"
)

func loadConfig(path string) (map[string]ServerConfig, error) {
	cfg, err := config.LoadFromFile(path)
	if err != nil {
		return nil, err
	}
	servers := make(map[string]ServerConfig, len(cfg.MCPServers))
	for name, server := range cfg.MCPServers {
		servers[name] = ServerConfig{
			Endpoint:  server.Endpoint,
			APIKey:    server.APIKey,
			Headers:   server.Headers,
			Name:      server.Name,
			Version:   server.Version,
			TimeoutMs: server.TimeoutMs,
		}
	}
	return servers, nil
}

func TestTavilyListTools(t *testing.T) {
	ctx := context.Background()
	mcpServers, err := loadConfig("../../../config/config.yaml")
	assert.NoError(t, err)

	clientOpts := make(map[string]ClientOptions)
	for name, server := range mcpServers {
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

func TestGezhePPTListTools(t *testing.T) {
	ctx := context.Background()
	mcpServers, err := loadConfig("../../../config/config.yaml")
	assert.NoError(t, err)

	clientOpts := make(map[string]ClientOptions)
	for name, server := range mcpServers {
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

	gezheClient, exists := multiClient.clients["歌者PPT"]
	assert.True(t, exists)

	tools, err := gezheClient.ListTools(ctx)
	assert.NoError(t, err)

	t.Logf("歌者PPT provides %d tools:", len(tools))
	for _, tool := range tools {
		t.Logf("- Tool Name: %s", tool.Name)
		t.Logf("  Description: %s", tool.Description)

		schema, _ := json.MarshalIndent(tool.InputSchema, "  ", "  ")
		t.Logf("  InputSchema: \n  %s", string(schema))
	}
}
