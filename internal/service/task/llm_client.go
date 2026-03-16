package tasksvc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type LLMClient interface {
	Chat(ctx context.Context, prompt string) (string, error)
}

type OpenAICompatClient struct {
	apiKey  string
	baseURL string
	model   string
	httpCli *http.Client
}

type OpenAICompatOptions struct {
	APIKey  string
	BaseURL string
	Model   string
	Timeout time.Duration
}

func NewOpenAICompatClient(opts OpenAICompatOptions) (*OpenAICompatClient, error) {
	apiKey := strings.TrimSpace(opts.APIKey)
	baseURL := strings.TrimSpace(opts.BaseURL)
	model := strings.TrimSpace(opts.Model)
	if apiKey == "" {
		return nil, errors.New("llm api key is empty")
	}
	if baseURL == "" {
		return nil, errors.New("llm base url is empty")
	}
	if model == "" {
		return nil, errors.New("llm model is empty")
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &OpenAICompatClient{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		httpCli: &http.Client{Timeout: timeout},
	}, nil
}

func (c *OpenAICompatClient) Chat(ctx context.Context, prompt string) (string, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return "", errors.New("llm prompt is empty")
	}
	body, err := json.Marshal(map[string]interface{}{
		"model": c.model,
		"messages": []map[string]string{
			{
				"role":    "user",
				"content": prompt,
			},
		},
		"stream": false,
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpCli.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	rawResp, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rawResp, &out); err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		if out.Error != nil && strings.TrimSpace(out.Error.Message) != "" {
			return "", errors.New(strings.TrimSpace(out.Error.Message))
		}
		return "", fmt.Errorf("llm request failed: status=%d", resp.StatusCode)
	}
	if len(out.Choices) == 0 {
		return "", errors.New("llm response has no choices")
	}
	content := strings.TrimSpace(out.Choices[0].Message.Content)
	content = extractAfterThinkTag(content)
	if content == "" {
		return "", errors.New("llm response content is empty")
	}
	return content, nil
}

func extractAfterThinkTag(content string) string {
	text := strings.TrimSpace(content)
	const endTag = "</think>"
	idx := strings.LastIndex(text, endTag)
	if idx < 0 {
		return text
	}
	after := strings.TrimSpace(text[idx+len(endTag):])
	if after == "" {
		return text
	}
	return after
}
