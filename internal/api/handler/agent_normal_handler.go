package handler

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	grpcpb "ququchat/agent/pkg/grpcpb"
)

type AgentNormalHandler struct {
	addr      string
	connErr   error
	conn      *grpc.ClientConn
	agentConn grpcpb.AgentServiceClient
}

type SubmitAddTaskRequest struct {
	A int64 `json:"a"`
	B int64 `json:"b"`
}

func NewAgentNormalHandler() *AgentNormalHandler {
	addr := strings.TrimSpace(os.Getenv("AGENT_GRPC_ADDR"))
	if addr == "" {
		addr = "127.0.0.1:50051"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, addr, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		return &AgentNormalHandler{
			addr:    addr,
			connErr: err,
		}
	}
	return &AgentNormalHandler{
		addr:      addr,
		conn:      conn,
		agentConn: grpcpb.NewAgentServiceClient(conn),
	}
}

func (h *AgentNormalHandler) SubmitAddTask(c *gin.Context) {
	if h.connErr != nil || h.agentConn == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":      "agent 服务不可用",
			"agent_addr": h.addr,
		})
		return
	}
	var req SubmitAddTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
		return
	}
	timeoutCtx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()
	resp, err := h.agentConn.Add(timeoutCtx, &grpcpb.AddRequest{
		A: req.A,
		B: req.B,
	})
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"error":      "调用 agent 失败",
			"details":    err.Error(),
			"agent_addr": h.addr,
		})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"task_type":  resp.GetTaskType().String(),
		"sum":        resp.GetSum(),
		"agent_addr": h.addr,
	})
}
