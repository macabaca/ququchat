package llmmq

type RequestMessage struct {
	RequestID string `json:"request_id"`
	Prompt    string `json:"prompt"`
}

type ResponseMessage struct {
	RequestID string `json:"request_id"`
	Text      string `json:"text"`
	Error     string `json:"error"`
}
