package llmmq

type RequestMessage struct {
	RequestID string `json:"request_id"`
	Prompt    string `json:"prompt"`
}

type TokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type ResponseMessage struct {
	RequestID string `json:"request_id"`
	Text      string `json:"text"`
	Usage     TokenUsage `json:"usage,omitempty"`
	Error     string `json:"error"`
}
