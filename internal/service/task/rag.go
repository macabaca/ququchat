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

type RAGSegment struct {
	SegmentID    string
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

type EmbeddingProvider interface {
	EmbedRawSegments(ctx context.Context, segments []RAGSegment) ([][]float32, error)
}

type VectorStore interface {
	UpsertPoints(ctx context.Context, points []VectorPoint) error
}

type RAGHandler interface {
	ExecuteRAG(ctx context.Context, payload *RAGPayload) (Result, error)
}
