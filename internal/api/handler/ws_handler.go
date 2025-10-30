package handler

import (
    "net/http"

    "github.com/gin-gonic/gin"
    "gorm.io/gorm"
)

type WsHandler struct {
    db *gorm.DB
}

func NewWsHandler(db *gorm.DB) *WsHandler {
    return &WsHandler{db: db}
}

// 占位示例：后续实现 WebSocket 升级与消息处理
func (h *WsHandler) Placeholder(c *gin.Context) {
    c.JSON(http.StatusNotImplemented, gin.H{"error": "未实现"})
}