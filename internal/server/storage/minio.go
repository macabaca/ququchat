package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"ququchat/internal/config"
)

type minioStorage struct {
	client *minio.Client
}

func (s *minioStorage) Provider() string {
	return "minio"
}

func (s *minioStorage) PutObject(ctx context.Context, bucket string, key string, body io.Reader, size int64, contentType *string) error {
	if s.client == nil {
		return errors.New("minio client is nil")
	}

	opts := minio.PutObjectOptions{}
	if contentType != nil {
		opts.ContentType = *contentType
	}
	if size < 0 {
		opts.PartSize = 10 * 1024 * 1024
	}

	_, err := s.client.PutObject(ctx, bucket, key, body, size, opts)
	if err != nil {
		return err
	}
	return nil
}

func (s *minioStorage) GetObject(ctx context.Context, bucket string, key string) (io.ReadCloser, error) {
	if s.client == nil {
		return nil, errors.New("minio client is nil")
	}
	obj, err := s.client.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	return obj, nil
}

func (s *minioStorage) StatObject(ctx context.Context, bucket string, key string) (ObjectInfo, error) {
	if s.client == nil {
		return ObjectInfo{}, errors.New("minio client is nil")
	}
	info, err := s.client.StatObject(ctx, bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return ObjectInfo{}, err
	}
	return ObjectInfo{Size: info.Size, ETag: info.ETag}, nil
}

func (s *minioStorage) RemoveObject(ctx context.Context, bucket string, key string) error {
	if s.client == nil {
		return errors.New("minio client is nil")
	}
	return s.client.RemoveObject(ctx, bucket, key, minio.RemoveObjectOptions{})
}

func (s *minioStorage) PresignGetObject(ctx context.Context, bucket string, key string, expires time.Duration) (string, error) {
	if s.client == nil {
		return "", errors.New("minio client is nil")
	}
	u, err := s.client.PresignedGetObject(ctx, bucket, key, expires, nil)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

func (s *minioStorage) NewMultipartUpload(ctx context.Context, bucket string, key string, contentType *string) (string, error) {
	if s.client == nil {
		return "", errors.New("minio client is nil")
	}
	opts := minio.PutObjectOptions{}
	if contentType != nil {
		opts.ContentType = *contentType
	}
	core := minio.Core{Client: s.client}
	uploadID, err := core.NewMultipartUpload(ctx, bucket, key, opts)
	if err != nil {
		return "", err
	}
	return uploadID, nil
}

func (s *minioStorage) UploadPart(ctx context.Context, bucket string, key string, uploadID string, partNumber int, body io.Reader, size int64) (UploadedPart, error) {
	if s.client == nil {
		return UploadedPart{}, errors.New("minio client is nil")
	}
	core := minio.Core{Client: s.client}
	part, err := core.PutObjectPart(ctx, bucket, key, uploadID, partNumber, body, size, minio.PutObjectPartOptions{})
	if err != nil {
		return UploadedPart{}, err
	}
	return UploadedPart{
		PartNumber: part.PartNumber,
		ETag:       part.ETag,
		Size:       part.Size,
	}, nil
}

func (s *minioStorage) ListUploadedParts(ctx context.Context, bucket string, key string, uploadID string) ([]UploadedPart, error) {
	if s.client == nil {
		return nil, errors.New("minio client is nil")
	}
	core := minio.Core{Client: s.client}
	res, err := core.ListObjectParts(ctx, bucket, key, uploadID, 0, 0)
	if err != nil {
		return nil, err
	}
	parts := make([]UploadedPart, 0, len(res.ObjectParts))
	for _, p := range res.ObjectParts {
		parts = append(parts, UploadedPart{
			PartNumber: p.PartNumber,
			ETag:       p.ETag,
			Size:       p.Size,
		})
	}
	return parts, nil
}

func (s *minioStorage) CompleteMultipartUpload(ctx context.Context, bucket string, key string, uploadID string, parts []UploadedPart) error {
	if s.client == nil {
		return errors.New("minio client is nil")
	}
	core := minio.Core{Client: s.client}
	completeParts := make([]minio.CompletePart, 0, len(parts))
	for _, p := range parts {
		completeParts = append(completeParts, minio.CompletePart{
			PartNumber: p.PartNumber,
			ETag:       p.ETag,
		})
	}
	_, err := core.CompleteMultipartUpload(ctx, bucket, key, uploadID, completeParts, minio.PutObjectOptions{})
	return err
}

func (s *minioStorage) AbortMultipartUpload(ctx context.Context, bucket string, key string, uploadID string) error {
	if s.client == nil {
		return errors.New("minio client is nil")
	}
	core := minio.Core{Client: s.client}
	return core.AbortMultipartUpload(ctx, bucket, key, uploadID)
}

func InitMinioStorage(cfg config.Minio) (ObjectStorage, error) {
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		return nil, errors.New("minio endpoint is empty")
	}
	accessKey := strings.TrimSpace(cfg.AccessKey)
	secretKey := strings.TrimSpace(cfg.SecretKey)
	bucket := strings.TrimSpace(cfg.Bucket)
	if accessKey == "" || secretKey == "" || bucket == "" {
		return nil, errors.New("minio credentials or bucket is empty")
	}

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("init minio client: %w", err)
	}

	exists, err := client.BucketExists(context.Background(), bucket)
	if err != nil {
		return nil, fmt.Errorf("check minio bucket: %w", err)
	}
	if !exists {
		return nil, fmt.Errorf("minio bucket not found: %s", bucket)
	}

	return &minioStorage{client: client}, nil
}
