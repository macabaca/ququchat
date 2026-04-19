package raghandler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	tasksvc "ququchat/internal/taskservice/task"
)

type HTTPRerankClientOptions struct {
	Endpoint string
	Timeout  time.Duration
}

type HTTPRerankClient struct {
	endpoint string
	httpCli  *http.Client
}

type httpRerankRequest struct {
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
}

type httpRerankResponse struct {
	Scores []float64 `json:"scores"`
}

func NewHTTPRerankClient(opts HTTPRerankClientOptions) *HTTPRerankClient {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	return &HTTPRerankClient{
		endpoint: strings.TrimSpace(opts.Endpoint),
		httpCli:  &http.Client{Timeout: timeout},
	}
}

func (c *HTTPRerankClient) Score(ctx context.Context, query string, documents []string) ([]float64, error) {
	if c == nil || strings.TrimSpace(c.endpoint) == "" {
		return nil, fmt.Errorf("rerank endpoint is empty")
	}
	payload := httpRerankRequest{
		Query:     strings.TrimSpace(query),
		Documents: documents,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpCli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("rerank request failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	var out httpRerankResponse
	if err := json.Unmarshal(respBody, &out); err != nil {
		return nil, err
	}
	if len(out.Scores) != len(documents) {
		return nil, fmt.Errorf("rerank response score size mismatch: want=%d got=%d", len(documents), len(out.Scores))
	}
	return out.Scores, nil
}

func resolveSearchHitText(hit tasksvc.VectorSearchHit, rawTextBySegmentID map[string]string) string {
	segmentID := strings.TrimSpace(fmt.Sprint(hit.Payload["segment_id"]))
	rawText := strings.TrimSpace(rawTextBySegmentID[segmentID])
	if rawText != "" {
		return rawText
	}
	rawText = strings.TrimSpace(fmt.Sprint(hit.Payload["raw_text"]))
	if rawText != "" {
		return rawText
	}
	summary := strings.TrimSpace(fmt.Sprint(hit.Payload["summary"]))
	if summary != "" {
		return summary
	}
	return strings.TrimSpace(fmt.Sprint(hit.Payload["text_preview"]))
}
