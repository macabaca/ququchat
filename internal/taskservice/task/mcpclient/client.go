package mcpclient

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ClientOptions struct {
	Endpoint string
	APIKey   string
	Headers  map[string]string
	Name     string
	Version  string
	Timeout  time.Duration
}

type Client struct {
	sdkClient  *mcp.Client
	session    *mcp.ClientSession
	httpClient *http.Client
	transport  mcp.Transport
}

func NewClient(ctx context.Context, opts ClientOptions) (*Client, error) {
	endpoint := strings.TrimSpace(opts.Endpoint)
	if endpoint == "" {
		return nil, errors.New("mcp endpoint is empty")
	}
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name = "ququchat-mcp-client"
	}
	version := strings.TrimSpace(opts.Version)
	if version == "" {
		version = "0.1.0"
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	baseTransport := http.DefaultTransport
	apiKey := strings.TrimSpace(opts.APIKey)
	headers := map[string]string{}
	for key, value := range opts.Headers {
		trimmedKey := strings.TrimSpace(key)
		trimmedValue := strings.TrimSpace(value)
		if trimmedKey == "" || trimmedValue == "" {
			continue
		}
		headers[trimmedKey] = trimmedValue
	}
	if apiKey != "" {
		headers["Authorization"] = "Bearer " + apiKey
		headers["X-API-Key"] = apiKey
	}
	if len(headers) > 0 {
		baseTransport = &headerRoundTripper{
			base:    http.DefaultTransport,
			headers: headers,
		}
	}
	httpClient := &http.Client{
		Timeout:   timeout,
		Transport: baseTransport,
	}
	transport := &mcp.StreamableClientTransport{
		Endpoint:   endpoint,
		HTTPClient: httpClient,
	}
	sdkClient := mcp.NewClient(&mcp.Implementation{
		Name:    name,
		Version: version,
	}, nil)
	session, err := sdkClient.Connect(ctx, transport, nil)
	if err != nil {
		return nil, err
	}
	return &Client{
		sdkClient:  sdkClient,
		session:    session,
		httpClient: httpClient,
		transport:  transport,
	}, nil
}

func (c *Client) Close() error {
	if c == nil || c.session == nil {
		return nil
	}
	return c.session.Close()
}

func (c *Client) ListTools(ctx context.Context) ([]*mcp.Tool, error) {
	if c == nil || c.session == nil {
		return nil, errors.New("mcp client is not initialized")
	}
	var cursor string
	tools := make([]*mcp.Tool, 0)
	for {
		resp, err := c.session.ListTools(ctx, &mcp.ListToolsParams{
			Cursor: cursor,
		})
		if err != nil {
			return nil, err
		}
		if resp == nil {
			break
		}
		if len(resp.Tools) > 0 {
			tools = append(tools, resp.Tools...)
		}
		nextCursor := strings.TrimSpace(resp.NextCursor)
		if nextCursor == "" || nextCursor == cursor {
			break
		}
		cursor = nextCursor
	}
	return tools, nil
}

func (c *Client) CallTool(ctx context.Context, name string, arguments map[string]any) (*mcp.CallToolResult, error) {
	if c == nil || c.session == nil {
		return nil, errors.New("mcp client is not initialized")
	}
	toolName := strings.TrimSpace(name)
	if toolName == "" {
		return nil, errors.New("tool name is empty")
	}
	args := arguments
	if args == nil {
		args = map[string]any{}
	}
	return c.session.CallTool(ctx, &mcp.CallToolParams{
		Name:      toolName,
		Arguments: args,
	})
}

type headerRoundTripper struct {
	base    http.RoundTripper
	headers map[string]string
}

func (r *headerRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, errors.New("http request is nil")
	}
	clone := req.Clone(req.Context())
	for key, value := range r.headers {
		clone.Header.Set(key, value)
	}
	base := r.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(clone)
}
