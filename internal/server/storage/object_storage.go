package storage

import (
	"context"
	"io"
	"time"
)

type ObjectInfo struct {
	Size int64
	ETag string
}

type UploadedPart struct {
	PartNumber int   `json:"part_number"`
	ETag       string `json:"etag"`
	Size       int64  `json:"size"`
}

type ObjectStorage interface {
	Provider() string

	PutObject(ctx context.Context, bucket string, key string, body io.Reader, size int64, contentType *string) error
	GetObject(ctx context.Context, bucket string, key string) (io.ReadCloser, error)
	StatObject(ctx context.Context, bucket string, key string) (ObjectInfo, error)
	RemoveObject(ctx context.Context, bucket string, key string) error

	PresignGetObject(ctx context.Context, bucket string, key string, expires time.Duration) (string, error)

	NewMultipartUpload(ctx context.Context, bucket string, key string, contentType *string) (string, error)
	UploadPart(ctx context.Context, bucket string, key string, uploadID string, partNumber int, body io.Reader, size int64) (UploadedPart, error)
	ListUploadedParts(ctx context.Context, bucket string, key string, uploadID string) ([]UploadedPart, error)
	CompleteMultipartUpload(ctx context.Context, bucket string, key string, uploadID string, parts []UploadedPart) error
	AbortMultipartUpload(ctx context.Context, bucket string, key string, uploadID string) error
}

