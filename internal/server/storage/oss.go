package storage

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"

	"ququchat/internal/config"
)

type ossStorage struct {
	client *oss.Client
}

func (s *ossStorage) Provider() string {
	return "oss"
}

func (s *ossStorage) PutObject(ctx context.Context, bucket string, key string, body io.Reader, size int64, contentType *string) error {
	if s.client == nil {
		return errors.New("oss client is nil")
	}
	req := &oss.PutObjectRequest{
		Bucket: oss.Ptr(bucket),
		Key:    oss.Ptr(key),
		Body:   body,
	}
	if contentType != nil && strings.TrimSpace(*contentType) != "" {
		req.ContentType = contentType
	}
	if size >= 0 {
		req.ContentLength = oss.Ptr(size)
	}
	_, err := s.client.PutObject(ctx, req)
	return err
}

func (s *ossStorage) GetObject(ctx context.Context, bucket string, key string) (io.ReadCloser, error) {
	if s.client == nil {
		return nil, errors.New("oss client is nil")
	}
	out, err := s.client.GetObject(ctx, &oss.GetObjectRequest{
		Bucket: oss.Ptr(bucket),
		Key:    oss.Ptr(key),
	})
	if err != nil {
		return nil, err
	}
	return out.Body, nil
}

func (s *ossStorage) StatObject(ctx context.Context, bucket string, key string) (ObjectInfo, error) {
	if s.client == nil {
		return ObjectInfo{}, errors.New("oss client is nil")
	}
	out, err := s.client.HeadObject(ctx, &oss.HeadObjectRequest{
		Bucket: oss.Ptr(bucket),
		Key:    oss.Ptr(key),
	})
	if err != nil {
		return ObjectInfo{}, err
	}

	etag := ""
	if out.ETag != nil {
		etag = strings.Trim(*out.ETag, "\"")
	}

	size := out.ContentLength
	if size < 0 {
		size = 0
	}
	return ObjectInfo{Size: size, ETag: etag}, nil
}

func (s *ossStorage) RemoveObject(ctx context.Context, bucket string, key string) error {
	if s.client == nil {
		return errors.New("oss client is nil")
	}
	_, err := s.client.DeleteObject(ctx, &oss.DeleteObjectRequest{
		Bucket: oss.Ptr(bucket),
		Key:    oss.Ptr(key),
	})
	return err
}

func (s *ossStorage) PresignGetObject(ctx context.Context, bucket string, key string, expires time.Duration) (string, error) {
	if s.client == nil {
		return "", errors.New("oss client is nil")
	}
	out, err := s.client.Presign(ctx, &oss.GetObjectRequest{
		Bucket: oss.Ptr(bucket),
		Key:    oss.Ptr(key),
	}, oss.PresignExpires(expires))
	if err != nil {
		return "", err
	}
	return out.URL, nil
}

func (s *ossStorage) NewMultipartUpload(ctx context.Context, bucket string, key string, contentType *string) (string, error) {
	if s.client == nil {
		return "", errors.New("oss client is nil")
	}
	req := &oss.InitiateMultipartUploadRequest{
		Bucket: oss.Ptr(bucket),
		Key:    oss.Ptr(key),
	}
	if contentType != nil && strings.TrimSpace(*contentType) != "" {
		req.ContentType = contentType
	}
	out, err := s.client.InitiateMultipartUpload(ctx, req)
	if err != nil {
		return "", err
	}
	if out.UploadId == nil || strings.TrimSpace(*out.UploadId) == "" {
		return "", errors.New("oss initiate multipart upload: empty upload id")
	}
	return *out.UploadId, nil
}

func (s *ossStorage) UploadPart(ctx context.Context, bucket string, key string, uploadID string, partNumber int, body io.Reader, size int64) (UploadedPart, error) {
	if s.client == nil {
		return UploadedPart{}, errors.New("oss client is nil")
	}
	req := &oss.UploadPartRequest{
		Bucket:     oss.Ptr(bucket),
		Key:        oss.Ptr(key),
		UploadId:   oss.Ptr(uploadID),
		PartNumber: int32(partNumber),
		Body:       body,
	}
	if size >= 0 {
		req.ContentLength = oss.Ptr(size)
	}
	out, err := s.client.UploadPart(ctx, req)
	if err != nil {
		return UploadedPart{}, err
	}
	etag := ""
	if out.ETag != nil {
		etag = strings.Trim(*out.ETag, "\"")
	}
	return UploadedPart{PartNumber: partNumber, ETag: etag, Size: size}, nil
}

func (s *ossStorage) ListUploadedParts(ctx context.Context, bucket string, key string, uploadID string) ([]UploadedPart, error) {
	if s.client == nil {
		return nil, errors.New("oss client is nil")
	}
	out, err := s.client.ListParts(ctx, &oss.ListPartsRequest{
		Bucket:   oss.Ptr(bucket),
		Key:      oss.Ptr(key),
		UploadId: oss.Ptr(uploadID),
		MaxParts: 1000,
	})
	if err != nil {
		return nil, err
	}
	parts := make([]UploadedPart, 0, len(out.Parts))
	for _, p := range out.Parts {
		etag := ""
		if p.ETag != nil {
			etag = strings.Trim(*p.ETag, "\"")
		}
		parts = append(parts, UploadedPart{
			PartNumber: int(p.PartNumber),
			ETag:       etag,
			Size:       p.Size,
		})
	}
	return parts, nil
}

func (s *ossStorage) CompleteMultipartUpload(ctx context.Context, bucket string, key string, uploadID string, parts []UploadedPart) error {
	if s.client == nil {
		return errors.New("oss client is nil")
	}
	uploadParts := make([]oss.UploadPart, 0, len(parts))
	for _, p := range parts {
		etag := strings.Trim(p.ETag, "\"")
		uploadParts = append(uploadParts, oss.UploadPart{
			PartNumber: int32(p.PartNumber),
			ETag:       oss.Ptr(etag),
		})
	}
	_, err := s.client.CompleteMultipartUpload(ctx, &oss.CompleteMultipartUploadRequest{
		Bucket:   oss.Ptr(bucket),
		Key:      oss.Ptr(key),
		UploadId: oss.Ptr(uploadID),
		CompleteMultipartUpload: &oss.CompleteMultipartUpload{
			Parts: uploadParts,
		},
	})
	return err
}

func (s *ossStorage) AbortMultipartUpload(ctx context.Context, bucket string, key string, uploadID string) error {
	if s.client == nil {
		return errors.New("oss client is nil")
	}
	_, err := s.client.AbortMultipartUpload(ctx, &oss.AbortMultipartUploadRequest{
		Bucket:   oss.Ptr(bucket),
		Key:      oss.Ptr(key),
		UploadId: oss.Ptr(uploadID),
	})
	return err
}

func InitOSSStorage(cfg config.OSS) (ObjectStorage, error) {
	region := strings.TrimSpace(cfg.Region)
	if region == "" {
		return nil, errors.New("oss region is empty")
	}
	bucket := strings.TrimSpace(cfg.Bucket)
	if bucket == "" {
		return nil, errors.New("oss bucket is empty")
	}

	ossCfg := oss.LoadDefaultConfig().
		WithRegion(region).
		WithCredentialsProvider(credentials.NewStaticCredentialsProvider(strings.TrimSpace(cfg.AccessKeyID), strings.TrimSpace(cfg.AccessKeySecret), strings.TrimSpace(cfg.SecurityToken))).
		WithDisableSSL(cfg.DisableSSL).
		WithUseCName(cfg.UseCName).
		WithUsePathStyle(cfg.UsePathStyle)

	if ep := strings.TrimSpace(cfg.Endpoint); ep != "" {
		ossCfg = ossCfg.WithEndpoint(ep)
	}

	client := oss.NewClient(ossCfg)

	return &ossStorage{client: client}, nil
}
