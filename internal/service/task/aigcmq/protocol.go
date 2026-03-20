package aigcmq

type GenerateRequest struct {
	Prompt            string  `json:"prompt"`
	NegativePrompt    string  `json:"negative_prompt,omitempty"`
	ImageSize         string  `json:"image_size"`
	BatchSize         int     `json:"batch_size"`
	NumInferenceSteps int     `json:"num_inference_steps"`
	GuidanceScale     float64 `json:"guidance_scale"`
}

type GeneratedImage struct {
	URL string `json:"url"`
}

type Timings struct {
	Inference float64 `json:"inference"`
}

type GenerateResult struct {
	Images  []GeneratedImage `json:"images"`
	Timings Timings          `json:"timings"`
	Seed    int64            `json:"seed"`
}

type ImageData struct {
	AttachmentID string `json:"attachment_id"`
}

type GenerateResponse struct {
	Images  []ImageData `json:"images"`
	Timings Timings     `json:"timings"`
	Seed    int64       `json:"seed"`
}

type RequestMessage struct {
	RequestID string `json:"request_id"`
	GenerateRequest
}

type ResponseMessage struct {
	RequestID string      `json:"request_id"`
	Images    []ImageData `json:"images"`
	Timings   Timings     `json:"timings"`
	Seed      int64       `json:"seed"`
	Error     string      `json:"error"`
}
