package handler

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"ququchat/internal/models"
)

type MessageHandler struct {
	db           *gorm.DB
	historyLimit int
}

func NewMessageHandler(db *gorm.DB, historyLimit int) *MessageHandler {
	if historyLimit <= 0 {
		historyLimit = 50
	}
	return &MessageHandler{
		db:           db,
		historyLimit: historyLimit,
	}
}

type HistoryRequest struct {
	MessageID string `form:"message_id" json:"message_id" binding:"required"`
}

type MessageDTO struct {
	ID          string `json:"id"`
	RoomID      string `json:"room_id"`
	SenderID    string `json:"sender_id,omitempty"`
	ContentType string `json:"content_type"`
	ContentText string `json:"content_text,omitempty"`
	CreatedAt   int64  `json:"created_at"`
}

func (h *MessageHandler) GetHistoryBefore(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	var req HistoryRequest
	if err := c.ShouldBind(&req); err != nil || req.MessageID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少消息ID"})
		return
	}
	var target models.Message
	if err := h.db.Where("id = ?", req.MessageID).First(&target).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "消息不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询消息失败"})
		return
	}
	if target.RoomID == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "消息数据异常"})
		return
	}
	var room models.Room
	if err := h.db.Where("id = ?", target.RoomID).First(&room).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询房间失败"})
		return
	}
	if room.RoomType == models.RoomTypeDirect {
		var member models.RoomMember
		if err := h.db.Where("room_id = ? AND user_id = ?", room.ID, userID).First(&member).Error; err != nil {
			if err != gorm.ErrRecordNotFound {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "查询房间成员失败"})
				return
			}
			parts := strings.Split(room.Name, ":")
			if len(parts) != 2 {
				c.JSON(http.StatusForbidden, gin.H{"error": "无权查看该消息历史"})
				return
			}
			if userID != parts[0] && userID != parts[1] {
				c.JSON(http.StatusForbidden, gin.H{"error": "无权查看该消息历史"})
				return
			}
			now := time.Now()
			for _, uid := range parts {
				if uid == "" {
					continue
				}
				var rm models.RoomMember
				if err := h.db.Where("room_id = ? AND user_id = ?", room.ID, uid).First(&rm).Error; err != nil {
					if err == gorm.ErrRecordNotFound {
						_ = h.db.Create(&models.RoomMember{
							RoomID:   room.ID,
							UserID:   uid,
							Role:     models.MemberRoleMember,
							JoinedAt: now,
						}).Error
					}
				}
			}
		}
	}
	var list []models.Message
	if err := h.db.
		Where("room_id = ? AND created_at < ?", target.RoomID, target.CreatedAt).
		Order("created_at desc").
		Limit(h.historyLimit).
		Find(&list).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询消息历史失败"})
		return
	}
	if len(list) == 0 {
		c.JSON(http.StatusOK, gin.H{"messages": []MessageDTO{}})
		return
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].CreatedAt.Before(list[j].CreatedAt)
	})
	result := make([]MessageDTO, 0, len(list))
	for _, m := range list {
		dto := MessageDTO{
			ID:          m.ID,
			RoomID:      m.RoomID,
			ContentType: string(m.ContentType),
			CreatedAt:   m.CreatedAt.Unix(),
		}
		if m.SenderID != nil {
			dto.SenderID = *m.SenderID
		}
		if m.ContentText != nil {
			dto.ContentText = *m.ContentText
		}
		result = append(result, dto)
	}
	c.JSON(http.StatusOK, gin.H{"messages": result})
}

type LatestRequest struct {
	FriendID string `form:"friend_id" json:"friend_id" binding:"required"`
}

func (h *MessageHandler) GetLatestByFriend(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	var req LatestRequest
	if err := c.ShouldBind(&req); err != nil || req.FriendID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少好友ID"})
		return
	}
	if userID == req.FriendID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "好友ID无效"})
		return
	}
	a, b := userID, req.FriendID
	if a > b {
		a, b = b, a
	}
	var fs models.Friendship
	if err := h.db.Where("user_id_a = ? AND user_id_b = ?", a, b).First(&fs).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusForbidden, gin.H{"error": "双方不是好友"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询好友关系失败"})
		return
	}
	x, y := userID, req.FriendID
	if x > y {
		x, y = y, x
	}
	name := x + ":" + y
	var room models.Room
	if err := h.db.Where("room_type = ? AND name = ?", models.RoomTypeDirect, name).First(&room).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusOK, gin.H{"messages": []MessageDTO{}})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询房间失败"})
		return
	}
	var list []models.Message
	if err := h.db.
		Where("room_id = ?", room.ID).
		Order("created_at desc").
		Limit(h.historyLimit).
		Find(&list).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询消息历史失败"})
		return
	}
	if len(list) == 0 {
		c.JSON(http.StatusOK, gin.H{"messages": []MessageDTO{}})
		return
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].CreatedAt.Before(list[j].CreatedAt)
	})
	result := make([]MessageDTO, 0, len(list))
	for _, m := range list {
		dto := MessageDTO{
			ID:          m.ID,
			RoomID:      m.RoomID,
			ContentType: string(m.ContentType),
			CreatedAt:   m.CreatedAt.Unix(),
		}
		if m.SenderID != nil {
			dto.SenderID = *m.SenderID
		}
		if m.ContentText != nil {
			dto.ContentText = *m.ContentText
		}
		result = append(result, dto)
	}
	c.JSON(http.StatusOK, gin.H{"messages": result})
}
