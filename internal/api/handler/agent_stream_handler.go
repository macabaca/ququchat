package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	taskservice "ququchat/internal/service"
)

type AgentStreamHandler struct {
	streamHub *taskservice.AgentStreamHub
}

func NewAgentStreamHandler(streamHub *taskservice.AgentStreamHub) *AgentStreamHandler {
	return &AgentStreamHandler{streamHub: streamHub}
}

func (h *AgentStreamHandler) Stream(c *gin.Context) {
	if h == nil || h.streamHub == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "agent stream is not ready"})
		return
	}
	userID := strings.TrimSpace(c.GetString("user_id"))
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	requestID := strings.TrimSpace(c.Query("request_id"))
	if requestID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "request_id is required"})
		return
	}
	requestUserID, _, _, _, ok := taskservice.ParseWSCommandRequestID(requestID)
	if !ok || strings.TrimSpace(requestUserID) != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "无权订阅该请求"})
		return
	}
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming not supported"})
		return
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	ch, replay, unsubscribe := h.streamHub.Subscribe(requestID)
	defer unsubscribe()
	startEvent := taskservice.AgentStreamEvent{
		EventType: "agent.start",
		RequestID: requestID,
		UserID:    userID,
		Status:    "running",
	}
	if err := writeSSEEvent(c, startEvent); err != nil {
		return
	}
	flusher.Flush()
	for _, event := range replay {
		if err := writeSSEEvent(c, event); err != nil {
			return
		}
		flusher.Flush()
	}
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case event, ok := <-ch:
			if !ok {
				return
			}
			if err := writeSSEEvent(c, event); err != nil {
				return
			}
			flusher.Flush()
			if event.EventType == "agent.done" || event.EventType == "agent.error" {
				return
			}
		case <-heartbeat.C:
			if _, err := c.Writer.Write([]byte("event: ping\ndata: {}\n\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func writeSSEEvent(c *gin.Context, event taskservice.AgentStreamEvent) error {
	eventType := strings.TrimSpace(event.EventType)
	if eventType == "" {
		eventType = "agent.step"
	}
	b, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if strings.TrimSpace(event.EventID) != "" {
		if _, err := c.Writer.Write([]byte("id: " + strings.TrimSpace(event.EventID) + "\n")); err != nil {
			return err
		}
	}
	if _, err := c.Writer.Write([]byte("event: " + eventType + "\n")); err != nil {
		return err
	}
	if _, err := c.Writer.Write([]byte("data: " + string(b) + "\n\n")); err != nil {
		return err
	}
	return nil
}
