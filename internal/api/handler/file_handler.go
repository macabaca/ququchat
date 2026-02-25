package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/minio/minio-go/v7"
	"gorm.io/gorm"

	"ququchat/internal/config"
	"ququchat/internal/models"
	filesvc "ququchat/internal/service/file"
)

type FileHandler struct {
	db  *gorm.DB
	svc *filesvc.Service
}

func attachmentResponse(attachment *models.Attachment) gin.H {
	if attachment == nil {
		return gin.H{}
	}
	return gin.H{
		"id":                  attachment.ID,
		"uploader_user_id":    attachment.UploaderUserID,
		"file_name":           attachment.FileName,
		"storage_key":         attachment.StorageKey,
		"mime_type":           attachment.MimeType,
		"size_bytes":          attachment.SizeBytes,
		"hash":                attachment.Hash,
		"storage_provider":    attachment.StorageProvider,
		"image_width":         attachment.ImageWidth,
		"image_height":        attachment.ImageHeight,
		"thumb_attachment_id": attachment.ThumbAttachmentID,
		"thumb_width":         attachment.ThumbWidth,
		"thumb_height":        attachment.ThumbHeight,
		"created_at":          attachment.CreatedAt,
	}
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
		case errors.Is(err, filesvc.ErrEmptyFile):
			c.JSON(http.StatusBadRequest, gin.H{"error": "文件为空"})
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
		"attachment": attachmentResponse(attachment),
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

func (h *FileHandler) GetThumbnailURL(c *gin.Context) {
	userID := c.GetString("user_id")
	attachmentID := c.Param("attachment_id")
	var attachment models.Attachment
	if err := h.db.Where("id = ?", attachmentID).First(&attachment).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "附件不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询附件失败"})
		return
	}
	if attachment.ThumbAttachmentID == nil || strings.TrimSpace(*attachment.ThumbAttachmentID) == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "缩略图不存在"})
		return
	}
	url, err := h.svc.PresignDownload(userID, *attachment.ThumbAttachmentID, 15*time.Minute)
	if err != nil {
		switch {
		case errors.Is(err, filesvc.ErrUserIDRequired):
			c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		case errors.Is(err, filesvc.ErrAttachmentNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "缩略图不存在"})
		case errors.Is(err, filesvc.ErrStorageKeyRequired):
			c.JSON(http.StatusBadRequest, gin.H{"error": "缩略图缺少存储信息"})
		case errors.Is(err, filesvc.ErrAttachmentExpired):
			c.JSON(http.StatusGone, gin.H{"error": "缩略图已过期"})
		case errors.Is(err, filesvc.ErrMinioClientRequired):
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "对象存储未就绪"})
		case errors.Is(err, filesvc.ErrBucketRequired):
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "对象存储配置缺失"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "生成缩略图链接失败"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"url": url})
}

func (h *FileHandler) StartMultipartUpload(c *gin.Context) {
	userID := c.GetString("user_id")
	var req struct {
		FileName string `json:"file_name"`
		MimeType string `json:"mime_type"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	session, err := h.svc.StartMultipartUpload(userID, req.FileName, req.MimeType)
	if err != nil {
		switch {
		case errors.Is(err, filesvc.ErrUserIDRequired):
			c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		case errors.Is(err, filesvc.ErrMinioClientRequired):
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "对象存储未就绪"})
		case errors.Is(err, filesvc.ErrBucketRequired):
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "对象存储配置缺失"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "初始化分片上传失败"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"upload_id":     session.UploadID,
		"attachment_id": session.AttachmentID,
		"storage_key":   session.StorageKey,
		"file_name":     session.FileName,
		"mime_type":     session.MimeType,
	})
}

func (h *FileHandler) UploadPart(c *gin.Context) {
	userID := c.GetString("user_id")
	uploadID := c.PostForm("upload_id")
	storageKey := c.PostForm("storage_key")
	partStr := c.PostForm("part_number")
	partNumber, _ := strconv.Atoi(partStr)
	file, err := c.FormFile("file")
	if err != nil {
		file = nil
	}
	if file == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少分片文件"})
		return
	}
	src, err := file.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "读取分片失败"})
		return
	}
	defer src.Close()
	part, err := h.svc.UploadPart(userID, storageKey, uploadID, partNumber, src, file.Size)
	if err != nil {
		switch {
		case errors.Is(err, filesvc.ErrUserIDRequired):
			c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		case errors.Is(err, filesvc.ErrUploadIDRequired):
			c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 upload_id"})
		case errors.Is(err, filesvc.ErrStorageKeyRequired):
			c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 storage_key"})
		case errors.Is(err, filesvc.ErrPartNumberInvalid):
			c.JSON(http.StatusBadRequest, gin.H{"error": "无效的分片号"})
		case errors.Is(err, filesvc.ErrFileTooLarge):
			c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "分片过大"})
		case errors.Is(err, filesvc.ErrMinioClientRequired):
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "对象存储未就绪"})
		case errors.Is(err, filesvc.ErrBucketRequired):
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "对象存储配置缺失"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "上传分片失败"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"part_number": part.PartNumber, "etag": part.ETag, "size": part.Size})
}

func (h *FileHandler) ListUploadedParts(c *gin.Context) {
	userID := c.GetString("user_id")
	uploadID := c.Query("upload_id")
	storageKey := c.Query("storage_key")
	parts, err := h.svc.ListUploadedParts(userID, storageKey, uploadID)
	if err != nil {
		switch {
		case errors.Is(err, filesvc.ErrUserIDRequired):
			c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		case errors.Is(err, filesvc.ErrUploadIDRequired):
			c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 upload_id"})
		case errors.Is(err, filesvc.ErrStorageKeyRequired):
			c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 storage_key"})
		case errors.Is(err, filesvc.ErrMinioClientRequired):
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "对象存储未就绪"})
		case errors.Is(err, filesvc.ErrBucketRequired):
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "对象存储配置缺失"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "获取分片列表失败"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"parts": parts})
}

func (h *FileHandler) CompleteMultipartUpload(c *gin.Context) {
	userID := c.GetString("user_id")
	var req struct {
		UploadID       string `json:"upload_id"`
		StorageKey     string `json:"storage_key"`
		AttachmentID   string `json:"attachment_id"`
		FileName       string `json:"file_name"`
		MimeType       string `json:"mime_type"`
		ExpectedSHA256 string `json:"expected_sha256"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	parts, err := h.svc.ListUploadedParts(userID, req.StorageKey, req.UploadID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "获取分片列表失败"})
		return
	}
	session := filesvc.MultipartSession{
		UploadID:     req.UploadID,
		AttachmentID: req.AttachmentID,
		StorageKey:   req.StorageKey,
		FileName:     req.FileName,
	}
	if strings.TrimSpace(req.MimeType) != "" {
		mt := strings.TrimSpace(req.MimeType)
		session.MimeType = &mt
	}
	attachment, err := h.svc.CompleteMultipartUpload(userID, session, parts, req.ExpectedSHA256)
	if err != nil {
		switch {
		case errors.Is(err, filesvc.ErrUserIDRequired):
			c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		case errors.Is(err, filesvc.ErrUploadIDRequired):
			c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 upload_id"})
		case errors.Is(err, filesvc.ErrStorageKeyRequired):
			c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 storage_key"})
		case errors.Is(err, filesvc.ErrEmptyFile):
			c.JSON(http.StatusBadRequest, gin.H{"error": "文件为空"})
		case errors.Is(err, filesvc.ErrChecksumMismatch):
			c.JSON(http.StatusBadRequest, gin.H{"error": "校验失败"})
		case errors.Is(err, filesvc.ErrMinioClientRequired):
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "对象存储未就绪"})
		case errors.Is(err, filesvc.ErrBucketRequired):
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "对象存储配置缺失"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "完成上传失败"})
		}
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"attachment": attachmentResponse(attachment),
	})
}

func (h *FileHandler) AbortMultipartUpload(c *gin.Context) {
	userID := c.GetString("user_id")
	var req struct {
		UploadID   string `json:"upload_id"`
		StorageKey string `json:"storage_key"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误"})
		return
	}
	if err := h.svc.AbortMultipartUpload(userID, req.StorageKey, req.UploadID); err != nil {
		switch {
		case errors.Is(err, filesvc.ErrUserIDRequired):
			c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		case errors.Is(err, filesvc.ErrUploadIDRequired):
			c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 upload_id"})
		case errors.Is(err, filesvc.ErrStorageKeyRequired):
			c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 storage_key"})
		case errors.Is(err, filesvc.ErrMinioClientRequired):
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "对象存储未就绪"})
		case errors.Is(err, filesvc.ErrBucketRequired):
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "对象存储配置缺失"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "取消上传失败"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "已取消"})
}
