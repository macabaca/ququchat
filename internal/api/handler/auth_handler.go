package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	mysqlerr "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"ququchat/internal/models"
	"ququchat/internal/server/auth"
)

type AuthHandler struct {
	db        *gorm.DB
	jwtSecret string
}

func NewAuthHandler(db *gorm.DB, jwtSecret string) *AuthHandler {
	return &AuthHandler{db: db, jwtSecret: jwtSecret}
}

type RegisterRequest struct {
	Username string  `json:"username"`
	Password string  `json:"password"`
	Email    *string `json:"email,omitempty"`
	Phone    *string `json:"phone,omitempty"`
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type LogoutRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// Logout 注销当前设备：验证并吊销当前访问令牌对应的会话
func (h *AuthHandler) Logout(c *gin.Context) {
	// 使用中间件注入的用户信息
	currentUserID := c.GetString("user_id")
	if currentUserID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}

	// 从 JSON 或 Cookie 读取刷新令牌，用于定位当前设备会话
	var req LogoutRequest
	_ = c.ShouldBindJSON(&req)
	token := strings.TrimSpace(req.RefreshToken)
	if token == "" {
		if cookie, err := c.Cookie("refresh_token"); err == nil {
			token = cookie
		}
	}
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少刷新令牌"})
		return
	}

	// 查找并吊销对应的会话（限定为当前用户，避免越权）
	var session models.AuthSession
	if err := h.db.Where("refresh_token = ? AND user_id = ? AND revoked_at IS NULL", token, currentUserID).First(&session).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{"error": "未找到有效会话"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询会话失败"})
		return
	}

	// 吊销会话
	now := time.Now()
	if err := h.db.Model(&session).Update("revoked_at", &now).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "吊销会话失败"})
		return
	}

	// 清除 Cookie
	c.SetCookie("refresh_token", "", -1, "/", "", false, true)
	c.JSON(http.StatusOK, gin.H{"message": "已成功登出当前设备"})
}

// // LogoutAll 注销所有设备：吊销该用户的所有有效会话
// func (h *AuthHandler) LogoutAll(c *gin.Context) {
// 	userID := c.GetString("user_id")
// 	if userID == "" {
// 		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
// 		return
// 	}

// 	// 吊销该用户所有未吊销的会话
// 	if err := h.db.Model(&models.AuthSession{}).
// 		Where("user_id = ? AND revoked_at IS NULL", userID).
// 		Update("revoked_at", time.Now()).Error; err != nil {
// 		c.JSON(http.StatusInternalServerError, gin.H{"error": "批量吊销会话失败"})
// 		return
// 	}

// 	// 清除当前设备的 Cookie
// 	c.SetCookie("refresh_token", "", -1, "/", "", false, true)
// 	c.JSON(http.StatusOK, gin.H{"message": "已成功登出所有设备"})
// }

func (h *AuthHandler) Register(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || len(req.Password) < 6 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "用户名不能为空，密码至少6位"})
		return
	}

	// 生成密码哈希
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "密码哈希失败"})
		return
	}

	u := models.User{
		ID:           uuid.NewString(),
		Username:     req.Username,
		Email:        req.Email,
		Phone:        req.Phone,
		PasswordHash: string(hash),
		Status:       "active",
	}

	if err := h.db.Create(&u).Error; err != nil {
		if isDuplicateKeyErr(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "用户名或邮箱/手机号已存在"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "创建用户失败"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"user": gin.H{
			"id":       u.ID,
			"username": u.Username,
			"status":   u.Status,
		},
	})
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || req.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "用户名和密码不能为空"})
		return
	}

	var u models.User
	if err := h.db.Where("username = ?", req.Username).First(&u).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询用户失败"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "用户名或密码错误"})
		return
	}

	// 登录成功：签发访问令牌 + 刷新令牌
	accessTTL := 15 * time.Minute
	accessToken, _, err := auth.SignAccessToken(u.ID, u.Username, accessTTL, h.jwtSecret)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "签发令牌失败"})
		return
	}

	refreshTTL := 30 * 24 * time.Hour
	refreshToken, err := auth.GenerateRefreshToken(32)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成刷新令牌失败"})
		return
	}

	// 记录登录会话（刷新令牌）
	ip := c.ClientIP()
	ua := c.GetHeader("User-Agent")
	session := models.AuthSession{
		ID:           uuid.NewString(),
		UserID:       u.ID,
		ExpiresAt:    time.Now().Add(refreshTTL),
		CreatedAt:    time.Now(),
		RefreshToken: &refreshToken,
	}
	if ip != "" {
		session.IP = &ip
	}
	if ua != "" {
		session.UserAgent = &ua
	}
	if err := h.db.Create(&session).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "记录刷新令牌失败"})
		return
	}

	// 设置 HttpOnly Cookie（Web 客户端友好）
	c.SetCookie("refresh_token", refreshToken, int(refreshTTL.Seconds()), "/", "", false, true)

	c.JSON(http.StatusOK, gin.H{
		"accessToken":  accessToken,
		"refreshToken": refreshToken,
		"user":         gin.H{"id": u.ID, "username": u.Username, "status": u.Status},
	})
}

// Refresh 使用刷新令牌换取新的访问令牌与刷新令牌（令牌轮换）
func (h *AuthHandler) Refresh(c *gin.Context) {
	// 从 JSON 或 Cookie 读取刷新令牌
	var req RefreshRequest
	_ = c.ShouldBindJSON(&req)
	token := req.RefreshToken
	if token == "" {
		if cookie, err := c.Cookie("refresh_token"); err == nil {
			token = cookie
		}
	}
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "缺少刷新令牌"})
		return
	}

	// 查找会话并校验有效性
	var sess models.AuthSession
	if err := h.db.Where("refresh_token = ?", token).First(&sess).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "刷新令牌无效"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "查询会话失败"})
		return
	}
	if sess.RevokedAt != nil || time.Now().After(sess.ExpiresAt) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "刷新令牌已失效"})
		return
	}

	// 加载用户
	var u models.User
	if err := h.db.Where("id = ?", sess.UserID).First(&u).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "关联用户不存在"})
		return
	}

	// 轮换：吊销旧刷新令牌
	now := time.Now()
	if err := h.db.Model(&sess).Update("revoked_at", &now).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "吊销旧令牌失败"})
		return
	}

	// 生成新的访问令牌与刷新令牌
	accessTTL := 15 * time.Minute
	accessToken, _, err := auth.SignAccessToken(u.ID, u.Username, accessTTL, h.jwtSecret)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "签发新访问令牌失败"})
		return
	}
	refreshTTL := 30 * 24 * time.Hour
	newRefresh, err := auth.GenerateRefreshToken(32)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "生成新刷新令牌失败"})
		return
	}

	ip := c.ClientIP()
	ua := c.GetHeader("User-Agent")
	newSess := models.AuthSession{
		ID:           uuid.NewString(),
		UserID:       u.ID,
		ExpiresAt:    time.Now().Add(refreshTTL),
		CreatedAt:    time.Now(),
		RefreshToken: &newRefresh,
	}
	if ip != "" {
		newSess.IP = &ip
	}
	if ua != "" {
		newSess.UserAgent = &ua
	}
	if err := h.db.Create(&newSess).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "保存新刷新令牌失败"})
		return
	}

	// 设置新的 Cookie
	c.SetCookie("refresh_token", newRefresh, int(refreshTTL.Seconds()), "/", "", false, true)

	c.JSON(http.StatusOK, gin.H{
		"accessToken":  accessToken,
		"refreshToken": newRefresh,
	})
}

func isDuplicateKeyErr(err error) bool {
	// 优先使用 MySQLError 类型，回退到字符串匹配
	if me, ok := err.(*mysqlerr.MySQLError); ok {
		return me.Number == 1062
	}
	// 兼容部分驱动返回的错误文本
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate entry")
}
