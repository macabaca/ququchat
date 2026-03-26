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

type QdrantVectorStoreOptions struct {
	BaseURL          string
	APIKey           string
	Collection       string
	Timeout          time.Duration
	SummaryVectorDim int
}

type QdrantVectorStore struct {
	baseURL          string
	apiKey           string
	collection       string
	summaryVectorDim int
	httpCli          *http.Client
}

func NewQdrantVectorStore(opts QdrantVectorStoreOptions) (*QdrantVectorStore, error) {
	baseURL := strings.TrimSpace(opts.BaseURL)
	collection := strings.TrimSpace(opts.Collection)
	if baseURL == "" {
		return nil, errors.New("qdrant base url is empty")
	}
	if collection == "" {
		return nil, errors.New("qdrant collection is empty")
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	return &QdrantVectorStore{
		baseURL:          strings.TrimRight(baseURL, "/"),
		apiKey:           strings.TrimSpace(opts.APIKey),
		collection:       collection,
		summaryVectorDim: opts.SummaryVectorDim,
		httpCli:          &http.Client{Timeout: timeout},
	}, nil
}

func (s *QdrantVectorStore) UpsertPoints(ctx context.Context, points []VectorPoint) error {
	if s == nil {
		return errors.New("qdrant vector store is nil")
	}
	if len(points) == 0 {
		return nil
	}
	items := make([]map[string]interface{}, 0, len(points))
	for _, p := range points {
		pointID := strings.TrimSpace(p.PointID)
		if pointID == "" {
			return errors.New("vector point id is empty")
		}
		if len(p.Raw) == 0 {
			return fmt.Errorf("vector point raw is empty: %s", pointID)
		}
		summary := p.Summary
		if len(summary) == 0 && s.summaryVectorDim > 0 {
			summary = make([]float32, s.summaryVectorDim)
		}
		if s.summaryVectorDim > 0 && len(summary) > 0 && len(summary) != s.summaryVectorDim {
			return fmt.Errorf("vector point summary dim mismatch: point=%s want=%d got=%d", pointID, s.summaryVectorDim, len(summary))
		}
		vector := map[string]interface{}{
			"raw": p.Raw,
		}
		if len(summary) > 0 {
			vector["summary"] = summary
		}
		item := map[string]interface{}{
			"id":     pointID,
			"vector": vector,
		}
		if p.Payload != nil {
			item["payload"] = p.Payload
		}
		items = append(items, item)
	}
	body, err := json.Marshal(map[string]interface{}{
		"points": items,
	})
	if err != nil {
		return err
	}
	url := s.baseURL + "/collections/" + s.collection + "/points?wait=true"
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.apiKey != "" {
		req.Header.Set("api-key", s.apiKey)
	}
	resp, err := s.httpCli.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var out struct {
		Status interface{} `json:"status"`
		Result interface{} `json:"result"`
		Error  interface{} `json:"error"`
	}
	if len(respBody) > 0 {
		_ = json.Unmarshal(respBody, &out)
	}
	if resp.StatusCode >= 400 {
		if out.Error != nil {
			return fmt.Errorf("qdrant upsert failed: status=%d error=%v", resp.StatusCode, out.Error)
		}
		return fmt.Errorf("qdrant upsert failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	statusText := strings.ToLower(strings.TrimSpace(fmt.Sprint(out.Status)))
	if statusText != "" && statusText != "ok" {
		return fmt.Errorf("qdrant upsert response status is not ok: %v", out.Status)
	}
	return nil
}

func (s *QdrantVectorStore) SearchRaw(ctx context.Context, roomID string, vector []float32, topK int) ([]VectorSearchHit, error) {
	return s.searchByNamedVector(ctx, roomID, "raw", vector, topK, false)
}

func (s *QdrantVectorStore) SearchSummary(ctx context.Context, roomID string, vector []float32, topK int) ([]VectorSearchHit, error) {
	return s.searchByNamedVector(ctx, roomID, "summary", vector, topK, true)
}

func (s *QdrantVectorStore) searchByNamedVector(ctx context.Context, roomID string, vectorName string, vector []float32, topK int, requireSummaryReady bool) ([]VectorSearchHit, error) {
	if s == nil {
		return nil, errors.New("qdrant vector store is nil")
	}
	if len(vector) == 0 {
		return nil, errors.New("query vector is empty")
	}
	cleanRoomID := strings.TrimSpace(roomID)
	if cleanRoomID == "" {
		return nil, errors.New("room id is empty")
	}
	if topK <= 0 {
		topK = 5
	}
	mustFilters := []map[string]interface{}{
		{
			"key": "room_id",
			"match": map[string]interface{}{
				"value": cleanRoomID,
			},
		},
	}
	if requireSummaryReady {
		mustFilters = append(mustFilters, map[string]interface{}{
			"key": "summary_ready",
			"match": map[string]interface{}{
				"value": true,
			},
		})
	}
	body, err := json.Marshal(map[string]interface{}{
		"vector": map[string]interface{}{
			"name":   vectorName,
			"vector": vector,
		},
		"limit":        topK,
		"with_payload": true,
		"filter": map[string]interface{}{
			"must": mustFilters,
		},
	})
	if err != nil {
		return nil, err
	}
	url := s.baseURL + "/collections/" + s.collection + "/points/search"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.apiKey != "" {
		req.Header.Set("api-key", s.apiKey)
	}
	resp, err := s.httpCli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var out struct {
		Status string `json:"status"`
		Result []struct {
			ID      interface{}            `json:"id"`
			Score   float64                `json:"score"`
			Payload map[string]interface{} `json:"payload"`
		} `json:"result"`
		Error interface{} `json:"error"`
	}
	if len(respBody) > 0 {
		_ = json.Unmarshal(respBody, &out)
	}
	if resp.StatusCode >= 400 {
		if out.Error != nil {
			return nil, fmt.Errorf("qdrant search failed: status=%d error=%v", resp.StatusCode, out.Error)
		}
		return nil, fmt.Errorf("qdrant search failed: status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	statusText := strings.ToLower(strings.TrimSpace(out.Status))
	if statusText != "" && statusText != "ok" {
		return nil, fmt.Errorf("qdrant search response status is not ok: %s", out.Status)
	}
	hits := make([]VectorSearchHit, 0, len(out.Result))
	for _, item := range out.Result {
		hits = append(hits, VectorSearchHit{
			PointID: strings.TrimSpace(fmt.Sprint(item.ID)),
			Score:   item.Score,
			Payload: item.Payload,
		})
	}
	return hits, nil
}
