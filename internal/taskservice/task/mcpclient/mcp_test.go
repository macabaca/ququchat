package mcpclient

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

func logPromptsForClient(t *testing.T, ctx context.Context, label string, client *Client) {
	t.Helper()
	if client == nil || client.session == nil {
		t.Logf("%s prompt 列表跳过: mcp client 未初始化", label)
		return
	}
	var cursor string
	total := 0
	for {
		resp, err := client.session.ListPrompts(ctx, &mcp.ListPromptsParams{Cursor: cursor})
		if err != nil {
			t.Logf("%s ListPrompts 调用失败(服务端可能未实现): %v", label, err)
			return
		}
		if resp == nil {
			break
		}
		for _, prompt := range resp.Prompts {
			if prompt == nil {
				continue
			}
			total++
			raw, _ := json.MarshalIndent(prompt, "  ", "  ")
			t.Logf("- Prompt[%d]:\n  %s", total, string(raw))
		}
		nextCursor := strings.TrimSpace(resp.NextCursor)
		if nextCursor == "" || nextCursor == cursor {
			break
		}
		cursor = nextCursor
	}
	t.Logf("%s provides %d prompts", label, total)
}

func TestTavilyListTools(t *testing.T) {
	ctx := context.Background()
	mcpServers, err := loadConfig("../../../config/config.yaml")
	require.NoError(t, err)
	tavilyServer, exists := mcpServers["tavily"]
	require.True(t, exists)

	clientOpts := map[string]ClientOptions{
		"tavily": {
			Endpoint: tavilyServer.Endpoint,
			APIKey:   tavilyServer.APIKey,
			Headers:  tavilyServer.Headers,
			Name:     tavilyServer.Name,
			Version:  tavilyServer.Version,
			Timeout:  time.Duration(tavilyServer.TimeoutMs) * time.Millisecond,
		},
	}

	multiClientOpts := MultiClientOptions{
		Servers: clientOpts,
	}
	multiClient, err := NewMultiClient(ctx, multiClientOpts)
	require.NoError(t, err)
	defer multiClient.Close()

	tavilyClient, exists := multiClient.clients["tavily"]
	require.True(t, exists)

	tools, err := tavilyClient.ListTools(ctx)
	assert.NoError(t, err)

	t.Logf("Tavily provides %d tools:", len(tools))
	for _, tool := range tools {
		t.Logf("- Tool Name: %s", tool.Name)
		t.Logf("  Description: %s", tool.Description)

		schema, _ := json.MarshalIndent(tool.InputSchema, "  ", "  ")
		t.Logf("  InputSchema: \n  %s", string(schema))
	}
	logPromptsForClient(t, ctx, "Tavily", tavilyClient)
}

func TestGezhePPTListTools(t *testing.T) {
	ctx := context.Background()
	mcpServers, err := loadConfig("../../../config/config.yaml")
	require.NoError(t, err)
	gezheServer, exists := mcpServers["歌者PPT"]
	require.True(t, exists)

	clientOpts := map[string]ClientOptions{
		"歌者PPT": {
			Endpoint: gezheServer.Endpoint,
			APIKey:   gezheServer.APIKey,
			Headers:  gezheServer.Headers,
			Name:     gezheServer.Name,
			Version:  gezheServer.Version,
			Timeout:  time.Duration(gezheServer.TimeoutMs) * time.Millisecond,
		},
	}

	multiClientOpts := MultiClientOptions{
		Servers: clientOpts,
	}
	multiClient, err := NewMultiClient(ctx, multiClientOpts)
	require.NoError(t, err)
	defer multiClient.Close()

	gezheClient, exists := multiClient.clients["歌者PPT"]
	require.True(t, exists)

	tools, err := gezheClient.ListTools(ctx)
	assert.NoError(t, err)

	t.Logf("歌者PPT provides %d tools:", len(tools))
	for _, tool := range tools {
		t.Logf("- Tool Name: %s", tool.Name)
		t.Logf("  Description: %s", tool.Description)

		schema, _ := json.MarshalIndent(tool.InputSchema, "  ", "  ")
		t.Logf("  InputSchema: \n  %s", string(schema))
	}
	logPromptsForClient(t, ctx, "歌者PPT", gezheClient)
}

func TestMinimaxListTools(t *testing.T) {
	ctx := context.Background()
	mcpServers, err := loadConfig("../../../config/config.yaml")
	require.NoError(t, err)
	minimaxServer, exists := mcpServers["minimax"]
	require.True(t, exists)
	runListToolsForServer(t, ctx, "minimax", minimaxServer)
}

func runListToolsForServer(t *testing.T, ctx context.Context, serverName string, server ServerConfig) {
	t.Helper()
	clientOpts := map[string]ClientOptions{
		serverName: {
			Endpoint: server.Endpoint,
			APIKey:   server.APIKey,
			Headers:  server.Headers,
			Name:     server.Name,
			Version:  server.Version,
			Timeout:  time.Duration(server.TimeoutMs) * time.Millisecond,
		},
	}

	multiClientOpts := MultiClientOptions{
		Servers: clientOpts,
	}
	multiClient, err := NewMultiClient(ctx, multiClientOpts)
	require.NoError(t, err)
	defer multiClient.Close()

	client, exists := multiClient.clients[serverName]
	require.True(t, exists)

	tools, err := client.ListTools(ctx)
	assert.NoError(t, err)

	t.Logf("%s provides %d tools:", serverName, len(tools))
	for _, tool := range tools {
		t.Logf("- Tool Name: %s", tool.Name)
		t.Logf("  Description: %s", tool.Description)
		schema, _ := json.MarshalIndent(tool.InputSchema, "  ", "  ")
		t.Logf("  InputSchema: \n  %s", string(schema))
	}
	logPromptsForClient(t, ctx, serverName, client)
}

func TestListToolsAllServers(t *testing.T) {
	mcpServers, err := loadConfig("../../../config/config.yaml")
	require.NoError(t, err)
	serverNames := make([]string, 0, len(mcpServers))
	for name := range mcpServers {
		serverNames = append(serverNames, name)
	}
	sort.Strings(serverNames)
	for _, name := range serverNames {
		server := mcpServers[name]
		t.Run(name, func(t *testing.T) {
			runListToolsForServer(t, context.Background(), name, server)
		})
	}
}
