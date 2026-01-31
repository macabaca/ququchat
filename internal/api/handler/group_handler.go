package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"ququchat/internal/models"
)

type GroupHandler struct {
	db *gorm.DB
}

func NewGroupHandler(db *gorm.DB) *GroupHandler {
	return &GroupHandler{db: db}
}

type CreateGroupRequest struct {
	Name      string   `json:"name"`
	MemberIDs []string `json:"member_ids"`
}

func (h *GroupHandler) CreateGroup(c *gin.Context) {
	currentUserID := c.GetString("user_id")
	if currentUserID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	var req CreateGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
		return
	}
	if req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "群名称不能为空"})
		return
	}
	now := time.Now()
	room := models.Room{
		ID:          uuid.NewString(),
		RoomType:    models.RoomTypeGroup,
		Name:        req.Name,
		OwnerUserID: currentUserID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := h.db.Create(&room).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建群失败"})
		return
	}
	ownerMember := models.RoomMember{
		RoomID:   room.ID,
		UserID:   currentUserID,
		Role:     models.MemberRoleOwner,
		JoinedAt: now,
	}
	if err := h.db.Create(&ownerMember).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建群成员失败"})
		return
	}
	if len(req.MemberIDs) > 0 {
		seen := map[string]bool{
			currentUserID: true,
		}
		var members []models.RoomMember
		for _, uid := range req.MemberIDs {
			if uid == "" || seen[uid] {
				continue
			}
			seen[uid] = true
			members = append(members, models.RoomMember{
				RoomID:   room.ID,
				UserID:   uid,
				Role:     models.MemberRoleMember,
				JoinedAt: now,
				InviteBy: &currentUserID,
			})
		}
		if len(members) > 0 {
			if err := h.db.Create(&members).Error; err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "创建群成员失败"})
				return
			}
		}
	}
	var memberCount int64
	if err := h.db.Model(&models.RoomMember{}).Where("room_id = ?", room.ID).Count(&memberCount).Error; err != nil {
		memberCount = 1
	}
	c.JSON(http.StatusCreated, gin.H{
		"group": gin.H{
			"id":           room.ID,
			"name":         room.Name,
			"owner_id":     room.OwnerUserID,
			"member_count": memberCount,
			"created_at":   room.CreatedAt,
		},
	})
}

func (h *GroupHandler) GetGroupDetail(c *gin.Context) {
	currentUserID := c.GetString("user_id")
	if currentUserID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	groupID := c.Param("group_id")
	if groupID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少群ID"})
		return
	}
	var room models.Room
	if err := h.db.Where("id = ? AND room_type = ?", groupID, models.RoomTypeGroup).First(&room).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "群不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询群信息失败"})
		return
	}
	var member models.RoomMember
	if err := h.db.Where("room_id = ? AND user_id = ?", room.ID, currentUserID).First(&member).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusForbidden, gin.H{"error": "未加入该群"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询群成员失败"})
		return
	}
	var memberCount int64
	if err := h.db.Model(&models.RoomMember{}).Where("room_id = ?", room.ID).Count(&memberCount).Error; err != nil {
		memberCount = 0
	}
	c.JSON(http.StatusOK, gin.H{
		"group": gin.H{
			"id":           room.ID,
			"name":         room.Name,
			"owner_id":     room.OwnerUserID,
			"member_count": memberCount,
			"my_role":      member.Role,
			"created_at":   room.CreatedAt,
			"updated_at":   room.UpdatedAt,
		},
	})
}

func (h *GroupHandler) ListMyGroups(c *gin.Context) {
	currentUserID := c.GetString("user_id")
	if currentUserID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	var memberships []models.RoomMember
	if err := h.db.Where("user_id = ?", currentUserID).Find(&memberships).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询群成员关系失败"})
		return
	}
	if len(memberships) == 0 {
		c.JSON(http.StatusOK, gin.H{"groups": []interface{}{}})
		return
	}
	roomIDs := make([]string, 0, len(memberships))
	roleByRoom := make(map[string]models.MemberRole, len(memberships))
	leftAtByRoom := make(map[string]*time.Time, len(memberships))
	for _, m := range memberships {
		roomIDs = append(roomIDs, m.RoomID)
		roleByRoom[m.RoomID] = m.Role
		leftAtByRoom[m.RoomID] = m.LeftAt
	}
	var rooms []models.Room
	if err := h.db.Unscoped().Where("id IN ? AND room_type = ?", roomIDs, models.RoomTypeGroup).Find(&rooms).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询群信息失败"})
		return
	}
	if len(rooms) == 0 {
		c.JSON(http.StatusOK, gin.H{"groups": []interface{}{}})
		return
	}
	resp := make([]gin.H, 0, len(rooms))
	for _, r := range rooms {
		var memberCount int64
		if err := h.db.Model(&models.RoomMember{}).Where("room_id = ? AND left_at IS NULL", r.ID).Count(&memberCount).Error; err != nil {
			memberCount = 0
		}

		status := "active"
		if r.DeletedAt.Valid {
			status = "dismissed"
		} else if leftAtByRoom[r.ID] != nil {
			status = "left"
		}

		resp = append(resp, gin.H{
			"id":           r.ID,
			"name":         r.Name,
			"owner_id":     r.OwnerUserID,
			"member_count": memberCount,
			"my_role":      roleByRoom[r.ID],
			"created_at":   r.CreatedAt,
			"status":       status,
		})
	}
	c.JSON(http.StatusOK, gin.H{"groups": resp})
}

func (h *GroupHandler) DismissGroup(c *gin.Context) {
	currentUserID := c.GetString("user_id")
	if currentUserID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	groupID := c.Param("group_id")
	if groupID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少群ID"})
		return
	}
	var room models.Room
	if err := h.db.Where("id = ? AND room_type = ?", groupID, models.RoomTypeGroup).First(&room).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "群不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询群信息失败"})
		return
	}
	if room.OwnerUserID != currentUserID {
		c.JSON(http.StatusForbidden, gin.H{"error": "只有群主可以解散群"})
		return
	}

	now := time.Now()
	// Soft delete the room
	if err := h.db.Model(&room).Update("deleted_at", now).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "解散群失败"})
		return
	}
	// Optionally mark all members as left
	if err := h.db.Model(&models.RoomMember{}).Where("room_id = ?", room.ID).Update("left_at", now).Error; err != nil {
		// Log error but don't fail the request since room is already deleted
	}

	c.JSON(http.StatusOK, gin.H{"message": "群已解散"})
}

type AddMembersRequest struct {
	UserIDs []string `json:"user_ids" binding:"required"`
}

func (h *GroupHandler) AddMembers(c *gin.Context) {
	currentUserID := c.GetString("user_id")
	if currentUserID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	groupID := c.Param("group_id")
	if groupID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少群ID"})
		return
	}
	var req AddMembersRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数错误"})
		return
	}
	if len(req.UserIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "未指定要添加的用户"})
		return
	}

	var room models.Room
	if err := h.db.Where("id = ? AND room_type = ?", groupID, models.RoomTypeGroup).First(&room).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "群不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询群信息失败"})
		return
	}

	// Check permission: Owner or Admin (Admin role logic simplified here, check if user is member with role)
	var operator models.RoomMember
	if err := h.db.Where("room_id = ? AND user_id = ?", groupID, currentUserID).First(&operator).Error; err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "您不是群成员"})
		return
	}
	if operator.Role != models.MemberRoleOwner && operator.Role != models.MemberRoleAdmin {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足，仅群主或管理员可拉人"})
		return
	}

	now := time.Now()
	var addedCount int
	for _, uid := range req.UserIDs {
		// Check if already member
		var existing models.RoomMember
		err := h.db.Where("room_id = ? AND user_id = ?", groupID, uid).First(&existing).Error
		if err == nil {
			// Already member, check if left
			if existing.LeftAt != nil {
				// Re-join
				h.db.Model(&existing).Updates(map[string]interface{}{
					"left_at":   nil,
					"joined_at": now,
					"invite_by": currentUserID,
				})
				addedCount++
			}
			continue
		} else if err != gorm.ErrRecordNotFound {
			continue
		}

		// New member
		newMember := models.RoomMember{
			RoomID:   groupID,
			UserID:   uid,
			Role:     models.MemberRoleMember,
			JoinedAt: now,
			InviteBy: &currentUserID,
		}
		if err := h.db.Create(&newMember).Error; err == nil {
			addedCount++
		}
	}

	c.JSON(http.StatusOK, gin.H{"message": "操作完成", "added_count": addedCount})
}

type RemoveMemberRequest struct {
	UserID string `json:"user_id" binding:"required"`
}

func (h *GroupHandler) RemoveMember(c *gin.Context) {
	currentUserID := c.GetString("user_id")
	if currentUserID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	groupID := c.Param("group_id")
	if groupID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少群ID"})
		return
	}
	var req RemoveMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数错误"})
		return
	}

	// Check permission
	var operator models.RoomMember
	if err := h.db.Where("room_id = ? AND user_id = ?", groupID, currentUserID).First(&operator).Error; err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "您不是群成员"})
		return
	}
	// Target member
	var target models.RoomMember
	if err := h.db.Where("room_id = ? AND user_id = ?", groupID, req.UserID).First(&target).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "目标用户不在群内"})
		return
	}

	// Owner can kick anyone; Admin can kick Member; Member cannot kick anyone
	canKick := false
	if operator.Role == models.MemberRoleOwner {
		canKick = true
	} else if operator.Role == models.MemberRoleAdmin {
		if target.Role == models.MemberRoleMember {
			canKick = true
		}
	}

	if !canKick {
		c.JSON(http.StatusForbidden, gin.H{"error": "权限不足"})
		return
	}

	now := time.Now()
	if err := h.db.Model(&target).Update("left_at", now).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "移除成员失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "成员已移除"})
}

func (h *GroupHandler) LeaveGroup(c *gin.Context) {
	currentUserID := c.GetString("user_id")
	if currentUserID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	groupID := c.Param("group_id")
	if groupID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少群ID"})
		return
	}

	var member models.RoomMember
	if err := h.db.Where("room_id = ? AND user_id = ?", groupID, currentUserID).First(&member).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusBadRequest, gin.H{"error": "您不在该群中"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询成员状态失败"})
		return
	}

	if member.Role == models.MemberRoleOwner {
		c.JSON(http.StatusForbidden, gin.H{"error": "群主不能直接退出，请先转让群主或解散群"})
		return
	}

	now := time.Now()
	if err := h.db.Model(&member).Update("left_at", now).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "退出群失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "已退出群"})
}

func (h *GroupHandler) ListGroupMembers(c *gin.Context) {
	currentUserID := c.GetString("user_id")
	if currentUserID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	groupID := c.Param("group_id")
	if groupID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少群ID"})
		return
	}

	// Verify membership (optional, but good for privacy)
	var me models.RoomMember
	if err := h.db.Where("room_id = ? AND user_id = ? AND left_at IS NULL", groupID, currentUserID).First(&me).Error; err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "您不是该群成员"})
		return
	}

	var members []models.RoomMember
	if err := h.db.Preload("User").Where("room_id = ? AND left_at IS NULL", groupID).Find(&members).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询群成员失败"})
		return
	}

	type MemberResp struct {
		UserID   string            `json:"user_id"`
		Username string            `json:"username"`
		Nickname string            `json:"nickname"` // Nickname in room
		Role     models.MemberRole `json:"role"`
		JoinedAt time.Time         `json:"joined_at"`
	}

	resp := make([]MemberResp, 0, len(members))
	for _, m := range members {
		nickname := ""
		if m.NicknameInRoom != nil {
			nickname = *m.NicknameInRoom
		} else if m.User != nil {
			if m.User.DisplayName != nil {
				nickname = *m.User.DisplayName
			} else {
				nickname = m.User.Username
			}
		}

		username := ""
		if m.User != nil {
			username = m.User.Username
		}

		resp = append(resp, MemberResp{
			UserID:   m.UserID,
			Username: username,
			Nickname: nickname,
			Role:     m.Role,
			JoinedAt: m.JoinedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{"members": resp})
}

type AddAdminsRequest struct {
	UserIDs []string `json:"user_ids" binding:"required"`
}

// AddAdmins 批量设置管理员（仅群主可用）
func (h *GroupHandler) AddAdmins(c *gin.Context) {
	currentUserID := c.GetString("user_id")
	if currentUserID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	groupID := c.Param("group_id")
	if groupID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少群ID"})
		return
	}
	var req AddAdminsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求参数错误"})
		return
	}
	if len(req.UserIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "未指定用户"})
		return
	}

	// 1. Check Group existence
	var room models.Room
	if err := h.db.Where("id = ? AND room_type = ?", groupID, models.RoomTypeGroup).First(&room).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "群不存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询群信息失败"})
		return
	}

	// 2. Check Requester Permission (Must be Owner)
	if room.OwnerUserID != currentUserID {
		c.JSON(http.StatusForbidden, gin.H{"error": "只有群主可以设置管理员"})
		return
	}

	// 3. Update roles
	// Update all matching members who are NOT the owner to Admin
	// 排除群主自己（虽然群主一般不在列表里，但为了安全）以及已经退群的人
	result := h.db.Model(&models.RoomMember{}).
		Where("room_id = ? AND user_id IN ? AND user_id != ? AND left_at IS NULL", groupID, req.UserIDs, currentUserID).
		Update("role", models.MemberRoleAdmin)

	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "设置管理员失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":       "操作成功",
		"updated_count": result.RowsAffected,
	})
}
