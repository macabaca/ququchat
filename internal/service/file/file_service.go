package filesvc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"mime/multipart"
	"path/filepath"
	"sort"
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
var ErrEmptyFile = errors.New("empty_file")
var ErrUploadIDRequired = errors.New("upload_id_required")
var ErrPartNumberInvalid = errors.New("part_number_invalid")
var ErrChecksumMismatch = errors.New("checksum_mismatch")

const thumbMaxDimension = 320
const thumbJPEGQuality = 80
const thumbRetryCount = 3
const thumbRetryDelay = 200 * time.Millisecond
const thumbMaxSourceBytes = int64(10 * 1024 * 1024)

type thumbResult struct {
	attachment *models.Attachment
	origW      int
	origH      int
	thumbW     int
	thumbH     int
}

type countReader struct {
	r   io.Reader
	n   int64
	max int64
}

func (c *countReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 {
		c.n += int64(n)
		if c.max > 0 && c.n > c.max {
			return n, ErrFileTooLarge
		}
	}
	return n, err
}

type Service struct {
	db           *gorm.DB
	maxSizeBytes int64
	minioClient  *minio.Client
	bucket       string
	retention    time.Duration
}

func isImageMime(mimePtr *string) bool {
	if mimePtr == nil {
		return false
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(*mimePtr)), "image/")
}

func shouldGenerateThumbnail(mimePtr *string, sizeBytes *int64) bool {
	if !isImageMime(mimePtr) {
		return false
	}
	if sizeBytes == nil || *sizeBytes <= 0 {
		return true
	}
	return *sizeBytes <= thumbMaxSourceBytes
}

func resizeNearest(src image.Image, width int, height int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	b := src.Bounds()
	srcW := b.Dx()
	srcH := b.Dy()
	if srcW == 0 || srcH == 0 {
		return dst
	}
	for y := 0; y < height; y++ {
		sy := b.Min.Y + (y*srcH)/height
		for x := 0; x < width; x++ {
			sx := b.Min.X + (x*srcW)/width
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}

func resizeToMax(src image.Image, maxDim int) (image.Image, int, int, int, int) {
	b := src.Bounds()
	origW := b.Dx()
	origH := b.Dy()
	if maxDim <= 0 || (origW <= maxDim && origH <= maxDim) {
		return src, origW, origH, origW, origH
	}
	var newW int
	var newH int
	if origW >= origH {
		newW = maxDim
		newH = int(float64(origH) * float64(maxDim) / float64(origW))
	} else {
		newH = maxDim
		newW = int(float64(origW) * float64(maxDim) / float64(origH))
	}
	if newW <= 0 {
		newW = 1
	}
	if newH <= 0 {
		newH = 1
	}
	return resizeNearest(src, newW, newH), origW, origH, newW, newH
}

func (s *Service) createThumbnailFromImage(userID string, original *models.Attachment, img image.Image) (*models.Attachment, int, int, int, int, error) {
	thumbImg, origW, origH, thumbW, thumbH := resizeToMax(img, thumbMaxDimension)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, thumbImg, &jpeg.Options{Quality: thumbJPEGQuality}); err != nil {
		return nil, origW, origH, 0, 0, err
	}
	thumbID := uuid.NewString()
	storedName := thumbID + ".jpg"
	storageKey := filepath.ToSlash(filepath.Join("uploads", "thumbs", storedName))
	contentType := "image/jpeg"
	provider := "minio"
	now := time.Now()
	expiresAt := now.Add(s.retention)
	sizeBytes := int64(buf.Len())
	hashValue := sha256.Sum256(buf.Bytes())
	hashHex := hex.EncodeToString(hashValue[:])
	uploadOptions := minio.PutObjectOptions{ContentType: contentType}
	if _, err := s.minioClient.PutObject(context.Background(), s.bucket, storageKey, bytes.NewReader(buf.Bytes()), sizeBytes, uploadOptions); err != nil {
		return nil, origW, origH, 0, 0, err
	}
	fileName := "thumb.jpg"
	if original.FileName != nil && strings.TrimSpace(*original.FileName) != "" {
		base := strings.TrimSpace(*original.FileName)
		fileName = base + ".thumb.jpg"
	}
	thumbAttachment := models.Attachment{
		ID:              thumbID,
		UploaderUserID:  original.UploaderUserID,
		FileName:        &fileName,
		StorageKey:      &storageKey,
		MimeType:        &contentType,
		SizeBytes:       &sizeBytes,
		Hash:            &hashHex,
		StorageProvider: &provider,
		ImageWidth:      &thumbW,
		ImageHeight:     &thumbH,
		ExpiresAt:       &expiresAt,
		CreatedAt:       now,
	}
	if err := s.db.Create(&thumbAttachment).Error; err != nil {
		return nil, origW, origH, 0, 0, err
	}
	return &thumbAttachment, origW, origH, thumbW, thumbH, nil
}

func (s *Service) tryCreateThumbnail(create func() (*models.Attachment, int, int, int, int, error)) *thumbResult {
	for i := 0; i < thumbRetryCount; i++ {
		attachment, origW, origH, thumbW, thumbH, err := create()
		if err == nil && attachment != nil {
			return &thumbResult{
				attachment: attachment,
				origW:      origW,
				origH:      origH,
				thumbW:     thumbW,
				thumbH:     thumbH,
			}
		}
		if i < thumbRetryCount-1 {
			time.Sleep(thumbRetryDelay * time.Duration(i+1))
		}
	}
	return nil
}

func (s *Service) ensureThumbnailForAttachment(userID string, attachment *models.Attachment) {
	if attachment == nil || attachment.StorageKey == nil || strings.TrimSpace(*attachment.StorageKey) == "" {
		return
	}
	if !shouldGenerateThumbnail(attachment.MimeType, attachment.SizeBytes) {
		return
	}
	if attachment.ThumbAttachmentID != nil && strings.TrimSpace(*attachment.ThumbAttachmentID) != "" {
		return
	}
	result := s.tryCreateThumbnail(func() (*models.Attachment, int, int, int, int, error) {
		obj, err := s.minioClient.GetObject(context.Background(), s.bucket, *attachment.StorageKey, minio.GetObjectOptions{})
		if err != nil {
			return nil, 0, 0, 0, 0, err
		}
		defer obj.Close()
		img, _, err := image.Decode(obj)
		if err != nil {
			return nil, 0, 0, 0, 0, err
		}
		return s.createThumbnailFromImage(userID, attachment, img)
	})
	if result != nil {
		attachment.ImageWidth = &result.origW
		attachment.ImageHeight = &result.origH
		attachment.ThumbAttachmentID = &result.attachment.ID
		attachment.ThumbWidth = &result.thumbW
		attachment.ThumbHeight = &result.thumbH
		_ = s.db.Model(&models.Attachment{}).Where("id = ?", attachment.ID).Updates(map[string]interface{}{
			"image_width":         result.origW,
			"image_height":        result.origH,
			"thumb_attachment_id": result.attachment.ID,
			"thumb_width":         result.thumbW,
			"thumb_height":        result.thumbH,
		}).Error
	}
}

type MultipartSession struct {
	UploadID     string
	AttachmentID string
	StorageKey   string
	FileName     string
	MimeType     *string
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

	storageKey := filepath.ToSlash(filepath.Join("uploads", storedName))
	provider := "minio"
	now := time.Now()
	expiresAt := now.Add(s.retention)
	sizeBytes := file.Size
	var reader io.Reader
	uploadSize := sizeBytes
	hasher := sha256.New()
	if sizeBytes > 0 {
		if s.maxSizeBytes > 0 && sizeBytes > s.maxSizeBytes {
			return nil, ErrFileTooLarge
		}
		reader = io.TeeReader(src, hasher)
	} else {
		reader = io.TeeReader(&countReader{r: src, max: s.maxSizeBytes}, hasher)
		uploadSize = -1
	}

	var mimePtr *string
	if ct := strings.TrimSpace(file.Header.Get("Content-Type")); ct != "" {
		mimePtr = &ct
	}

	uploadOptions := minio.PutObjectOptions{}
	if mimePtr != nil {
		uploadOptions.ContentType = *mimePtr
	}
	if uploadSize < 0 {
		uploadOptions.PartSize = 10 * 1024 * 1024
	}
	if _, err := s.minioClient.PutObject(context.Background(), s.bucket, storageKey, reader, uploadSize, uploadOptions); err != nil {
		if errors.Is(err, ErrFileTooLarge) {
			return nil, ErrFileTooLarge
		}
		return nil, fmt.Errorf("put object: %w", err)
	}
	hashValue := hex.EncodeToString(hasher.Sum(nil))
	stat, err := s.minioClient.StatObject(context.Background(), s.bucket, storageKey, minio.StatObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("stat object: %w", err)
	}
	if stat.Size > 0 {
		sizeBytes = stat.Size
	}
	if sizeBytes <= 0 {
		_ = s.minioClient.RemoveObject(context.Background(), s.bucket, storageKey, minio.RemoveObjectOptions{})
		return nil, ErrEmptyFile
	}

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
	if shouldGenerateThumbnail(mimePtr, &sizeBytes) {
		result := s.tryCreateThumbnail(func() (*models.Attachment, int, int, int, int, error) {
			imgFile, err := file.Open()
			if err != nil {
				return nil, 0, 0, 0, 0, err
			}
			defer imgFile.Close()
			img, _, err := image.Decode(imgFile)
			if err != nil {
				return nil, 0, 0, 0, 0, err
			}
			return s.createThumbnailFromImage(userID, &attachment, img)
		})
		if result != nil {
			attachment.ImageWidth = &result.origW
			attachment.ImageHeight = &result.origH
			attachment.ThumbAttachmentID = &result.attachment.ID
			attachment.ThumbWidth = &result.thumbW
			attachment.ThumbHeight = &result.thumbH
			_ = s.db.Model(&models.Attachment{}).Where("id = ?", attachment.ID).Updates(map[string]interface{}{
				"image_width":         result.origW,
				"image_height":        result.origH,
				"thumb_attachment_id": result.attachment.ID,
				"thumb_width":         result.thumbW,
				"thumb_height":        result.thumbH,
			}).Error
		}
	}

	return &attachment, nil
}

func (s *Service) StartMultipartUpload(userID string, filename string, mimeType string) (*MultipartSession, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, ErrUserIDRequired
	}
	if s.minioClient == nil {
		return nil, ErrMinioClientRequired
	}
	if strings.TrimSpace(s.bucket) == "" {
		return nil, ErrBucketRequired
	}
	name := strings.TrimSpace(filename)
	if name == "" {
		name = "file"
	}
	name = filepath.Base(name)
	ext := filepath.Ext(name)
	id := uuid.NewString()
	storedName := id
	if ext != "" {
		storedName = id + ext
	}
	storageKey := filepath.ToSlash(filepath.Join("uploads", storedName))
	var mimePtr *string
	if mt := strings.TrimSpace(mimeType); mt != "" {
		mimePtr = &mt
	}
	opts := minio.PutObjectOptions{}
	if mimePtr != nil {
		opts.ContentType = *mimePtr
	}
	core := minio.Core{Client: s.minioClient}
	uploadID, err := core.NewMultipartUpload(context.Background(), s.bucket, storageKey, opts)
	if err != nil {
		return nil, fmt.Errorf("new multipart upload: %w", err)
	}
	return &MultipartSession{
		UploadID:     uploadID,
		AttachmentID: id,
		StorageKey:   storageKey,
		FileName:     name,
		MimeType:     mimePtr,
	}, nil
}

func (s *Service) UploadPart(userID string, storageKey string, uploadID string, partNumber int, reader io.Reader, size int64) (minio.ObjectPart, error) {
	if strings.TrimSpace(userID) == "" {
		return minio.ObjectPart{}, ErrUserIDRequired
	}
	if strings.TrimSpace(uploadID) == "" {
		return minio.ObjectPart{}, ErrUploadIDRequired
	}
	if strings.TrimSpace(storageKey) == "" {
		return minio.ObjectPart{}, ErrStorageKeyRequired
	}
	if partNumber <= 0 {
		return minio.ObjectPart{}, ErrPartNumberInvalid
	}
	if s.minioClient == nil {
		return minio.ObjectPart{}, ErrMinioClientRequired
	}
	if strings.TrimSpace(s.bucket) == "" {
		return minio.ObjectPart{}, ErrBucketRequired
	}
	core := minio.Core{Client: s.minioClient}
	part, err := core.PutObjectPart(context.Background(), s.bucket, storageKey, uploadID, partNumber, reader, size, minio.PutObjectPartOptions{})
	if err != nil {
		return minio.ObjectPart{}, fmt.Errorf("put object part: %w", err)
	}
	return part, nil
}

func (s *Service) ListUploadedParts(userID string, storageKey string, uploadID string) ([]minio.ObjectPart, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, ErrUserIDRequired
	}
	if strings.TrimSpace(uploadID) == "" {
		return nil, ErrUploadIDRequired
	}
	if strings.TrimSpace(storageKey) == "" {
		return nil, ErrStorageKeyRequired
	}
	if s.minioClient == nil {
		return nil, ErrMinioClientRequired
	}
	if strings.TrimSpace(s.bucket) == "" {
		return nil, ErrBucketRequired
	}
	core := minio.Core{Client: s.minioClient}
	result, err := core.ListObjectParts(context.Background(), s.bucket, storageKey, uploadID, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("list object parts: %w", err)
	}
	return result.ObjectParts, nil
}

func (s *Service) AbortMultipartUpload(userID string, storageKey string, uploadID string) error {
	if strings.TrimSpace(userID) == "" {
		return ErrUserIDRequired
	}
	if strings.TrimSpace(uploadID) == "" {
		return ErrUploadIDRequired
	}
	if strings.TrimSpace(storageKey) == "" {
		return ErrStorageKeyRequired
	}
	if s.minioClient == nil {
		return ErrMinioClientRequired
	}
	if strings.TrimSpace(s.bucket) == "" {
		return ErrBucketRequired
	}
	core := minio.Core{Client: s.minioClient}
	if err := core.AbortMultipartUpload(context.Background(), s.bucket, storageKey, uploadID); err != nil {
		return fmt.Errorf("abort multipart upload: %w", err)
	}
	return nil
}

func (s *Service) CompleteMultipartUpload(userID string, session MultipartSession, parts []minio.ObjectPart, expectedSHA256 string) (*models.Attachment, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, ErrUserIDRequired
	}
	if strings.TrimSpace(session.UploadID) == "" {
		return nil, ErrUploadIDRequired
	}
	if strings.TrimSpace(session.StorageKey) == "" {
		return nil, ErrStorageKeyRequired
	}
	if s.minioClient == nil {
		return nil, ErrMinioClientRequired
	}
	if strings.TrimSpace(s.bucket) == "" {
		return nil, ErrBucketRequired
	}
	sort.Slice(parts, func(i, j int) bool {
		return parts[i].PartNumber < parts[j].PartNumber
	})
	completeParts := make([]minio.CompletePart, 0, len(parts))
	for _, p := range parts {
		completeParts = append(completeParts, minio.CompletePart{PartNumber: p.PartNumber, ETag: p.ETag})
	}
	core := minio.Core{Client: s.minioClient}
	_, err := core.CompleteMultipartUpload(context.Background(), s.bucket, session.StorageKey, session.UploadID, completeParts, minio.PutObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("complete multipart upload: %w", err)
	}
	stat, err := s.minioClient.StatObject(context.Background(), s.bucket, session.StorageKey, minio.StatObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("stat object: %w", err)
	}
	if stat.Size <= 0 {
		_ = s.minioClient.RemoveObject(context.Background(), s.bucket, session.StorageKey, minio.RemoveObjectOptions{})
		return nil, ErrEmptyFile
	}
	hashValue, err := s.hashObjectSHA256(session.StorageKey)
	if err != nil {
		return nil, err
	}
	if expectedSHA256 != "" && !strings.EqualFold(expectedSHA256, hashValue) {
		_ = s.minioClient.RemoveObject(context.Background(), s.bucket, session.StorageKey, minio.RemoveObjectOptions{})
		return nil, ErrChecksumMismatch
	}
	now := time.Now()
	expiresAt := now.Add(s.retention)
	sizeBytes := stat.Size
	attachment := models.Attachment{
		ID:              session.AttachmentID,
		UploaderUserID:  &userID,
		FileName:        &session.FileName,
		StorageKey:      &session.StorageKey,
		MimeType:        session.MimeType,
		SizeBytes:       &sizeBytes,
		Hash:            &hashValue,
		StorageProvider: func() *string { v := "minio"; return &v }(),
		ExpiresAt:       &expiresAt,
		CreatedAt:       now,
	}
	if err := s.db.Create(&attachment).Error; err != nil {
		return nil, fmt.Errorf("create attachment: %w", err)
	}
	s.ensureThumbnailForAttachment(userID, &attachment)
	return &attachment, nil
}

func (s *Service) hashObjectSHA256(storageKey string) (string, error) {
	if strings.TrimSpace(storageKey) == "" {
		return "", ErrStorageKeyRequired
	}
	obj, err := s.minioClient.GetObject(context.Background(), s.bucket, storageKey, minio.GetObjectOptions{})
	if err != nil {
		return "", fmt.Errorf("get object: %w", err)
	}
	defer obj.Close()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, obj); err != nil {
		return "", fmt.Errorf("hash object: %w", err)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
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
