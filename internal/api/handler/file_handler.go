package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/minio/minio-go/v7"
	"gorm.io/gorm"

	"ququchat/internal/config"
	filesvc "ququchat/internal/service/file"
)

type FileHandler struct {
	db  *gorm.DB
	svc *filesvc.Service
}

func NewFileHandler(db *gorm.DB, cfg config.File, minioClient *minio.Client, bucket string) *FileHandler {
	return &FileHandler{
		db:  db,
		svc: filesvc.NewService(db, minioClient, bucket, cfg.MaxSizeBytes, cfg.RetentionDuration()),
	}
}

func (h *FileHandler) Upload(c *gin.Context) {
	userID := c.GetString("user_id")
	file, err := c.FormFile("file")
	if err != nil {
		file = nil
	}

	attachment, err := h.svc.Upload(userID, file)
	if err != nil {
		switch {
		case errors.Is(err, filesvc.ErrUserIDRequired):
			c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		case errors.Is(err, filesvc.ErrFileRequired):
			c.JSON(http.StatusBadRequest, gin.H{"error": "缺少文件"})
		case errors.Is(err, filesvc.ErrFileTooLarge):
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "文件过大"})
		case errors.Is(err, filesvc.ErrMinioClientRequired):
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "对象存储未就绪"})
		case errors.Is(err, filesvc.ErrBucketRequired):
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "对象存储配置缺失"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "上传失败"})
		}
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"attachment": gin.H{
			"id":               attachment.ID,
			"uploader_user_id": attachment.UploaderUserID,
			"file_name":        attachment.FileName,
			"storage_key":      attachment.StorageKey,
			"mime_type":        attachment.MimeType,
			"size_bytes":       attachment.SizeBytes,
			"hash":             attachment.Hash,
			"storage_provider": attachment.StorageProvider,
			"created_at":       attachment.CreatedAt,
		},
	})
}

func (h *FileHandler) GetDownloadURL(c *gin.Context) {
	userID := c.GetString("user_id")
	attachmentID := c.Param("attachment_id")
	url, err := h.svc.PresignDownload(userID, attachmentID, 15*time.Minute)
	if err != nil {
		switch {
		case errors.Is(err, filesvc.ErrUserIDRequired):
			c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		case errors.Is(err, filesvc.ErrAttachmentNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "附件不存在"})
		case errors.Is(err, filesvc.ErrStorageKeyRequired):
			c.JSON(http.StatusBadRequest, gin.H{"error": "附件缺少存储信息"})
		case errors.Is(err, filesvc.ErrAttachmentExpired):
			c.JSON(http.StatusGone, gin.H{"error": "附件已过期"})
		case errors.Is(err, filesvc.ErrMinioClientRequired):
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "对象存储未就绪"})
		case errors.Is(err, filesvc.ErrBucketRequired):
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "对象存储配置缺失"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "生成临时链接失败"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"url": url})
}
