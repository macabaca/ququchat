package tasksvc

import "context"

type SubmitRAGRequest struct {
	RequestID            string
	Priority             Priority
	RoomID               string
	SegmentGapSeconds    int
	MaxCharsPerSegment   int
	MaxMessagesPerSeg    int
	OverlapMessages      int
	MinMessageSequenceID int64
}

type SubmitRAGSearchRequest struct {
	RequestID string
	Priority  Priority
	RoomID    string
	Query     string
	TopK      int
}

type SubmitRAGAddMemoryRequest struct {
	RequestID          string
	Priority           Priority
	RoomID             string
	StartSequenceID    int64
	EndSequenceID      int64
	SegmentGapSeconds  int
	MaxCharsPerSegment int
	MaxMessagesPerSeg  int
	OverlapMessages    int
}

type RAGSegment struct {
	SegmentID    string
	PointID      string
	RoomID       string
	StartSeq     int64
	EndSeq       int64
	StartUnixSec int64
	EndUnixSec   int64
	MessageCount int
	RawText      string
	TextPreview  string
}

type VectorPoint struct {
	PointID string
	Raw     []float32
	Summary []float32
	Payload map[string]interface{}
}

type VectorSearchHit struct {
	PointID string
	Score   float64
	Payload map[string]interface{}
}

type EmbeddingProvider interface {
	EmbedRawSegments(ctx context.Context, segments []RAGSegment) ([][]float32, error)
	EmbedTexts(ctx context.Context, inputs []string) ([][]float32, error)
}

type VectorStore interface {
	UpsertPoints(ctx context.Context, points []VectorPoint) error
	SearchRaw(ctx context.Context, roomID string, vector []float32, topK int) ([]VectorSearchHit, error)
}

type RAGHandler interface {
	ExecuteRAG(ctx context.Context, payload *RAGPayload) (Result, error)
	ExecuteRAGSearch(ctx context.Context, payload *RAGSearchPayload) (Result, error)
	ExecuteRAGAddMemory(ctx context.Context, payload *RAGAddMemoryPayload) (Result, error)
}
