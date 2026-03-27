package mcpclient

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type MultiClientOptions struct {
	Servers map[string]ClientOptions
}

type MultiClient struct {
	clients map[string]*Client
}

type RoutedTool struct {
	Server        string
	Name          string
	QualifiedName string
	Tool          *mcp.Tool
}

func NewMultiClient(ctx context.Context, opts MultiClientOptions) (*MultiClient, error) {
	if len(opts.Servers) == 0 {
		return nil, errors.New("mcp servers config is empty")
	}
	clients := make(map[string]*Client, len(opts.Servers))
	normalizedServers := make(map[string]ClientOptions, len(opts.Servers))
	for rawName, serverOpts := range opts.Servers {
		name := strings.TrimSpace(rawName)
		if name == "" {
			closeClients(clients)
			return nil, errors.New("mcp server name is empty")
		}
		if _, exists := normalizedServers[name]; exists {
			closeClients(clients)
			return nil, fmt.Errorf("duplicate mcp server name: %s", name)
		}
		normalizedServers[name] = serverOpts
	}
	serverNames := make([]string, 0, len(normalizedServers))
	for name := range normalizedServers {
		serverNames = append(serverNames, name)
	}
	sort.Strings(serverNames)
	for _, name := range serverNames {
		serverOpts := normalizedServers[name]
		client, err := NewClient(ctx, serverOpts)
		if err != nil {
			closeClients(clients)
			return nil, fmt.Errorf("connect mcp server %s failed: %w", name, err)
		}
		clients[name] = client
	}
	return &MultiClient{
		clients: clients,
	}, nil
}

func (m *MultiClient) Close() error {
	if m == nil || len(m.clients) == 0 {
		return nil
	}
	var errs []error
	for _, client := range m.clients {
		if client == nil {
			continue
		}
		if err := client.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (m *MultiClient) ListTools(ctx context.Context) ([]RoutedTool, error) {
	if m == nil || len(m.clients) == 0 {
		return nil, errors.New("multi mcp client is not initialized")
	}
	serverNames := make([]string, 0, len(m.clients))
	for name := range m.clients {
		serverNames = append(serverNames, name)
	}
	sort.Strings(serverNames)
	out := make([]RoutedTool, 0)
	for _, server := range serverNames {
		client := m.clients[server]
		if client == nil {
			continue
		}
		tools, err := client.ListTools(ctx)
		if err != nil {
			return nil, fmt.Errorf("list tools from %s failed: %w", server, err)
		}
		for _, tool := range tools {
			if tool == nil {
				continue
			}
			name := strings.TrimSpace(tool.Name)
			if name == "" {
				continue
			}
			out = append(out, RoutedTool{
				Server:        server,
				Name:          name,
				QualifiedName: makeQualifiedToolName(server, name),
				Tool:          tool,
			})
		}
	}
	return out, nil
}

func (m *MultiClient) CallTool(ctx context.Context, server string, toolName string, arguments map[string]any) (*mcp.CallToolResult, error) {
	if m == nil || len(m.clients) == 0 {
		return nil, errors.New("multi mcp client is not initialized")
	}
	trimmedServer := strings.TrimSpace(server)
	if trimmedServer == "" {
		return nil, errors.New("mcp server name is empty")
	}
	resolvedServer, err := m.resolveServerName(trimmedServer)
	if err != nil {
		return nil, err
	}
	client, ok := m.clients[resolvedServer]
	if !ok || client == nil {
		return nil, fmt.Errorf("mcp server not found: %s", trimmedServer)
	}
	return client.CallTool(ctx, toolName, arguments)
}

func (m *MultiClient) CallToolByQualifiedName(ctx context.Context, qualifiedToolName string, arguments map[string]any) (*mcp.CallToolResult, error) {
	server, toolName, err := parseQualifiedToolName(qualifiedToolName)
	if err != nil {
		return nil, err
	}
	return m.CallTool(ctx, server, toolName, arguments)
}

func (m *MultiClient) CallToolAuto(ctx context.Context, toolName string, arguments map[string]any) (*mcp.CallToolResult, error) {
	trimmedToolName := strings.TrimSpace(toolName)
	if trimmedToolName == "" {
		return nil, errors.New("tool name is empty")
	}
	tools, err := m.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	hits := make([]RoutedTool, 0, 2)
	for _, tool := range tools {
		if strings.EqualFold(strings.TrimSpace(tool.Name), trimmedToolName) {
			hits = append(hits, tool)
		}
	}
	if len(hits) == 0 {
		return nil, fmt.Errorf("tool not found: %s", trimmedToolName)
	}
	if len(hits) > 1 {
		names := make([]string, 0, len(hits))
		for _, hit := range hits {
			names = append(names, hit.QualifiedName)
		}
		return nil, fmt.Errorf("tool name is ambiguous, use qualified name: %s", strings.Join(names, ", "))
	}
	return m.CallTool(ctx, hits[0].Server, hits[0].Name, arguments)
}

func makeQualifiedToolName(server string, toolName string) string {
	return strings.TrimSpace(server) + ":" + strings.TrimSpace(toolName)
}

func parseQualifiedToolName(value string) (string, string, error) {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return "", "", errors.New("qualified tool name is empty")
	}
	parts := strings.SplitN(raw, ":", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid qualified tool name: %s", raw)
	}
	server := strings.TrimSpace(parts[0])
	toolName := strings.TrimSpace(parts[1])
	if server == "" || toolName == "" {
		return "", "", fmt.Errorf("invalid qualified tool name: %s", raw)
	}
	return server, toolName, nil
}

func closeClients(clients map[string]*Client) {
	for _, client := range clients {
		if client == nil {
			continue
		}
		_ = client.Close()
	}
}

func (m *MultiClient) resolveServerName(server string) (string, error) {
	if m == nil || len(m.clients) == 0 {
		return "", errors.New("multi mcp client is not initialized")
	}
	trimmedServer := strings.TrimSpace(server)
	if trimmedServer == "" {
		return "", errors.New("mcp server name is empty")
	}
	if _, ok := m.clients[trimmedServer]; ok {
		return trimmedServer, nil
	}
	matches := make([]string, 0, 2)
	for name := range m.clients {
		if strings.EqualFold(strings.TrimSpace(name), trimmedServer) {
			matches = append(matches, name)
		}
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("mcp server not found: %s", trimmedServer)
	}
	if len(matches) > 1 {
		sort.Strings(matches)
		return "", fmt.Errorf("mcp server name is ambiguous: %s (%s)", trimmedServer, strings.Join(matches, ", "))
	}
	return matches[0], nil
}
