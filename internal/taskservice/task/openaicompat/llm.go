package openaicompat

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

	"ququchat/internal/taskservice/task/agent/toolruntime"
)

type LLMClient struct {
	apiKey  string
	baseURL string
	model   string
	httpCli *http.Client
}

type LLMOptions struct {
	APIKey  string
	BaseURL string
	Model   string
	Timeout time.Duration
}

type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string          `json:"name"`
					Arguments json.RawMessage `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func NewLLMClient(opts LLMOptions) (*LLMClient, error) {
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
	return &LLMClient{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		httpCli: &http.Client{Timeout: timeout},
	}, nil
}

func (c *LLMClient) Chat(ctx context.Context, prompt string) (string, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return "", errors.New("llm prompt is empty")
	}
	body, err := json.Marshal(map[string]any{
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
	out, err := c.doChatCompletion(ctx, body)
	if err != nil {
		return "", err
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

func (c *LLMClient) ChatWithFunctionCalling(ctx context.Context, prompt string, tools []toolruntime.FunctionToolDefinition) (string, map[string]any, string, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return "", nil, "", errors.New("llm prompt is empty")
	}
	if len(tools) == 0 {
		return "", nil, "", errors.New("function calling tools are empty")
	}
	body, err := json.Marshal(map[string]any{
		"model": c.model,
		"messages": []map[string]string{
			{
				"role":    "user",
				"content": prompt,
			},
		},
		"stream":      false,
		"tools":       tools,
		"tool_choice": "required",
	})
	if err != nil {
		return "", nil, "", err
	}
	out, err := c.doChatCompletion(ctx, body)
	if err != nil {
		return "", nil, "", err
	}
	if len(out.Choices) == 0 {
		return "", nil, "", errors.New("llm response has no choices")
	}
	message := out.Choices[0].Message
	if len(message.ToolCalls) == 0 {
		return "", nil, strings.TrimSpace(message.Content), errors.New("llm response has no tool calls")
	}
	toolCall := message.ToolCalls[0]
	toolName := strings.TrimSpace(toolCall.Function.Name)
	if toolName == "" {
		return "", nil, strings.TrimSpace(message.Content), errors.New("llm tool call name is empty")
	}
	args := map[string]any{}
	if len(toolCall.Function.Arguments) > 0 {
		if err := json.Unmarshal(toolCall.Function.Arguments, &args); err != nil {
			return "", nil, strings.TrimSpace(string(toolCall.Function.Arguments)), errors.New("llm tool call arguments is not valid json object")
		}
	}
	raw := map[string]any{
		"tool_calls": []map[string]any{
			{
				"id":   strings.TrimSpace(toolCall.ID),
				"type": strings.TrimSpace(toolCall.Type),
				"function": map[string]any{
					"name":      toolName,
					"arguments": args,
				},
			},
		},
	}
	rawBytes, marshalErr := json.Marshal(raw)
	if marshalErr != nil {
		return toolName, args, strings.TrimSpace(message.Content), nil
	}
	return toolName, args, strings.TrimSpace(string(rawBytes)), nil
}

func (c *LLMClient) doChatCompletion(ctx context.Context, body []byte) (chatCompletionResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return chatCompletionResponse{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpCli.Do(req)
	if err != nil {
		return chatCompletionResponse{}, err
	}
	defer resp.Body.Close()
	rawResp, err := io.ReadAll(resp.Body)
	if err != nil {
		return chatCompletionResponse{}, err
	}
	var out chatCompletionResponse
	if err := json.Unmarshal(rawResp, &out); err != nil {
		return chatCompletionResponse{}, err
	}
	if resp.StatusCode >= 400 {
		if out.Error != nil && strings.TrimSpace(out.Error.Message) != "" {
			return chatCompletionResponse{}, errors.New(strings.TrimSpace(out.Error.Message))
		}
		return chatCompletionResponse{}, fmt.Errorf("llm request failed: status=%d", resp.StatusCode)
	}
	return out, nil
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
