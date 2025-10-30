package handler

import (
    "net/http"

    "github.com/gin-gonic/gin"
    "gorm.io/gorm"
)

type UserHandler struct {
    db *gorm.DB
}

func NewUserHandler(db *gorm.DB) *UserHandler {
    return &UserHandler{db: db}
}

// 占位示例：后续实现用户信息查询/更新
func (h *UserHandler) Placeholder(c *gin.Context) {
    c.JSON(http.StatusNotImplemented, gin.H{"error": "未实现"})
}