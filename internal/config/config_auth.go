package config

import (
    "log"
    "os"
    "strings"
    "time"

    "ququchat/internal/server/auth"
)

// Auth 认证相关配置（从 YAML 读取的原始结构）
type Auth struct {
    JWTSecret         string `yaml:"jwt_secret" json:"jwt_secret"`
    AccessTTL         string `yaml:"access_ttl" json:"access_ttl"`
    RefreshTTL        string `yaml:"refresh_ttl" json:"refresh_ttl"`
    RefreshTokenBytes int    `yaml:"refresh_token_bytes" json:"refresh_token_bytes"`
}

// AuthSettings 为运行时使用的认证配置（已解析为具体类型）
type AuthSettings struct {
    JWTSecret         string
    AccessTTL         time.Duration
    RefreshTTL        time.Duration
    RefreshTokenBytes int
}

// ToSettings 解析 YAML 中的字符串时长并应用默认值，生成运行时配置
// 同时若未在配置文件提供 JWTSecret，则从环境变量回退，最后采用开发默认值
func (a Auth) ToSettings() AuthSettings {
    access := auth.DefaultAccessTTL
    refresh := auth.DefaultRefreshTTL
    if s := strings.TrimSpace(a.AccessTTL); s != "" {
        if d, err := time.ParseDuration(s); err == nil && d > 0 {
            access = d
        }
    }
    if s := strings.TrimSpace(a.RefreshTTL); s != "" {
        if d, err := time.ParseDuration(s); err == nil && d > 0 {
            refresh = d
        }
    }
    bytes := a.RefreshTokenBytes
    if bytes <= 0 {
        bytes = auth.DefaultRefreshTokenBytes
    }

    // JWTSecret: 优先使用配置文件；否则回退到环境变量；最后使用开发默认值
    secret := strings.TrimSpace(a.JWTSecret)
    if secret == "" {
        if s := os.Getenv("JWT_SECRET"); strings.TrimSpace(s) != "" {
            secret = strings.TrimSpace(s)
        } else if s := os.Getenv("QUQUCHAT_JWT_SECRET"); strings.TrimSpace(s) != "" {
            secret = strings.TrimSpace(s)
        } else {
            const dev = "dev-secret-change-me"
            log.Println("警告: 未配置 JWT_SECRET，使用开发默认值。请设置 JWT_SECRET 环境变量或配置文件！")
            secret = dev
        }
    }

    return AuthSettings{
        JWTSecret:         secret,
        AccessTTL:         access,
        RefreshTTL:        refresh,
        RefreshTokenBytes: bytes,
    }
}