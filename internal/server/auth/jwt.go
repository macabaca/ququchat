package auth

import (
    "crypto/rand"
    "encoding/base64"
    "time"

    "github.com/golang-jwt/jwt/v5"
)

// Claims 定义访问令牌的负载
type Claims struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	jwt.RegisteredClaims
}

// 移除密钥加载回退逻辑，统一在配置层处理

// SignAccessToken 生成访问令牌
func SignAccessToken(userID, username string, ttl time.Duration, secret string) (string, time.Time, error) {
	exp := time.Now().Add(ttl)
	claims := Claims{
		UserID:   userID,
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			ExpiresAt: jwt.NewNumericDate(exp),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := token.SignedString([]byte(secret))
	return s, exp, err
}

// ParseAndValidate 解析并校验访问令牌
func ParseAndValidate(tokenStr, secret string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(secret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil {
		return nil, err
	}
	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}
	return nil, jwt.ErrTokenInvalidClaims
}

// GenerateRefreshToken 生成高熵的随机刷新令牌（Base64 URL 编码）
func GenerateRefreshToken(nBytes int) (string, error) {
	if nBytes <= 0 {
		nBytes = 32
	}
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
