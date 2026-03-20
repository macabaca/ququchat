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

	"ququchat/internal/service/task/aigcmq"
)

type AIGCClient struct {
	apiKey  string
	baseURL string
	model   string
	httpCli *http.Client
}

type AIGCOptions struct {
	APIKey  string
	BaseURL string
	Model   string
	Timeout time.Duration
}

func NewAIGCClient(opts AIGCOptions) (*AIGCClient, error) {
	apiKey := strings.TrimSpace(opts.APIKey)
	baseURL := strings.TrimSpace(opts.BaseURL)
	model := strings.TrimSpace(opts.Model)
	if apiKey == "" {
		return nil, errors.New("aigc api key is empty")
	}
	if baseURL == "" {
		return nil, errors.New("aigc base url is empty")
	}
	if model == "" {
		return nil, errors.New("aigc model is empty")
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	return &AIGCClient{
		apiKey:  apiKey,
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		httpCli: &http.Client{Timeout: timeout},
	}, nil
}

func (c *AIGCClient) Generate(ctx context.Context, req aigcmq.GenerateRequest) (aigcmq.GenerateResult, error) {
	if c == nil {
		return aigcmq.GenerateResult{}, errors.New("aigc client is nil")
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return aigcmq.GenerateResult{}, errors.New("aigc prompt is empty")
	}
	imageSize := strings.TrimSpace(req.ImageSize)
	if imageSize == "" {
		imageSize = "1024x1024"
	}
	batchSize := req.BatchSize
	if batchSize <= 0 {
		batchSize = 1
	}
	numInferenceSteps := req.NumInferenceSteps
	if numInferenceSteps <= 0 {
		numInferenceSteps = 20
	}
	guidanceScale := req.GuidanceScale
	if guidanceScale <= 0 {
		guidanceScale = 7.5
	}
	bodyMap := map[string]interface{}{
		"model":               c.model,
		"prompt":              prompt,
		"image_size":          imageSize,
		"batch_size":          batchSize,
		"num_inference_steps": numInferenceSteps,
		"guidance_scale":      guidanceScale,
	}
	if strings.TrimSpace(req.NegativePrompt) != "" {
		bodyMap["negative_prompt"] = strings.TrimSpace(req.NegativePrompt)
	}
	body, err := json.Marshal(bodyMap)
	if err != nil {
		return aigcmq.GenerateResult{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/images/generations", bytes.NewReader(body))
	if err != nil {
		return aigcmq.GenerateResult{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpResp, err := c.httpCli.Do(httpReq)
	if err != nil {
		return aigcmq.GenerateResult{}, err
	}
	defer httpResp.Body.Close()
	rawResp, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return aigcmq.GenerateResult{}, err
	}
	var out struct {
		Images []struct {
			URL string `json:"url"`
		} `json:"images"`
		Timings struct {
			Inference float64 `json:"inference"`
		} `json:"timings"`
		Seed    int64  `json:"seed"`
		Message string `json:"message"`
		Error   *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rawResp, &out); err != nil {
		return aigcmq.GenerateResult{}, err
	}
	if httpResp.StatusCode >= 400 {
		if out.Error != nil && strings.TrimSpace(out.Error.Message) != "" {
			return aigcmq.GenerateResult{}, errors.New(strings.TrimSpace(out.Error.Message))
		}
		if strings.TrimSpace(out.Message) != "" {
			return aigcmq.GenerateResult{}, errors.New(strings.TrimSpace(out.Message))
		}
		return aigcmq.GenerateResult{}, fmt.Errorf("aigc request failed: status=%d", httpResp.StatusCode)
	}
	images := make([]aigcmq.GeneratedImage, 0, len(out.Images))
	for _, image := range out.Images {
		url := strings.TrimSpace(image.URL)
		if url == "" {
			continue
		}
		images = append(images, aigcmq.GeneratedImage{URL: url})
	}
	if len(images) == 0 {
		return aigcmq.GenerateResult{}, errors.New("aigc response has no images")
	}
	return aigcmq.GenerateResult{
		Images: images,
		Timings: aigcmq.Timings{
			Inference: out.Timings.Inference,
		},
		Seed: out.Seed,
	}, nil
}
