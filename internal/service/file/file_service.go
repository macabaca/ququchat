package filesvc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"gorm.io/gorm"

	"ququchat/internal/models"
)

var ErrUserIDRequired = errors.New("user_id_required")
var ErrFileRequired = errors.New("file_required")
var ErrFileTooLarge = errors.New("file_too_large")
var ErrMinioClientRequired = errors.New("minio_client_required")
var ErrBucketRequired = errors.New("bucket_required")
var ErrAttachmentNotFound = errors.New("attachment_not_found")
var ErrStorageKeyRequired = errors.New("storage_key_required")
var ErrAttachmentExpired = errors.New("attachment_expired")

type Service struct {
	db           *gorm.DB
	maxSizeBytes int64
	minioClient  *minio.Client
	bucket       string
	retention    time.Duration
}

func NewService(db *gorm.DB, minioClient *minio.Client, bucket string, maxSizeBytes int64, retention time.Duration) *Service {
	if retention <= 0 {
		retention = 30 * 24 * time.Hour
	}
	return &Service{
		db:           db,
		maxSizeBytes: maxSizeBytes,
		minioClient:  minioClient,
		bucket:       strings.TrimSpace(bucket),
		retention:    retention,
	}
}

func (s *Service) Upload(userID string, file *multipart.FileHeader) (*models.Attachment, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, ErrUserIDRequired
	}
	if file == nil {
		return nil, ErrFileRequired
	}
	if s.maxSizeBytes > 0 && file.Size > s.maxSizeBytes {
		return nil, ErrFileTooLarge
	}
	if s.minioClient == nil {
		return nil, ErrMinioClientRequired
	}
	if strings.TrimSpace(s.bucket) == "" {
		return nil, ErrBucketRequired
	}

	filename := strings.TrimSpace(file.Filename)
	if filename == "" {
		filename = "file"
	}
	filename = filepath.Base(filename)
	fileNamePtr := &filename
	ext := filepath.Ext(filename)

	id := uuid.NewString()
	storedName := id
	if ext != "" {
		storedName = id + ext
	}

	src, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("open upload: %w", err)
	}
	defer src.Close()

	hasher := sha256.New()
	reader := io.TeeReader(src, hasher)
	storageKey := filepath.ToSlash(filepath.Join("uploads", storedName))
	provider := "minio"
	now := time.Now()
	expiresAt := now.Add(s.retention)
	sizeBytes := file.Size

	var mimePtr *string
	if ct := strings.TrimSpace(file.Header.Get("Content-Type")); ct != "" {
		mimePtr = &ct
	}

	uploadOptions := minio.PutObjectOptions{}
	if mimePtr != nil {
		uploadOptions.ContentType = *mimePtr
	}
	if _, err := s.minioClient.PutObject(context.Background(), s.bucket, storageKey, reader, file.Size, uploadOptions); err != nil {
		return nil, fmt.Errorf("put object: %w", err)
	}
	hashValue := hex.EncodeToString(hasher.Sum(nil))

	attachment := models.Attachment{
		ID:              id,
		UploaderUserID:  &userID,
		FileName:        fileNamePtr,
		StorageKey:      &storageKey,
		MimeType:        mimePtr,
		SizeBytes:       &sizeBytes,
		Hash:            &hashValue,
		StorageProvider: &provider,
		ExpiresAt:       &expiresAt,
		CreatedAt:       now,
	}

	if err := s.db.Create(&attachment).Error; err != nil {
		return nil, fmt.Errorf("create attachment: %w", err)
	}

	return &attachment, nil
}

func (s *Service) PresignDownload(userID string, attachmentID string, expires time.Duration) (string, error) {
	if strings.TrimSpace(userID) == "" {
		return "", ErrUserIDRequired
	}
	if strings.TrimSpace(attachmentID) == "" {
		return "", ErrAttachmentNotFound
	}
	if s.minioClient == nil {
		return "", ErrMinioClientRequired
	}
	if strings.TrimSpace(s.bucket) == "" {
		return "", ErrBucketRequired
	}

	var attachment models.Attachment
	if err := s.db.Where("id = ?", attachmentID).First(&attachment).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return "", ErrAttachmentNotFound
		}
		return "", fmt.Errorf("load attachment: %w", err)
	}
	if attachment.StorageKey == nil || strings.TrimSpace(*attachment.StorageKey) == "" {
		return "", ErrStorageKeyRequired
	}
	if attachment.ExpiresAt == nil || time.Now().After(*attachment.ExpiresAt) {
		return "", ErrAttachmentExpired
	}

	if expires <= 0 {
		expires = 15 * time.Minute
	}
	presignedURL, err := s.minioClient.PresignedGetObject(context.Background(), s.bucket, *attachment.StorageKey, expires, nil)
	if err != nil {
		return "", fmt.Errorf("presign download: %w", err)
	}

	return presignedURL.String(), nil
}
