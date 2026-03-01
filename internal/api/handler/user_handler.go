package handler

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"ququchat/internal/config"
	"ququchat/internal/models"
	serverstorage "ququchat/internal/server/storage"
	filesvc "ququchat/internal/service/file"
)

type UserHandler struct {
	db        *gorm.DB
	fileSvc   *filesvc.Service
	avatarCfg config.Avatar
}

func NewUserHandler(db *gorm.DB, cfg config.File, avatarCfg config.Avatar, objStorage serverstorage.ObjectStorage, bucket string) *UserHandler {
	thumb := filesvc.ThumbnailOptions{
		MaxDimension:   cfg.Thumbnail.MaxDimensionOrDefault(),
		JPEGQuality:    cfg.Thumbnail.JPEGQualityOrDefault(),
		RetryCount:     cfg.Thumbnail.RetryCountOrDefault(),
		RetryDelay:     cfg.Thumbnail.RetryDelayDuration(),
		MaxSourceBytes: cfg.Thumbnail.MaxSourceBytesOrDefault(),
	}
	return &UserHandler{
		db:        db,
		fileSvc:   filesvc.NewService(db, objStorage, bucket, cfg.MaxSizeBytes, cfg.RetentionDuration(), thumb),
		avatarCfg: avatarCfg,
	}
}

type AddFriendRequest struct {
	TargetUserCode int64   `json:"target_user_code"`
	Message        *string `json:"message,omitempty"`
}

type RemoveFriendRequest struct {
	TargetUserCode int64 `json:"target_user_code"`
}

type RespondFriendRequest struct {
	RequestID string `json:"request_id"`
	Action    string `json:"action"`
}

func userAttachmentResponse(attachment *models.Attachment) gin.H {
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

func (h *UserHandler) UploadAvatar(c *gin.Context) {
	currentUserID := c.GetString("user_id")
	if currentUserID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	file, err := c.FormFile("file")
	if err != nil {
		file = nil
	}

	attachment, err := h.fileSvc.UploadAvatar(
		currentUserID,
		file,
		h.avatarCfg.MaxSizeOrDefault(),
		h.avatarCfg.PermanentOrDefault(),
		h.avatarCfg.RetentionDuration(),
	)
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
		case errors.Is(err, filesvc.ErrImageOnly):
			c.JSON(http.StatusBadRequest, gin.H{"error": "只允许上传图片"})
		case errors.Is(err, filesvc.ErrMinioClientRequired):
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "对象存储未就绪"})
		case errors.Is(err, filesvc.ErrBucketRequired):
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "对象存储配置缺失"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "上传头像失败"})
		}
		return
	}

	var oldKeys []string
	if err := h.db.Transaction(func(tx *gorm.DB) error {
		var u models.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", currentUserID).First(&u).Error; err != nil {
			return err
		}

		oldAvatarID := ""
		if u.AvatarAttachmentID != nil {
			oldAvatarID = strings.TrimSpace(*u.AvatarAttachmentID)
		}

		if err := tx.Model(&models.User{}).Where("id = ?", currentUserID).Update("avatar_attachment_id", attachment.ID).Error; err != nil {
			return err
		}

		if oldAvatarID != "" && oldAvatarID != attachment.ID {
			keys, err := h.fileSvc.DeleteAttachmentRecordsTx(tx, currentUserID, oldAvatarID)
			if err != nil {
				if errors.Is(err, filesvc.ErrAttachmentNotFound) {
					return nil
				}
				return err
			}
			oldKeys = append(oldKeys, keys...)
		}
		return nil
	}); err != nil {
		_ = h.fileSvc.DeleteAttachment(currentUserID, attachment.ID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "更新头像失败"})
		return
	}

	for _, k := range oldKeys {
		_ = h.fileSvc.RemoveObjectByKey(k)
	}

	c.JSON(http.StatusCreated, gin.H{
		"avatar_attachment_id": attachment.ID,
		"attachment":           userAttachmentResponse(attachment),
	})
}

func (h *UserHandler) GetAvatarURL(c *gin.Context) {
	currentUserID := c.GetString("user_id")
	if currentUserID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	targetUserID := strings.TrimSpace(c.Param("user_id"))
	if targetUserID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 user_id"})
		return
	}

	var target models.User
	if err := h.db.Where("id = ?", targetUserID).First(&target).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询用户失败"})
		return
	}

	if target.AvatarAttachmentID == nil || strings.TrimSpace(*target.AvatarAttachmentID) == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户未设置头像"})
		return
	}

	url, err := h.fileSvc.PresignDownload(currentUserID, strings.TrimSpace(*target.AvatarAttachmentID), 15*time.Minute)
	if err != nil {
		switch {
		case errors.Is(err, filesvc.ErrUserIDRequired):
			c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		case errors.Is(err, filesvc.ErrAttachmentNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "头像不存在"})
		case errors.Is(err, filesvc.ErrStorageKeyRequired):
			c.JSON(http.StatusBadRequest, gin.H{"error": "头像缺少存储信息"})
		case errors.Is(err, filesvc.ErrAttachmentExpired):
			c.JSON(http.StatusGone, gin.H{"error": "头像已过期"})
		case errors.Is(err, filesvc.ErrMinioClientRequired):
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "对象存储未就绪"})
		case errors.Is(err, filesvc.ErrBucketRequired):
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "对象存储配置缺失"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "生成头像链接失败"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"url": url})
}

func (h *UserHandler) GetAvatarThumbURL(c *gin.Context) {
	currentUserID := c.GetString("user_id")
	if currentUserID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	targetUserID := strings.TrimSpace(c.Param("user_id"))
	if targetUserID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少 user_id"})
		return
	}

	var target models.User
	if err := h.db.Where("id = ?", targetUserID).First(&target).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询用户失败"})
		return
	}

	if target.AvatarAttachmentID == nil || strings.TrimSpace(*target.AvatarAttachmentID) == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "用户未设置头像"})
		return
	}

	avatarID := strings.TrimSpace(*target.AvatarAttachmentID)
	var attachment models.Attachment
	if err := h.db.Where("id = ?", avatarID).First(&attachment).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "头像不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询头像失败"})
		return
	}

	downloadID := avatarID
	if attachment.ThumbAttachmentID != nil && strings.TrimSpace(*attachment.ThumbAttachmentID) != "" {
		downloadID = strings.TrimSpace(*attachment.ThumbAttachmentID)
	}

	url, err := h.fileSvc.PresignDownload(currentUserID, downloadID, 15*time.Minute)
	if err != nil {
		switch {
		case errors.Is(err, filesvc.ErrUserIDRequired):
			c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		case errors.Is(err, filesvc.ErrAttachmentNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "头像不存在"})
		case errors.Is(err, filesvc.ErrStorageKeyRequired):
			c.JSON(http.StatusBadRequest, gin.H{"error": "头像缺少存储信息"})
		case errors.Is(err, filesvc.ErrAttachmentExpired):
			c.JSON(http.StatusGone, gin.H{"error": "头像已过期"})
		case errors.Is(err, filesvc.ErrMinioClientRequired):
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "对象存储未就绪"})
		case errors.Is(err, filesvc.ErrBucketRequired):
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "对象存储配置缺失"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "生成头像链接失败"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"url": url})
}

func (h *UserHandler) AddFriend(c *gin.Context) {
	currentUserID := c.GetString("user_id")
	if currentUserID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	var req AddFriendRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.TargetUserCode <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误或目标用户无效"})
		return
	}

	var target models.User
	if err := h.db.Where("user_code = ?", req.TargetUserCode).First(&target).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询用户失败"})
		return
	}

	if target.ID == currentUserID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "不能添加自己为好友"})
		return
	}

	a, b := currentUserID, target.ID
	if a > b {
		a, b = b, a
	}

	var existingFriend models.Friendship
	if err := h.db.Where("user_id_a = ? AND user_id_b = ?", a, b).First(&existingFriend).Error; err == nil {
		c.JSON(http.StatusOK, gin.H{
			"message": "已是好友",
			"friend": gin.H{
				"id":                   target.ID,
				"user_code":            target.UserCode,
				"username":             target.Username,
				"status":               target.Status,
				"avatar_attachment_id": target.AvatarAttachmentID,
			},
		})
		return
	} else if err != gorm.ErrRecordNotFound {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询好友关系失败"})
		return
	}

	var existingReq models.FriendRequest
	if err := h.db.Where(
		"(from_user_id = ? AND to_user_id = ? OR from_user_id = ? AND to_user_id = ?) AND status = ?",
		currentUserID, target.ID, target.ID, currentUserID, models.FriendRequestPending,
	).First(&existingReq).Error; err == nil {
		c.JSON(http.StatusOK, gin.H{
			"message": "已存在待处理的好友请求",
			"request": gin.H{
				"id":                          existingReq.ID,
				"from_user_id":                existingReq.FromUserID,
				"to_user_id":                  existingReq.ToUserID,
				"status":                      existingReq.Status,
				"message":                     existingReq.Message,
				"created_at":                  existingReq.CreatedAt,
				"responded_at":                existingReq.RespondedAt,
				"target_id":                   target.ID,
				"target_name":                 target.Username,
				"target_status":               target.Status,
				"target_avatar_attachment_id": target.AvatarAttachmentID,
			},
		})
		return
	} else if err != gorm.ErrRecordNotFound {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询已有好友请求失败"})
		return
	}

	now := time.Now()
	fr := models.FriendRequest{
		ID:         uuid.NewString(),
		FromUserID: currentUserID,
		ToUserID:   target.ID,
		Status:     models.FriendRequestPending,
		CreatedAt:  now,
	}
	if req.Message != nil {
		fr.Message = req.Message
	}
	if err := h.db.Create(&fr).Error; err != nil {
		var dup models.FriendRequest
		if err2 := h.db.Where("from_user_id = ? AND to_user_id = ? AND status = ?", currentUserID, target.ID, models.FriendRequestPending).First(&dup).Error; err2 == nil {
			c.JSON(http.StatusOK, gin.H{
				"message": "已存在待处理的好友请求",
				"request": gin.H{
					"id":           dup.ID,
					"from_user_id": dup.FromUserID,
					"to_user_id":   dup.ToUserID,
					"status":       dup.Status,
					"message":      dup.Message,
					"created_at":   dup.CreatedAt,
					"responded_at": dup.RespondedAt,
				},
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建好友请求失败"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "好友请求已发送",
		"request": gin.H{
			"id":            fr.ID,
			"from_user_id":  fr.FromUserID,
			"to_user_id":    fr.ToUserID,
			"status":        fr.Status,
			"message":       fr.Message,
			"created_at":    fr.CreatedAt,
			"responded_at":  fr.RespondedAt,
			"target_id":     target.ID,
			"target_name":   target.Username,
			"target_status": target.Status,
		},
	})
}

func (h *UserHandler) RemoveFriend(c *gin.Context) {
	currentUserID := c.GetString("user_id")
	if currentUserID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	var req RemoveFriendRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.TargetUserCode <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误或目标用户无效"})
		return
	}

	var target models.User
	if err := h.db.Where("user_code = ?", req.TargetUserCode).First(&target).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询用户失败"})
		return
	}

	a, b := currentUserID, target.ID
	if a > b {
		a, b = b, a
	}

	tx := h.db.Where("user_id_a = ? AND user_id_b = ?", a, b).Delete(&models.Friendship{})
	if tx.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "删除好友关系失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "已删除好友"})
}

func (h *UserHandler) ListFriends(c *gin.Context) {
	currentUserID := c.GetString("user_id")
	if currentUserID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	var relations []models.Friendship
	if err := h.db.Where("user_id_a = ? OR user_id_b = ?", currentUserID, currentUserID).Find(&relations).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询好友关系失败"})
		return
	}

	if len(relations) == 0 {
		c.JSON(http.StatusOK, gin.H{"friends": []interface{}{}})
		return
	}

	friendIDs := make([]string, 0, len(relations))
	for _, r := range relations {
		if r.UserIDA == currentUserID {
			friendIDs = append(friendIDs, r.UserIDB)
		} else {
			friendIDs = append(friendIDs, r.UserIDA)
		}
	}

	var users []models.User
	if err := h.db.Where("id IN ?", friendIDs).Find(&users).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询好友信息失败"})
		return
	}

	// Batch find room IDs for these friends
	roomNames := make([]string, 0, len(friendIDs))
	for _, fid := range friendIDs {
		a, b := currentUserID, fid
		if a > b {
			a, b = b, a
		}
		roomNames = append(roomNames, a+":"+b)
	}

	var rooms []models.Room
	if len(roomNames) > 0 {
		h.db.Where("room_type = ? AND name IN ?", models.RoomTypeDirect, roomNames).Find(&rooms)
	}

	roomMap := make(map[string]string) // friendID -> roomID
	for _, r := range rooms {
		// Name is "id1:id2"
		parts := strings.Split(r.Name, ":")
		if len(parts) == 2 {
			var fid string
			if parts[0] == currentUserID {
				fid = parts[1]
			} else {
				fid = parts[0]
			}
			roomMap[fid] = r.ID
		}
	}

	userMap := make(map[string]models.User, len(users))
	for _, u := range users {
		userMap[u.ID] = u
	}

	resp := make([]gin.H, 0, len(friendIDs))
	for _, id := range friendIDs {
		if u, ok := userMap[id]; ok {
			resp = append(resp, gin.H{
				"id":                   u.ID,
				"user_code":            u.UserCode,
				"username":             u.Username,
				"status":               u.Status,
				"avatar_attachment_id": u.AvatarAttachmentID,
				"room_id":              roomMap[id],
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{"friends": resp})
}

func (h *UserHandler) ListIncomingFriendRequests(c *gin.Context) {
	currentUserID := c.GetString("user_id")
	if currentUserID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	var requests []models.FriendRequest
	if err := h.db.Where("to_user_id = ? AND status = ?", currentUserID, models.FriendRequestPending).
		Order("created_at DESC").
		Find(&requests).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询好友请求失败"})
		return
	}

	if len(requests) == 0 {
		c.JSON(http.StatusOK, gin.H{"requests": []interface{}{}})
		return
	}

	fromIDs := make([]string, 0, len(requests))
	for _, r := range requests {
		fromIDs = append(fromIDs, r.FromUserID)
	}

	var users []models.User
	if err := h.db.Where("id IN ?", fromIDs).Find(&users).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询请求发起人信息失败"})
		return
	}

	userMap := make(map[string]models.User, len(users))
	for _, u := range users {
		userMap[u.ID] = u
	}

	resp := make([]gin.H, 0, len(requests))
	for _, r := range requests {
		if u, ok := userMap[r.FromUserID]; ok {
			resp = append(resp, gin.H{
				"request_id":                r.ID,
				"from_user_id":              r.FromUserID,
				"from_user_code":            u.UserCode,
				"from_username":             u.Username,
				"from_avatar_attachment_id": u.AvatarAttachmentID,
				"message":                   r.Message,
				"status":                    r.Status,
				"created_at":                r.CreatedAt,
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{"requests": resp})
}

func (h *UserHandler) RespondFriendRequest(c *gin.Context) {
	currentUserID := c.GetString("user_id")
	if currentUserID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	var req RespondFriendRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.RequestID == "" || (req.Action != "accept" && req.Action != "reject") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误或操作无效"})
		return
	}

	var fr models.FriendRequest
	if err := h.db.Where("id = ?", req.RequestID).First(&fr).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "好友请求不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询好友请求失败"})
		return
	}

	if fr.ToUserID != currentUserID {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权处理该好友请求"})
		return
	}

	if fr.Status != models.FriendRequestPending {
		c.JSON(http.StatusBadRequest, gin.H{"error": "该好友请求已被处理"})
		return
	}

	now := time.Now()
	if req.Action == "reject" {
		if err := h.db.Model(&fr).Updates(map[string]interface{}{
			"status":       models.FriendRequestRejected,
			"responded_at": &now,
		}).Error; err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "更新好友请求状态失败"})
			return
		}
		fr.Status = models.FriendRequestRejected
		fr.RespondedAt = &now
		c.JSON(http.StatusOK, gin.H{
			"message": "已拒绝好友请求",
			"request": gin.H{
				"id":           fr.ID,
				"from_user_id": fr.FromUserID,
				"to_user_id":   fr.ToUserID,
				"status":       fr.Status,
				"message":      fr.Message,
				"created_at":   fr.CreatedAt,
				"responded_at": fr.RespondedAt,
			},
		})
		return
	}

	if err := h.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&fr).Updates(map[string]interface{}{
			"status":       models.FriendRequestAccepted,
			"responded_at": &now,
		}).Error; err != nil {
			return err
		}

		a, b := fr.FromUserID, fr.ToUserID
		if a > b {
			a, b = b, a
		}

		var existing models.Friendship
		if err := tx.Where("user_id_a = ? AND user_id_b = ?", a, b).First(&existing).Error; err != nil {
			if err != gorm.ErrRecordNotFound {
				return err
			}
			f := models.Friendship{
				ID:        uuid.NewString(),
				UserIDA:   a,
				UserIDB:   b,
				CreatedAt: now,
			}
			if err := tx.Create(&f).Error; err != nil {
				return err
			}
		}

		// Eagerly create Direct Room and Members
		name := a + ":" + b
		var room models.Room
		if err := tx.Where("room_type = ? AND name = ?", models.RoomTypeDirect, name).First(&room).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				room = models.Room{
					ID:          uuid.NewString(),
					RoomType:    models.RoomTypeDirect,
					Name:        name,
					OwnerUserID: a,
					CreatedAt:   now,
					UpdatedAt:   now,
				}
				if err := tx.Create(&room).Error; err != nil {
					return err
				}
			} else {
				return err
			}
		}

		// Ensure Members exist
		for _, uid := range []string{a, b} {
			var m models.RoomMember
			if err := tx.Where("room_id = ? AND user_id = ?", room.ID, uid).First(&m).Error; err != nil {
				if err == gorm.ErrRecordNotFound {
					if err := tx.Create(&models.RoomMember{
						RoomID:   room.ID,
						UserID:   uid,
						Role:     models.MemberRoleMember,
						JoinedAt: now,
					}).Error; err != nil {
						return err
					}
				} else {
					return err
				}
			}
		}

		return nil
	}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "接受好友请求失败"})
		return
	}

	fr.Status = models.FriendRequestAccepted
	fr.RespondedAt = &now

	c.JSON(http.StatusOK, gin.H{
		"message": "已接受好友请求",
		"request": gin.H{
			"id":           fr.ID,
			"from_user_id": fr.FromUserID,
			"to_user_id":   fr.ToUserID,
			"status":       fr.Status,
			"message":      fr.Message,
			"created_at":   fr.CreatedAt,
			"responded_at": fr.RespondedAt,
		},
	})
}
