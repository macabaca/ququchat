package middleware

import (
    "errors"
    "net/http"
    "strings"

    "github.com/gin-gonic/gin"
    "github.com/golang-jwt/jwt/v5"

    "ququchat/internal/server/auth"
)

// JWTAuth Gin 中间件：验证 Bearer 令牌，将用户信息注入到 Context
func JWTAuth(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "缺少 Authorization 头"})
			return
		}
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Authorization 头格式错误"})
			return
		}
        tokenStr := strings.TrimSpace(parts[1])
        claims, err := auth.ParseAndValidate(tokenStr, secret)
        if err != nil {
            if errors.Is(err, jwt.ErrTokenExpired) {
                c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "访问令牌已过期"})
                return
            }
            c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "访问令牌无效"})
            return
        }
		// 注入用户信息到上下文
		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)
		c.Next()
	}
}
