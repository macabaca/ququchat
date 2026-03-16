package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	kafkago "github.com/segmentio/kafka-go"

	"ququchat/agent/pkg/llmmsg"
	llmsvc "ququchat/internal/service/llm"
)

type AgentLLMHandler struct {
	llmKafkaErr        error
	llmIngressProducer *kafkago.Writer
	llmIngressTopic    string
	llmResultTopic     string
	llmResultConsumer  *kafkago.Reader
	hub                *Hub
	llmTaskService     *llmsvc.Service
	llmResultCache     map[string]llmmsg.Result
	llmResultMu        sync.RWMutex
	taskOwnerByID      map[string]string
	taskOwnerMu        sync.RWMutex
	taskTypeByID       map[string]string
	taskTypeMu         sync.RWMutex
}

type SubmitFakeLLMTaskRequest struct {
	RequestID string `json:"request_id"`
	UserID    string `json:"user_id"`
	Priority  string `json:"priority"`
	Prompt    string `json:"prompt"`
	SleepMs   int64  `json:"sleep_ms"`
}

func NewAgentLLMHandler(hub *Hub) *AgentLLMHandler {
	h := &AgentLLMHandler{
		llmResultCache: make(map[string]llmmsg.Result),
		taskOwnerByID:  make(map[string]string),
		taskTypeByID:   make(map[string]string),
		hub:            hub,
		llmTaskService: llmsvc.NewService(),
	}
	brokers := splitCSV(strings.TrimSpace(os.Getenv("AGENT_LLM_KAFKA_BROKERS")))
	if len(brokers) == 0 {
		brokers = []string{"127.0.0.1:9092"}
	}
	ingressTopic := strings.TrimSpace(os.Getenv("AGENT_LLM_INGRESS_TOPIC"))
	if ingressTopic == "" {
		ingressTopic = "agent.llm.ingress"
	}
	if len(brokers) > 0 && ingressTopic != "" {
		h.llmIngressProducer = &kafkago.Writer{
			Addr:     kafkago.TCP(brokers...),
			Topic:    ingressTopic,
			Balancer: &kafkago.LeastBytes{},
		}
		h.llmIngressTopic = ingressTopic
	}
	resultTopic := strings.TrimSpace(os.Getenv("AGENT_LLM_RESULT_TOPIC"))
	if resultTopic == "" {
		resultTopic = "agent.llm.result"
	}
	resultGroupID := strings.TrimSpace(os.Getenv("AGENT_LLM_RESULT_GROUP_ID"))
	if resultGroupID == "" {
		resultGroupID = "backend-llm-result-consumer"
	}
	if len(brokers) > 0 && resultTopic != "" {
		h.llmResultTopic = resultTopic
		h.llmResultConsumer = kafkago.NewReader(kafkago.ReaderConfig{
			Brokers: brokers,
			Topic:   resultTopic,
			GroupID: resultGroupID,
		})
		go h.consumeLLMResult()
	}
	return h
}

func (h *AgentLLMHandler) SubmitLLMTask(c *gin.Context) {
	if h.llmKafkaErr != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"error":   "llm kafka 不可用",
			"details": h.llmKafkaErr.Error(),
		})
		return
	}
	if h.llmIngressProducer == nil || strings.TrimSpace(h.llmIngressTopic) == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "llm kafka producer 未配置"})
		return
	}
	var req llmsvc.SubmitTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
		return
	}
	h.submitLLMTask(c, &req)
}

func (h *AgentLLMHandler) submitLLMTask(c *gin.Context, req *llmsvc.SubmitTaskRequest) {
	if req == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
		return
	}
	msg, userID, err := h.llmTaskService.BuildIngress(req, c.GetString("user_id"))
	if err != nil {
		h.respondTaskBuildError(c, err)
		return
	}
	h.taskOwnerMu.Lock()
	h.taskOwnerByID[msg.TaskID] = userID
	h.taskOwnerMu.Unlock()
	h.taskTypeMu.Lock()
	h.taskTypeByID[msg.TaskID] = msg.TaskType
	h.taskTypeMu.Unlock()
	b, err := json.Marshal(msg)
	if err != nil {
		h.taskOwnerMu.Lock()
		delete(h.taskOwnerByID, msg.TaskID)
		h.taskOwnerMu.Unlock()
		h.taskTypeMu.Lock()
		delete(h.taskTypeByID, msg.TaskID)
		h.taskTypeMu.Unlock()
		c.JSON(http.StatusInternalServerError, gin.H{"error": "序列化 llm 任务失败"})
		return
	}
	timeoutCtx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()
	if err := h.llmIngressProducer.WriteMessages(timeoutCtx, kafkago.Message{
		Key:   []byte(msg.TaskID),
		Value: b,
	}); err != nil {
		h.taskOwnerMu.Lock()
		delete(h.taskOwnerByID, msg.TaskID)
		h.taskOwnerMu.Unlock()
		h.taskTypeMu.Lock()
		delete(h.taskTypeByID, msg.TaskID)
		h.taskTypeMu.Unlock()
		c.JSON(http.StatusBadGateway, gin.H{
			"error":   "投递 llm 任务失败",
			"details": err.Error(),
		})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{
		"accepted":      true,
		"task_id":       msg.TaskID,
		"request_id":    msg.RequestID,
		"task_type":     msg.TaskType,
		"priority":      msg.Priority,
		"kafka_topic":   h.llmIngressTopic,
		"executionType": "async_llm_task",
	})
}

func (h *AgentLLMHandler) SubmitFakeLLMTask(c *gin.Context) {
	var req SubmitFakeLLMTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "请求体格式错误"})
		return
	}
	input, err := h.llmTaskService.BuildFakeRequestInput(req.Prompt, req.SleepMs)
	if err != nil {
		h.respondTaskBuildError(c, err)
		return
	}
	genericReq := llmsvc.SubmitTaskRequest{
		RequestID: strings.TrimSpace(req.RequestID),
		UserID:    strings.TrimSpace(req.UserID),
		TaskType:  "fake_llm",
		Priority:  strings.TrimSpace(req.Priority),
		Input:     input,
	}
	h.submitLLMTask(c, &genericReq)
}

func (h *AgentLLMHandler) respondTaskBuildError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, llmsvc.ErrTaskTypeRequired):
		c.JSON(http.StatusBadRequest, gin.H{"error": "task_type 不能为空"})
	case errors.Is(err, llmsvc.ErrUnsupportedTaskType):
		c.JSON(http.StatusBadRequest, gin.H{"error": "不支持的 task_type"})
	case errors.Is(err, llmsvc.ErrTaskInputRequired):
		c.JSON(http.StatusBadRequest, gin.H{"error": "input 不能为空"})
	case errors.Is(err, llmsvc.ErrInvalidTaskInput):
		c.JSON(http.StatusBadRequest, gin.H{"error": "input 格式错误"})
	case errors.Is(err, llmsvc.ErrPromptRequired):
		c.JSON(http.StatusBadRequest, gin.H{"error": "prompt 不能为空"})
	case errors.Is(err, llmsvc.ErrUserIDRequired):
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id 不能为空"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "构建 llm 任务失败"})
	}
}

func splitCSV(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		s := strings.TrimSpace(p)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func (h *AgentLLMHandler) consumeLLMResult() {
	if h.llmResultConsumer == nil {
		return
	}
	ctx := context.Background()
	for {
		msg, err := h.llmResultConsumer.FetchMessage(ctx)
		if err != nil {
			log.Printf("[backend-llm-result] fetch failed: %v", err)
			return
		}
		var res llmmsg.Result
		if err := json.Unmarshal(msg.Value, &res); err != nil {
			_ = h.llmResultConsumer.CommitMessages(ctx, msg)
			continue
		}
		if strings.TrimSpace(res.TaskID) != "" {
			h.llmResultMu.Lock()
			h.llmResultCache[res.TaskID] = res
			h.llmResultMu.Unlock()
			h.pushResultToUser(&res)
		}
		if err := h.llmResultConsumer.CommitMessages(ctx, msg); err != nil {
			log.Printf("[backend-llm-result] commit failed: %v", err)
			return
		}
	}
}

func (h *AgentLLMHandler) pushResultToUser(res *llmmsg.Result) {
	if h == nil || h.hub == nil || res == nil || strings.TrimSpace(res.TaskID) == "" {
		return
	}
	h.taskOwnerMu.RLock()
	userID := strings.TrimSpace(h.taskOwnerByID[res.TaskID])
	h.taskOwnerMu.RUnlock()
	if userID == "" {
		return
	}
	h.taskTypeMu.RLock()
	fallbackTaskType := strings.TrimSpace(h.taskTypeByID[res.TaskID])
	h.taskTypeMu.RUnlock()
	envelope, err := h.llmTaskService.BuildResultEnvelope(res, fallbackTaskType)
	if err != nil {
		log.Printf("[backend-llm-result] build ws envelope failed task=%s err=%v", res.TaskID, err)
		return
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		log.Printf("[backend-llm-result] marshal ws payload failed task=%s err=%v", res.TaskID, err)
		return
	}
	h.hub.SendDataToUser(userID, payload)
	h.taskOwnerMu.Lock()
	delete(h.taskOwnerByID, res.TaskID)
	h.taskOwnerMu.Unlock()
	h.taskTypeMu.Lock()
	delete(h.taskTypeByID, res.TaskID)
	h.taskTypeMu.Unlock()
}
