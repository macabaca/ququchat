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
		tokenStr, ok := extractBearerFromHeader(c)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "缺少 Authorization 头"})
			return
		}
		if !injectClaims(c, tokenStr, secret) {
			return
		}
		c.Next()
	}
}

func JWTAuthFromHeaderOrQuery(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		tokenStr, ok := extractBearerFromHeader(c)
		if !ok {
			q := strings.TrimSpace(c.Query("token"))
			if q == "" {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "缺少访问令牌"})
				return
			}
			tokenStr = q
		}
		if !injectClaims(c, tokenStr, secret) {
			return
		}
		c.Next()
	}
}

func extractBearerFromHeader(c *gin.Context) (string, bool) {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		return "", false
	}
	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}
	tokenStr := strings.TrimSpace(parts[1])
	if tokenStr == "" {
		return "", false
	}
	return tokenStr, true
}

func injectClaims(c *gin.Context, tokenStr, secret string) bool {
	claims, err := auth.ParseAndValidate(tokenStr, secret)
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "访问令牌已过期"})
			return false
		}
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "访问令牌无效"})
		return false
	}
	c.Set("user_id", claims.UserID)
	c.Set("username", claims.Username)
	return true
}

func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Vary", "Origin")
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
			reqHeaders := strings.TrimSpace(c.GetHeader("Access-Control-Request-Headers"))
			if reqHeaders != "" {
				c.Header("Access-Control-Allow-Headers", reqHeaders)
			} else {
				c.Header("Access-Control-Allow-Headers", "Authorization,Content-Type,Accept,Origin,X-Requested-With")
			}
			c.Header("Access-Control-Expose-Headers", "Content-Length,Content-Type")
			c.Header("Access-Control-Max-Age", "86400")
		}

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
