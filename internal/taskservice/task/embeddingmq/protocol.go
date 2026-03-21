package embeddingmq

type RequestMessage struct {
	RequestID string   `json:"request_id"`
	Inputs    []string `json:"inputs"`
}

type ResponseMessage struct {
	RequestID string      `json:"request_id"`
	Vectors   [][]float32 `json:"vectors"`
	Error     string      `json:"error"`
}
