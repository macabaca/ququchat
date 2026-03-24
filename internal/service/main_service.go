package taskservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"ququchat/internal/models"
	tasksvc "ququchat/internal/service/task"
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
const ragSegmentGapSeconds = 300
const ragMaxCharsPerSegment = 2000
const ragMaxMessagesPerSeg = 30
const ragOverlapMessages = 3
const ragSearchTopKDefault = 5
const ragSearchTopKMax = 20

type MainService struct {
	db                          *gorm.DB
	producer                    *Producer
	commandPriorityRules        []CommandPriorityRule
	doneEventURL                string
	doneEventQueue              string
	doneConsumeRetryMaxAttempts int
	doneConsumeRetryDelay       time.Duration
	doneConsumePrefetch         int
	doneConsumerMu              sync.Mutex
	doneConsumerUp              bool
	doneConsumerErr             error
}

type CommandPriorityRule struct {
	Prefix   string
	Priority tasksvc.Priority
}

type ServiceOptions struct {
	CommandPriorityRules []CommandPriorityRule
}

type SubmitCommandRequest struct {
	UserID   string
	RoomID   string
	Content  string
	Priority tasksvc.Priority
}

func NewMainService(db *gorm.DB, opts tasksvc.RuntimeOptions) *MainService {
	return NewMainServiceWithOptions(db, opts, ServiceOptions{})
}

func NewMainServiceWithOptions(db *gorm.DB, opts tasksvc.RuntimeOptions, svcOpts ServiceOptions) *MainService {
	doneEventURL := strings.TrimSpace(opts.DoneEventRabbitMQURL)
	if doneEventURL == "" {
		doneEventURL = strings.TrimSpace(opts.QueueRabbitMQURL)
	}
	doneEventQueue := strings.TrimSpace(opts.DoneEventQueueName)
	if doneEventQueue == "" {
		doneEventQueue = "ququchat.task.done"
	}
	return &MainService{
		db:                          db,
		producer:                    NewProducer(db, opts),
		commandPriorityRules:        normalizeCommandPriorityRules(svcOpts.CommandPriorityRules),
		doneEventURL:                doneEventURL,
		doneEventQueue:              doneEventQueue,
		doneConsumeRetryMaxAttempts: normalizeRetryMaxAttempts(opts.DoneEventConsumeRetryMaxAttempts),
		doneConsumeRetryDelay:       normalizeRetryDelay(opts.DoneEventConsumeRetryDelay),
		doneConsumePrefetch:         normalizePrefetch(opts.WorkerSize),
	}
}

func (s *MainService) SubmitCommand(req SubmitCommandRequest) (string, error) {
	if s == nil || s.producer == nil {
		return "", ErrServiceNotInitialized
	}
	requestID := BuildWSCommandRequestID(req.UserID, req.RoomID)
	raw := strings.TrimSpace(req.Content)
	if raw == "" {
		return "", ErrCommandRequired
	}
	if !strings.HasPrefix(raw, "\\") {
		return "", ErrUnsupportedCommand
	}
	cmd := strings.TrimSpace(strings.TrimPrefix(raw, "\\"))
	priority := s.matchCommandPriority(cmd, req.Priority)
	var (
		t   *tasksvc.Task
		err error
	)
	if strings.HasPrefix(cmd, "task:fake_llm") {
		prompt := strings.TrimSpace(strings.TrimPrefix(cmd, "task:fake_llm"))
		if prompt == "" {
			prompt = cmd
		}
		t, err = s.producer.SubmitFakeLLM(tasksvc.SubmitFakeLLMRequest{
			RequestID: requestID,
			Priority:  priority,
			Prompt:    prompt,
			SleepMs:   800,
		})
	} else if strings.HasPrefix(cmd, "task:llm") {
		prompt := strings.TrimSpace(strings.TrimPrefix(cmd, "task:llm"))
		if prompt == "" {
			prompt = cmd
		}
		t, err = s.producer.SubmitLLM(tasksvc.SubmitLLMRequest{
			RequestID: requestID,
			Priority:  priority,
			Prompt:    prompt,
		})
	} else if strings.HasPrefix(cmd, "对话") {
		prompt := strings.TrimSpace(strings.TrimPrefix(cmd, "对话"))
		if prompt == "" {
			return "", ErrCommandRequired
		}
		t, err = s.producer.SubmitLLM(tasksvc.SubmitLLMRequest{
			RequestID: requestID,
			Priority:  priority,
			Prompt:    prompt,
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
		t, err = s.producer.SubmitSummary(tasksvc.SubmitSummaryRequest{
			RequestID: requestID,
			Priority:  priority,
			Prompt:    prompt,
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
		t, err = s.producer.SubmitAgent(tasksvc.SubmitAgentRequest{
			RequestID:      requestID,
			Priority:       priority,
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
		t, err = s.producer.SubmitRAGSearch(tasksvc.SubmitRAGSearchRequest{
			RequestID: requestID,
			Priority:  priority,
			RoomID:    strings.TrimSpace(req.RoomID),
			Query:     ragQuery,
			TopK:      topK,
			Vector:    vectorMode,
		})
	} else if strings.HasPrefix(cmd, "添加记忆") {
		if strings.TrimSpace(req.RoomID) == "" {
			return "", ErrRAGRoomRequired
		}
		startSeq, endSeq, parseErr := parseRAGMemorySequenceRange(cmd)
		if parseErr != nil {
			return "", parseErr
		}
		t, err = s.producer.SubmitRAGAddMemory(tasksvc.SubmitRAGAddMemoryRequest{
			RequestID:          requestID,
			Priority:           priority,
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
		t, err = s.producer.SubmitRAG(tasksvc.SubmitRAGRequest{
			RequestID:          requestID,
			Priority:           priority,
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
	return t.ID, nil
}

func (s *MainService) StartDoneEventConsumer(ctx context.Context, handler DoneEventHandler) error {
	if s == nil {
		return ErrServiceNotInitialized
	}
	s.doneConsumerMu.Lock()
	defer s.doneConsumerMu.Unlock()
	if s.doneConsumerUp {
		return s.doneConsumerErr
	}
	if strings.TrimSpace(s.doneEventURL) == "" || strings.TrimSpace(s.doneEventQueue) == "" {
		return nil
	}
	dlqConsumer, err := NewRabbitMQDoneEventDeadLetterConsumer(RabbitMQDoneEventDeadLetterConsumerOptions{
		URL: s.doneEventURL,
		DB:  s.db,
	})
	if err != nil {
		s.doneConsumerErr = err
		return err
	}
	consumer, err := NewRabbitMQDoneEventConsumer(RabbitMQDoneEventConsumerOptions{
		URL:              s.doneEventURL,
		QueueName:        s.doneEventQueue,
		Prefetch:         s.doneConsumePrefetch,
		RetryMaxAttempts: s.doneConsumeRetryMaxAttempts,
		RetryDelay:       s.doneConsumeRetryDelay,
		OnMaxRetry:       s.handleDoneEventConsumeExhausted,
	})
	if err != nil {
		s.doneConsumerErr = err
		return err
	}
	s.doneConsumerUp = true
	go func() {
		if runErr := dlqConsumer.Start(ctx); runErr != nil {
			log.Printf("done-event dlq consumer exited: %v", runErr)
		}
	}()
	go func() {
		runErr := consumer.Start(ctx, handler)
		s.doneConsumerMu.Lock()
		s.doneConsumerErr = runErr
		s.doneConsumerUp = false
		s.doneConsumerMu.Unlock()
	}()
	return s.doneConsumerErr
}

func (s *MainService) handleDoneEventConsumeExhausted(ctx context.Context, event DoneEvent) error {
	if s == nil {
		return nil
	}
	failureErr := fmt.Errorf("done event consume exhausted retries task=%s", event.TaskID)
	if err := s.markTaskFailed(event.TaskID, failureErr.Error()); err != nil {
		return err
	}
	return nil
}

func (s *MainService) markTaskFailed(taskID string, message string) error {
	trimmedID := strings.TrimSpace(taskID)
	if s == nil || trimmedID == "" {
		return nil
	}
	if s.producer == nil {
		return ErrServiceNotInitialized
	}
	_, err := s.producer.MarkFailed(trimmedID, message)
	return err
}

func (s *MainService) matchCommandPriority(cmd string, fallback tasksvc.Priority) tasksvc.Priority {
	if s == nil {
		return normalizePriority(fallback)
	}
	for _, item := range s.commandPriorityRules {
		if strings.HasPrefix(cmd, item.Prefix) {
			return item.Priority
		}
	}
	return normalizePriority(fallback)
}

func (s *MainService) loadAgentRecentMessages(roomID string, limit int) ([]string, error) {
	if s == nil || s.db == nil {
		return nil, ErrServiceNotInitialized
	}
	if limit <= 0 {
		limit = agentRecentMessageLimit
	}
	queryLimit := limit * 3
	if queryLimit < 30 {
		queryLimit = 30
	}
	if queryLimit > 300 {
		queryLimit = 300
	}
	var raw []models.Message
	if err := s.db.
		Where("room_id = ? AND content_type IN ?", roomID, []models.ContentType{
			models.ContentTypeText,
			models.ContentTypeImage,
			models.ContentTypeFile,
		}).
		Order("sequence_id desc").
		Limit(queryLimit).
		Find(&raw).Error; err != nil {
		return nil, err
	}
	senderNames, err := s.loadSenderNames(raw)
	if err != nil {
		return nil, err
	}
	lines := make([]string, 0, limit)
	for _, m := range raw {
		text := ""
		switch m.ContentType {
		case models.ContentTypeText:
			if m.ContentText == nil {
				continue
			}
			text = strings.TrimSpace(*m.ContentText)
			if strings.HasPrefix(text, "\\") {
				continue
			}
		case models.ContentTypeImage:
			text = "[图片]"
		case models.ContentTypeFile:
			text = "[文件]"
		default:
			continue
		}
		if text == "" {
			continue
		}
		sender := "系统"
		if m.SenderID != nil && strings.TrimSpace(*m.SenderID) != "" {
			senderID := strings.TrimSpace(*m.SenderID)
			if name, ok := senderNames[senderID]; ok && strings.TrimSpace(name) != "" {
				sender = strings.TrimSpace(name)
			} else {
				sender = senderID
			}
		}
		lines = append(lines, fmt.Sprintf("[%s] %s", sender, text))
		if len(lines) >= limit {
			break
		}
	}
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}
	return lines, nil
}

func (s *MainService) buildSummaryPrompt(roomID string, count int) (string, error) {
	if s == nil || s.db == nil {
		return "", ErrServiceNotInitialized
	}
	queryLimit := count * 3
	if queryLimit < 30 {
		queryLimit = 30
	}
	if queryLimit > 300 {
		queryLimit = 300
	}
	var raw []models.Message
	if err := s.db.
		Where("room_id = ? AND content_type IN ?", roomID, []models.ContentType{
			models.ContentTypeText,
			models.ContentTypeImage,
			models.ContentTypeFile,
		}).
		Order("sequence_id desc").
		Limit(queryLimit).
		Find(&raw).Error; err != nil {
		return "", err
	}
	senderNames, err := s.loadSenderNames(raw)
	if err != nil {
		return "", err
	}
	lines := make([]string, 0, count)
	for _, m := range raw {
		text := ""
		switch m.ContentType {
		case models.ContentTypeText:
			if m.ContentText == nil {
				continue
			}
			text = strings.TrimSpace(*m.ContentText)
			if strings.HasPrefix(text, "\\") {
				continue
			}
		case models.ContentTypeImage:
			text = "[图片]"
		case models.ContentTypeFile:
			text = "[文件]"
		default:
			continue
		}
		if text == "" {
			continue
		}
		sender := "系统"
		if m.SenderID != nil && strings.TrimSpace(*m.SenderID) != "" {
			senderID := strings.TrimSpace(*m.SenderID)
			if name, ok := senderNames[senderID]; ok && strings.TrimSpace(name) != "" {
				sender = strings.TrimSpace(name)
			} else {
				sender = senderID
			}
		}
		lines = append(lines, fmt.Sprintf("[%s] %s", sender, text))
		if len(lines) >= count {
			break
		}
	}
	if len(lines) == 0 {
		return "", ErrSummarySourceEmpty
	}
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}
	builder := strings.Builder{}
	builder.WriteString("请基于以下群聊消息生成简洁摘要，输出要点列表，不要编造内容：\n")
	for i, line := range lines {
		builder.WriteString(strconv.Itoa(i + 1))
		builder.WriteString(". ")
		builder.WriteString(line)
		builder.WriteString("\n")
	}
	return builder.String(), nil
}

func (s *MainService) loadSenderNames(messages []models.Message) (map[string]string, error) {
	names := make(map[string]string)
	userIDSet := make(map[string]struct{})
	userIDs := make([]string, 0)
	for _, m := range messages {
		if m.SenderID == nil {
			continue
		}
		userID := strings.TrimSpace(*m.SenderID)
		if userID == "" {
			continue
		}
		if _, exists := userIDSet[userID]; exists {
			continue
		}
		userIDSet[userID] = struct{}{}
		userIDs = append(userIDs, userID)
	}
	if len(userIDs) == 0 {
		return names, nil
	}
	var users []models.User
	if err := s.db.Where("id IN ?", userIDs).Find(&users).Error; err != nil {
		return nil, err
	}
	for _, u := range users {
		display := strings.TrimSpace(u.Username)
		if u.DisplayName != nil && strings.TrimSpace(*u.DisplayName) != "" {
			display = strings.TrimSpace(*u.DisplayName)
		}
		if display != "" {
			names[u.ID] = display
		}
	}
	return names, nil
}

func extractDoneResult(doneTask *tasksvc.Task) (string, map[string]interface{}) {
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

func normalizePriority(p tasksvc.Priority) tasksvc.Priority {
	switch p {
	case tasksvc.PriorityHigh, tasksvc.PriorityNormal, tasksvc.PriorityLow:
		return p
	default:
		return tasksvc.PriorityNormal
	}
}

func normalizeCommandPriorityRules(rules []CommandPriorityRule) []CommandPriorityRule {
	if len(rules) == 0 {
		return nil
	}
	normalized := make([]CommandPriorityRule, 0, len(rules))
	for _, item := range rules {
		prefix := strings.TrimSpace(item.Prefix)
		if prefix == "" {
			continue
		}
		normalized = append(normalized, CommandPriorityRule{
			Prefix:   prefix,
			Priority: normalizePriority(item.Priority),
		})
	}
	return normalized
}
