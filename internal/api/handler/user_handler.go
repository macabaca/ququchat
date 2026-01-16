package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"ququchat/internal/models"
)

type UserHandler struct {
	db *gorm.DB
}

func NewUserHandler(db *gorm.DB) *UserHandler {
	return &UserHandler{db: db}
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
				"id":        target.ID,
				"user_code": target.UserCode,
				"username":  target.Username,
				"status":    target.Status,
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
				"id":            existingReq.ID,
				"from_user_id":  existingReq.FromUserID,
				"to_user_id":    existingReq.ToUserID,
				"status":        existingReq.Status,
				"message":       existingReq.Message,
				"created_at":    existingReq.CreatedAt,
				"responded_at":  existingReq.RespondedAt,
				"target_id":     target.ID,
				"target_name":   target.Username,
				"target_status": target.Status,
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

	userMap := make(map[string]models.User, len(users))
	for _, u := range users {
		userMap[u.ID] = u
	}

	resp := make([]gin.H, 0, len(friendIDs))
	for _, id := range friendIDs {
		if u, ok := userMap[id]; ok {
			resp = append(resp, gin.H{
				"id":        u.ID,
				"user_code": u.UserCode,
				"username":  u.Username,
				"status":    u.Status,
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
				"request_id":     r.ID,
				"from_user_id":   r.FromUserID,
				"from_user_code": u.UserCode,
				"from_username":  u.Username,
				"message":        r.Message,
				"status":         r.Status,
				"created_at":     r.CreatedAt,
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
