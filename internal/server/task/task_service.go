package task

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"

	"gorm.io/gorm"

	tasksvc "ququchat/internal/service/task"
	"ququchat/internal/service/task/raghandler"
)

var ErrServiceNotInitialized = errors.New("task service not initialized")
var ErrCommandRequired = errors.New("command required")
var ErrUnsupportedCommand = errors.New("unsupported command")
var ErrSummaryCountRequired = errors.New("summary count is required")
var ErrSummaryCountInvalid = errors.New("summary count must be a positive integer")
var ErrSummaryCountTooLarge = errors.New("summary count is too large")
var ErrSummaryRoomRequired = errors.New("summary room id is required")
var ErrSummarySourceEmpty = errors.New("no messages available for summary")
var ErrAgentRoomRequired = errors.New("agent room id is required")
var ErrAgentGoalRequired = errors.New("agent goal is required")
var ErrRAGRoomRequired = errors.New("rag room id is required")
var ErrRAGSearchQueryRequired = errors.New("rag search query is required")
var ErrRAGSearchTopKInvalid = errors.New("rag search topK must be a positive integer")
var ErrRAGSearchTopKTooLarge = errors.New("rag search topK is too large")
var ErrRAGSearchVectorInvalid = errors.New("rag search vector must be raw or summary")
var ErrRAGMemorySequenceRangeRequired = errors.New("rag memory start/end sequence ids are required")
var ErrRAGMemorySequenceRangeInvalid = errors.New("rag memory sequence range is invalid")

const summaryCountMax = 1000
const agentRecentMessageLimit = 12
const agentMaxSteps = 5
const ragSegmentGapSeconds = 600
const ragMaxCharsPerSegment = 2000
const ragMaxMessagesPerSeg = 24
const ragOverlapMessages = 3
const ragSearchTopKDefault = 5
const ragSearchTopKMax = 20

type TaskCallback func(ctx context.Context, doneTask *tasksvc.Task, final string, payload map[string]interface{})

type Service struct {
	db         *gorm.DB
	runtime    *tasksvc.Runtime
	callbackMu sync.Mutex
	callbacks  map[string]TaskCallback
}

func NewService(db *gorm.DB, opts tasksvc.RuntimeOptions) *Service {
	s := &Service{callbacks: make(map[string]TaskCallback)}
	upstreamOnFinish := opts.OnFinish
	opts.OnFinish = func(ctx context.Context, doneTask *tasksvc.Task) {
		s.dispatchCallback(ctx, doneTask)
		if upstreamOnFinish != nil {
			upstreamOnFinish(ctx, doneTask)
		}
	}
	if opts.Store == nil && db != nil {
		opts.Store = tasksvc.NewGormStore(db)
	}
	if opts.RAGHandler == nil {
		opts.RAGHandler = raghandler.New(raghandler.Options{
			DB:                    db,
			LLMClient:             opts.LLMClient,
			EmbeddingProvider:     opts.EmbeddingProvider,
			VectorStore:           opts.VectorStore,
			EmbeddingModelRaw:     opts.EmbeddingModelRaw,
			EmbeddingModelSummary: opts.EmbeddingModelSummary,
			SummaryVectorDim:      opts.SummaryVectorDim,
		})
	}
	s.db = db
	s.runtime = tasksvc.NewRuntime(opts)
	return s
}

func (s *Service) Start(ctx context.Context) {
	if s == nil || s.runtime == nil {
		return
	}
	go s.runtime.Start(ctx)
}

func (s *Service) SubmitFakeLLM(req tasksvc.SubmitFakeLLMRequest) (*tasksvc.Task, error) {
	if s == nil || s.runtime == nil {
		return nil, ErrServiceNotInitialized
	}
	return s.runtime.SubmitFakeLLM(req)
}

func (s *Service) SubmitLLM(req tasksvc.SubmitLLMRequest) (*tasksvc.Task, error) {
	if s == nil || s.runtime == nil {
		return nil, ErrServiceNotInitialized
	}
	return s.runtime.SubmitLLM(req)
}

func (s *Service) SubmitAgent(req tasksvc.SubmitAgentRequest) (*tasksvc.Task, error) {
	if s == nil || s.runtime == nil {
		return nil, ErrServiceNotInitialized
	}
	return s.runtime.SubmitAgent(req)
}

func (s *Service) SubmitRAG(req tasksvc.SubmitRAGRequest) (*tasksvc.Task, error) {
	if s == nil || s.runtime == nil {
		return nil, ErrServiceNotInitialized
	}
	return s.runtime.SubmitRAG(req)
}

func (s *Service) SubmitRAGAddMemory(req tasksvc.SubmitRAGAddMemoryRequest) (*tasksvc.Task, error) {
	if s == nil || s.runtime == nil {
		return nil, ErrServiceNotInitialized
	}
	return s.runtime.SubmitRAGAddMemory(req)
}

func (s *Service) Get(taskID string) (*tasksvc.Task, bool) {
	if s == nil || s.runtime == nil {
		return nil, false
	}
	return s.runtime.Get(taskID)
}

type SubmitCommandRequest struct {
	UserID  string
	RoomID  string
	Content string
}

func (s *Service) SubmitCommand(req SubmitCommandRequest, cb TaskCallback) (string, error) {
	if s == nil || s.runtime == nil {
		return "", ErrServiceNotInitialized
	}
	raw := strings.TrimSpace(req.Content)
	if raw == "" {
		return "", ErrCommandRequired
	}
	if !strings.HasPrefix(raw, "\\") {
		return "", ErrUnsupportedCommand
	}
	cmd := strings.TrimSpace(strings.TrimPrefix(raw, "\\"))
	var (
		t   *tasksvc.Task
		err error
	)
	if strings.HasPrefix(cmd, "task:fake_llm") {
		prompt := strings.TrimSpace(strings.TrimPrefix(cmd, "task:fake_llm"))
		if prompt == "" {
			prompt = cmd
		}
		t, err = s.runtime.SubmitFakeLLM(tasksvc.SubmitFakeLLMRequest{
			Priority: tasksvc.PriorityNormal,
			Prompt:   prompt,
			SleepMs:  800,
		})
	} else if strings.HasPrefix(cmd, "task:llm") {
		prompt := strings.TrimSpace(strings.TrimPrefix(cmd, "task:llm"))
		if prompt == "" {
			prompt = cmd
		}
		t, err = s.runtime.SubmitLLM(tasksvc.SubmitLLMRequest{
			Priority: tasksvc.PriorityNormal,
			Prompt:   prompt,
		})
	} else if strings.HasPrefix(cmd, "对话") {
		prompt := strings.TrimSpace(strings.TrimPrefix(cmd, "对话"))
		if prompt == "" {
			return "", ErrCommandRequired
		}
		t, err = s.runtime.SubmitLLM(tasksvc.SubmitLLMRequest{
			Priority: tasksvc.PriorityNormal,
			Prompt:   prompt,
		})
	} else if strings.HasPrefix(cmd, "生成摘要") {
		if strings.TrimSpace(req.RoomID) == "" {
			return "", ErrSummaryRoomRequired
		}
		summaryCount, parseErr := parseSummaryCount(cmd)
		if parseErr != nil {
			return "", parseErr
		}
		prompt, promptErr := s.buildSummaryPrompt(req.RoomID, summaryCount)
		if promptErr != nil {
			return "", promptErr
		}
		t, err = s.runtime.SubmitSummary(tasksvc.SubmitSummaryRequest{
			Priority: tasksvc.PriorityNormal,
			Prompt:   prompt,
		})
	} else if strings.HasPrefix(cmd, "agent") || strings.HasPrefix(cmd, "智能体") {
		if strings.TrimSpace(req.RoomID) == "" {
			return "", ErrAgentRoomRequired
		}
		goal, parseErr := parseAgentGoal(cmd)
		if parseErr != nil {
			return "", parseErr
		}
		recentMessages, recentErr := s.loadAgentRecentMessages(req.RoomID, agentRecentMessageLimit)
		if recentErr != nil {
			return "", recentErr
		}
		t, err = s.runtime.SubmitAgent(tasksvc.SubmitAgentRequest{
			Priority:       tasksvc.PriorityNormal,
			Goal:           goal,
			RecentMessages: recentMessages,
			MaxSteps:       agentMaxSteps,
			RoomID:         strings.TrimSpace(req.RoomID),
		})
	} else if strings.HasPrefix(cmd, "rag检索") {
		if strings.TrimSpace(req.RoomID) == "" {
			return "", ErrRAGRoomRequired
		}
		ragQuery, topK, vectorMode, parseErr := parseRAGSearchQuery(cmd)
		if parseErr != nil {
			return "", parseErr
		}
		t, err = s.runtime.SubmitRAGSearch(tasksvc.SubmitRAGSearchRequest{
			Priority: tasksvc.PriorityNormal,
			RoomID:   strings.TrimSpace(req.RoomID),
			Query:    ragQuery,
			TopK:     topK,
			Vector:   vectorMode,
		})
	} else if strings.HasPrefix(cmd, "添加记忆") {
		if strings.TrimSpace(req.RoomID) == "" {
			return "", ErrRAGRoomRequired
		}
		startSeq, endSeq, parseErr := parseRAGMemorySequenceRange(cmd)
		if parseErr != nil {
			return "", parseErr
		}
		t, err = s.runtime.SubmitRAGAddMemory(tasksvc.SubmitRAGAddMemoryRequest{
			Priority:           tasksvc.PriorityNormal,
			RoomID:             strings.TrimSpace(req.RoomID),
			StartSequenceID:    startSeq,
			EndSequenceID:      endSeq,
			SegmentGapSeconds:  ragSegmentGapSeconds,
			MaxCharsPerSegment: ragMaxCharsPerSegment,
			MaxMessagesPerSeg:  ragMaxMessagesPerSeg,
			OverlapMessages:    ragOverlapMessages,
		})
	} else if strings.HasPrefix(cmd, "生成rag") || strings.HasPrefix(cmd, "rag") {
		if strings.TrimSpace(req.RoomID) == "" {
			return "", ErrRAGRoomRequired
		}
		t, err = s.runtime.SubmitRAG(tasksvc.SubmitRAGRequest{
			Priority:           tasksvc.PriorityNormal,
			RoomID:             strings.TrimSpace(req.RoomID),
			SegmentGapSeconds:  ragSegmentGapSeconds,
			MaxCharsPerSegment: ragMaxCharsPerSegment,
			MaxMessagesPerSeg:  ragMaxMessagesPerSeg,
			OverlapMessages:    ragOverlapMessages,
		})
	} else {
		return "", ErrUnsupportedCommand
	}
	if err != nil {
		return "", err
	}
	s.registerCallback(t.ID, cb)
	return t.ID, nil
}

func (s *Service) registerCallback(taskID string, cb TaskCallback) {
	if cb == nil || strings.TrimSpace(taskID) == "" {
		return
	}
	s.callbackMu.Lock()
	s.callbacks[taskID] = cb
	s.callbackMu.Unlock()
}

func (s *Service) dispatchCallback(ctx context.Context, doneTask *tasksvc.Task) {
	if doneTask == nil {
		return
	}
	s.callbackMu.Lock()
	cb := s.callbacks[doneTask.ID]
	delete(s.callbacks, doneTask.ID)
	s.callbackMu.Unlock()
	if cb != nil {
		final, payload := extractCallbackResult(doneTask)
		cb(ctx, doneTask.Clone(), final, payload)
	}
}

func extractCallbackResult(doneTask *tasksvc.Task) (string, map[string]interface{}) {
	if doneTask == nil {
		return "", nil
	}
	final := ""
	if doneTask.Result.Final != nil {
		final = strings.TrimSpace(*doneTask.Result.Final)
	}
	if final == "" && doneTask.Result.Text != nil {
		final = strings.TrimSpace(*doneTask.Result.Text)
	}
	payload := clonePayloadMap(doneTask.Result.Payload)
	reportText := ""
	if doneTask.Result.Text != nil {
		reportText = strings.TrimSpace(*doneTask.Result.Text)
	}
	if reportText == "" {
		reportText = strings.TrimSpace(doneTask.ErrorMessage)
	}
	reportFinal, reportMemory := splitAgentReport(reportText)
	if final == "" {
		final = reportFinal
	}
	if strings.TrimSpace(reportMemory) != "" {
		if payload == nil {
			payload = make(map[string]interface{})
		}
		if _, exists := payload["memory"]; !exists {
			payload["memory"] = strings.TrimSpace(reportMemory)
		}
	}
	if final == "" {
		final = strings.TrimSpace(doneTask.ErrorMessage)
	}
	return final, payload
}

func splitAgentReport(text string) (string, string) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", ""
	}
	const memoryMarker = "工具调用记录："
	const finalMarker = "最终结果："
	const errorMarker = "错误报告："
	if idx := strings.LastIndex(trimmed, finalMarker); idx >= 0 {
		memory := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(trimmed[:idx]), memoryMarker))
		final := strings.TrimSpace(trimmed[idx+len(finalMarker):])
		if final == "" {
			final = trimmed
		}
		return final, memory
	}
	if idx := strings.LastIndex(trimmed, errorMarker); idx >= 0 {
		memory := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(trimmed[:idx]), memoryMarker))
		final := strings.TrimSpace(trimmed[idx+len(errorMarker):])
		if final == "" {
			final = trimmed
		}
		return final, memory
	}
	return trimmed, ""
}

func clonePayloadMap(src map[string]interface{}) map[string]interface{} {
	if src == nil {
		return nil
	}
	b, err := json.Marshal(src)
	if err != nil {
		dst := make(map[string]interface{}, len(src))
		for k, v := range src {
			dst[k] = v
		}
		return dst
	}
	var dst map[string]interface{}
	if err := json.Unmarshal(b, &dst); err != nil {
		dst = make(map[string]interface{}, len(src))
		for k, v := range src {
			dst[k] = v
		}
	}
	return dst
}

func parseSummaryCount(cmd string) (int, error) {
	parts := strings.Fields(strings.TrimSpace(cmd))
	if len(parts) < 2 {
		return 0, ErrSummaryCountRequired
	}
	n, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || n <= 0 {
		return 0, ErrSummaryCountInvalid
	}
	if n > summaryCountMax {
		return 0, ErrSummaryCountTooLarge
	}
	return n, nil
}

func parseAgentGoal(cmd string) (string, error) {
	trimmed := strings.TrimSpace(cmd)
	if trimmed == "" {
		return "", ErrAgentGoalRequired
	}
	if strings.HasPrefix(trimmed, "agent") {
		goal := strings.TrimSpace(strings.TrimPrefix(trimmed, "agent"))
		if goal == "" {
			return "", ErrAgentGoalRequired
		}
		return goal, nil
	}
	if strings.HasPrefix(trimmed, "智能体") {
		goal := strings.TrimSpace(strings.TrimPrefix(trimmed, "智能体"))
		if goal == "" {
			return "", ErrAgentGoalRequired
		}
		return goal, nil
	}
	return "", ErrAgentGoalRequired
}

func parseRAGSearchQuery(cmd string) (string, int, string, error) {
	trimmed := strings.TrimSpace(cmd)
	if !strings.HasPrefix(trimmed, "rag检索") {
		return "", 0, "", ErrRAGSearchQueryRequired
	}
	body := strings.TrimSpace(strings.TrimPrefix(trimmed, "rag检索"))
	if body == "" {
		return "", 0, "", ErrRAGSearchQueryRequired
	}
	topK := ragSearchTopKDefault
	vectorMode := "raw"
	parts := strings.Fields(body)
	consumed := 0
	for consumed < len(parts) && consumed < 2 {
		token := strings.ToLower(strings.TrimSpace(parts[consumed]))
		if token == "" {
			consumed++
			continue
		}
		if parsedTopK, err := strconv.Atoi(token); err == nil {
			if parsedTopK <= 0 {
				return "", 0, "", ErrRAGSearchTopKInvalid
			}
			if parsedTopK > ragSearchTopKMax {
				return "", 0, "", ErrRAGSearchTopKTooLarge
			}
			topK = parsedTopK
			consumed++
			continue
		}
		switch token {
		case "raw", "summary":
			vectorMode = token
			consumed++
			continue
		case "raw:", "summary:":
			vectorMode = strings.TrimSuffix(token, ":")
			consumed++
			continue
		}
		break
	}
	if consumed > 0 {
		body = strings.TrimSpace(strings.Join(parts[consumed:], " "))
	}
	query := strings.TrimSpace(body)
	if query == "" {
		return "", 0, "", ErrRAGSearchQueryRequired
	}
	modeLower := strings.ToLower(strings.TrimSpace(vectorMode))
	if modeLower == "" {
		modeLower = "raw"
	}
	if modeLower != "raw" && modeLower != "summary" {
		return "", 0, "", ErrRAGSearchVectorInvalid
	}
	return query, topK, modeLower, nil
}

func parseRAGMemorySequenceRange(cmd string) (int64, int64, error) {
	parts := strings.Fields(strings.TrimSpace(cmd))
	if len(parts) < 3 {
		return 0, 0, ErrRAGMemorySequenceRangeRequired
	}
	startSeq, startErr := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
	endSeq, endErr := strconv.ParseInt(strings.TrimSpace(parts[2]), 10, 64)
	if startErr != nil || endErr != nil || startSeq <= 0 || endSeq <= 0 || startSeq > endSeq {
		return 0, 0, ErrRAGMemorySequenceRangeInvalid
	}
	return startSeq, endSeq, nil
}
