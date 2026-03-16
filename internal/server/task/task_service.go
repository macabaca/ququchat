package task

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"gorm.io/gorm"

	"ququchat/internal/models"
	tasksvc "ququchat/internal/service/task"
)

var ErrServiceNotInitialized = errors.New("task service not initialized")
var ErrCommandRequired = errors.New("command required")
var ErrUnsupportedCommand = errors.New("unsupported command")
var ErrSummaryCountRequired = errors.New("summary count is required")
var ErrSummaryCountInvalid = errors.New("summary count must be a positive integer")
var ErrSummaryCountTooLarge = errors.New("summary count is too large, 最大1000")
var ErrSummaryRoomRequired = errors.New("summary room id is required")
var ErrSummarySourceEmpty = errors.New("no messages available for summary")

const summaryCountMax = 1000

type TaskCallback func(ctx context.Context, doneTask *tasksvc.Task)

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
		cb(ctx, doneTask.Clone())
	}
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

func (s *Service) buildSummaryPrompt(roomID string, count int) (string, error) {
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

func (s *Service) loadSenderNames(messages []models.Message) (map[string]string, error) {
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
