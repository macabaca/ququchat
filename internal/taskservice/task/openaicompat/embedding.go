package openaicompat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

type EmbeddingClient struct {
	apiKey  string
	baseURL string
	model   string
	httpCli *http.Client
}

type EmbeddingOptions struct {
	APIKey  string
	BaseURL string
	Model   string
	Timeout time.Duration
}

func NewEmbeddingClient(opts EmbeddingOptions) (*EmbeddingClient, error) {
	apiKey := strings.TrimSpace(opts.APIKey)
	baseURL := strings.TrimSpace(opts.BaseURL)
	model := strings.TrimSpace(opts.Model)
	if apiKey == "" {
		return nil, errors.New("embedding api key is empty")
	}
	if baseURL == "" {
		return nil, errors.New("embedding base url is empty")
	}
	if model == "" {
		return nil, errors.New("embedding model is empty")
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &EmbeddingClient{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		httpCli: &http.Client{Timeout: timeout},
	}, nil
}

func (c *EmbeddingClient) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	if c == nil {
		return nil, errors.New("embedding client is nil")
	}
	cleaned := make([]string, 0, len(inputs))
	for _, input := range inputs {
		text := strings.TrimSpace(input)
		if text == "" {
			continue
		}
		cleaned = append(cleaned, text)
	}
	if len(cleaned) == 0 {
		return nil, errors.New("embedding inputs are empty")
	}
	body, err := json.Marshal(map[string]interface{}{
		"model": c.model,
		"input": cleaned,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpCli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	rawResp, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var out struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rawResp, &out); err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		if out.Error != nil && strings.TrimSpace(out.Error.Message) != "" {
			return nil, errors.New(strings.TrimSpace(out.Error.Message))
		}
		return nil, fmt.Errorf("embedding request failed: status=%d", resp.StatusCode)
	}
	if len(out.Data) == 0 {
		return nil, errors.New("embedding response has no data")
	}
	sort.Slice(out.Data, func(i, j int) bool {
		return out.Data[i].Index < out.Data[j].Index
	})
	vectors := make([][]float32, 0, len(out.Data))
	for _, item := range out.Data {
		if len(item.Embedding) == 0 {
			return nil, errors.New("embedding response contains empty vector")
		}
		vectors = append(vectors, item.Embedding)
	}
	if len(vectors) != len(cleaned) {
		return nil, errors.New("embedding response vector count mismatch")
	}
	return vectors, nil
}
