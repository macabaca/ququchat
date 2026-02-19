package handler

import (
	"encoding/json"
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
	RoomID    string `form:"room_id" json:"room_id" binding:"required"`
}

type MessageDTO struct {
	ID           string          `json:"id"`
	RoomID       string          `json:"room_id"`
	SequenceID   int64           `json:"sequence_id"`
	SenderID     string          `json:"sender_id,omitempty"`
	ContentType  string          `json:"content_type"`
	ContentText  string          `json:"content_text,omitempty"`
	AttachmentID string          `json:"attachment_id,omitempty"`
	PayloadJSON  json.RawMessage `json:"payload_json,omitempty"`
	CreatedAt    int64           `json:"created_at"`
}

func (h *MessageHandler) GetHistoryBefore(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	var req HistoryRequest
	if err := c.ShouldBind(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误: 缺少 message_id 或 room_id"})
		return
	}

	// 1. 先加载房间，进行权限检查 (Context-Aware)
	var room models.Room
	if err := h.db.Unscoped().Where("id = ?", req.RoomID).First(&room).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "房间不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询房间失败"})
		return
	}

	// 2. 权限校验
	var memberLeftAt *time.Time
	if room.RoomType == models.RoomTypeDirect {
		parts := strings.Split(room.Name, ":")
		if len(parts) != 2 {
			c.JSON(http.StatusForbidden, gin.H{"error": "无效的私聊房间"})
			return
		}
		if userID != parts[0] && userID != parts[1] {
			c.JSON(http.StatusForbidden, gin.H{"error": "无权查看该私聊历史"})
			return
		}
		// 私聊也检查一下成员表，确保数据一致性（可选，但推荐）
		// 如果是第一次发消息，可能还没写成员表，这里可以补全逻辑，或者简化为只信 Name
		// 这里保持原逻辑：尝试查找或创建成员记录
		var member models.RoomMember
		if err := h.db.Where("room_id = ? AND user_id = ?", room.ID, userID).First(&member).Error; err != nil {
			if err != gorm.ErrRecordNotFound {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "查询房间成员失败"})
				return
			}
			// 如果没找到成员记录，但名字匹配，说明是新私聊，补全成员
			now := time.Now()
			// 补全双方
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
	} else if room.RoomType == models.RoomTypeGroup {
		var member models.RoomMember
		if err := h.db.Where("room_id = ? AND user_id = ?", room.ID, userID).First(&member).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				c.JSON(http.StatusForbidden, gin.H{"error": "您不是该群成员"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "查询成员关系失败"})
			return
		}
		memberLeftAt = member.LeftAt
	}

	// 3. 加载参照消息 (Cursor)
	var target models.Message
	if err := h.db.Where("id = ?", req.MessageID).First(&target).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "参照消息不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询消息失败"})
		return
	}

	// 4. 安全检查：确保参照消息属于该房间
	if target.RoomID != req.RoomID {
		c.JSON(http.StatusBadRequest, gin.H{"error": "消息不属于该房间"})
		return
	}

	// 5. 执行查询
	// 优化：改用 sequence_id 进行范围查询，避免时间戳重复或回拨导致的问题
	query := h.db.Where("room_id = ? AND sequence_id < ?", room.ID, target.SequenceID)
	if memberLeftAt != nil {
		query = query.Where("created_at < ?", memberLeftAt)
	}

	var list []models.Message
	if err := query.
		Order("sequence_id desc"). // 使用 sequence_id 倒序
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
			SequenceID:  m.SequenceID,
			ContentType: string(m.ContentType),
			CreatedAt:   m.CreatedAt.Unix(),
		}
		if m.SenderID != nil {
			dto.SenderID = *m.SenderID
		}
		if m.ContentText != nil {
			dto.ContentText = *m.ContentText
		}
		if m.AttachmentID != nil {
			dto.AttachmentID = *m.AttachmentID
		}
		if len(m.PayloadJSON) > 0 {
			dto.PayloadJSON = json.RawMessage(m.PayloadJSON)
		}
		result = append(result, dto)
	}
	c.JSON(http.StatusOK, gin.H{"messages": result})
}

type SyncHistoryRequest struct {
	RoomID          string `form:"room_id" json:"room_id" binding:"required"`
	AfterSequenceID int64  `form:"after_sequence_id" json:"after_sequence_id"`
}

// GetHistoryAfter 获取指定 sequence_id 之后的消息（用于增量同步）
func (h *MessageHandler) GetHistoryAfter(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	var req SyncHistoryRequest
	if err := c.ShouldBind(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "参数错误: 缺少 room_id"})
		return
	}

	// 1. 加载房间，进行权限检查
	var room models.Room
	if err := h.db.Unscoped().Where("id = ?", req.RoomID).First(&room).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "房间不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询房间失败"})
		return
	}

	// 2. 权限校验
	var memberLeftAt *time.Time
	if room.RoomType == models.RoomTypeDirect {
		parts := strings.Split(room.Name, ":")
		if len(parts) != 2 {
			c.JSON(http.StatusForbidden, gin.H{"error": "无效的私聊房间"})
			return
		}
		if userID != parts[0] && userID != parts[1] {
			c.JSON(http.StatusForbidden, gin.H{"error": "无权查看该私聊历史"})
			return
		}
	} else if room.RoomType == models.RoomTypeGroup {
		var member models.RoomMember
		if err := h.db.Where("room_id = ? AND user_id = ?", room.ID, userID).First(&member).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				c.JSON(http.StatusForbidden, gin.H{"error": "您不是该群成员"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "查询成员关系失败"})
			return
		}
		memberLeftAt = member.LeftAt
	}

	// 3. 执行查询
	query := h.db.Where("room_id = ? AND sequence_id > ?", room.ID, req.AfterSequenceID)
	if memberLeftAt != nil {
		query = query.Where("created_at < ?", memberLeftAt)
	}

	var list []models.Message
	if err := query.
		Order("sequence_id asc"). // 增量同步按 sequence_id 正序
		Limit(h.historyLimit).
		Find(&list).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询消息历史失败"})
		return
	}

	if len(list) == 0 {
		c.JSON(http.StatusOK, gin.H{"messages": []MessageDTO{}})
		return
	}

	result := make([]MessageDTO, 0, len(list))
	for _, m := range list {
		dto := MessageDTO{
			ID:          m.ID,
			RoomID:      m.RoomID,
			SequenceID:  m.SequenceID,
			ContentType: string(m.ContentType),
			CreatedAt:   m.CreatedAt.Unix(),
		}
		if m.SenderID != nil {
			dto.SenderID = *m.SenderID
		}
		if m.ContentText != nil {
			dto.ContentText = *m.ContentText
		}
		if m.AttachmentID != nil {
			dto.AttachmentID = *m.AttachmentID
		}
		if len(m.PayloadJSON) > 0 {
			dto.PayloadJSON = json.RawMessage(m.PayloadJSON)
		}
		result = append(result, dto)
	}
	c.JSON(http.StatusOK, gin.H{"messages": result})
}

type GroupHistoryRequest struct {
	GroupID string `form:"group_id" json:"group_id" binding:"required"`
}

func (h *MessageHandler) GetLatestByGroup(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	var req GroupHistoryRequest
	if err := c.ShouldBind(&req); err != nil || req.GroupID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少群组ID"})
		return
	}

	var room models.Room
	if err := h.db.Unscoped().Where("id = ? AND room_type = ?", req.GroupID, models.RoomTypeGroup).First(&room).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "群组不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询群组失败"})
		return
	}

	var member models.RoomMember
	if err := h.db.Where("room_id = ? AND user_id = ?", room.ID, userID).First(&member).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusForbidden, gin.H{"error": "您不是该群成员"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询成员关系失败"})
		return
	}

	query := h.db.Where("room_id = ?", room.ID)
	if member.LeftAt != nil {
		query = query.Where("created_at < ?", member.LeftAt)
	}

	var list []models.Message
	if err := query.
		Order("sequence_id desc").
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
			SequenceID:  m.SequenceID,
			ContentType: string(m.ContentType),
			CreatedAt:   m.CreatedAt.Unix(),
		}
		if m.SenderID != nil {
			dto.SenderID = *m.SenderID
		}
		if m.ContentText != nil {
			dto.ContentText = *m.ContentText
		}
		if m.AttachmentID != nil {
			dto.AttachmentID = *m.AttachmentID
		}
		if len(m.PayloadJSON) > 0 {
			dto.PayloadJSON = json.RawMessage(m.PayloadJSON)
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
		Order("sequence_id desc").
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
			SequenceID:  m.SequenceID,
			ContentType: string(m.ContentType),
			CreatedAt:   m.CreatedAt.Unix(),
		}
		if m.SenderID != nil {
			dto.SenderID = *m.SenderID
		}
		if m.ContentText != nil {
			dto.ContentText = *m.ContentText
		}
		if m.AttachmentID != nil {
			dto.AttachmentID = *m.AttachmentID
		}
		if len(m.PayloadJSON) > 0 {
			dto.PayloadJSON = json.RawMessage(m.PayloadJSON)
		}
		result = append(result, dto)
	}
	c.JSON(http.StatusOK, gin.H{"messages": result})
}
