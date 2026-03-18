package task

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"ququchat/internal/models"
	tasksvc "ququchat/internal/service/task"
)

type ragSegmentLine struct {
	seq       int64
	created   time.Time
	formatted string
}

var ragSpaceRe = regexp.MustCompile(`\s+`)
var ragNoiseOnlyRe = regexp.MustCompile(`^[\p{P}\p{S}\s]+$`)
var ragStopPhraseFilePath = filepath.Join("internal", "config", "rag_stop_phrases.yaml")

func (s *Service) ExecuteRAG(ctx context.Context, payload *tasksvc.RAGPayload) (tasksvc.Result, error) {
	if s == nil || s.db == nil {
		return tasksvc.Result{}, ErrServiceNotInitialized
	}
	if payload == nil || strings.TrimSpace(payload.RoomID) == "" {
		return tasksvc.Result{}, tasksvc.ErrInvalidRAGRoomID
	}
	roomID := strings.TrimSpace(payload.RoomID)
	gapSeconds := payload.SegmentGapSeconds
	if gapSeconds <= 0 {
		gapSeconds = ragSegmentGapSeconds
	}
	maxChars := payload.MaxCharsPerSegment
	if maxChars <= 0 {
		maxChars = ragMaxCharsPerSegment
	}
	maxMessages := payload.MaxMessagesPerSeg
	if maxMessages <= 0 {
		maxMessages = ragMaxMessagesPerSeg
	}
	overlapMessages := payload.OverlapMessages
	if overlapMessages < 0 {
		overlapMessages = 0
	}
	if overlapMessages > 3 {
		overlapMessages = 3
	}
	stopPhrases := loadRAGStopPhrases()
	var latest models.Message
	if err := s.db.Where("room_id = ?", roomID).Order("sequence_id desc").Take(&latest).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			final := "当前群聊没有可向量化消息"
			return tasksvc.Result{
				Text:  &final,
				Final: &final,
				Payload: map[string]interface{}{
					"room_id": roomID,
					"status":  "empty",
				},
			}, nil
		}
		return tasksvc.Result{}, err
	}
	lastIndexedSeq := payload.MinMessageSequenceID - 1
	if lastIndexedSeq < 0 {
		lastIndexedSeq = 0
	}
	var cursor models.ChatSegmentCursor
	cursorErr := s.db.Where("room_id = ?", roomID).Take(&cursor).Error
	if cursorErr == nil && cursor.LastIndexedSeq > lastIndexedSeq {
		lastIndexedSeq = cursor.LastIndexedSeq
	}
	if cursorErr != nil && !errors.Is(cursorErr, gorm.ErrRecordNotFound) {
		return tasksvc.Result{}, cursorErr
	}
	if lastIndexedSeq >= latest.SequenceID {
		final := fmt.Sprintf("群聊 %s 已完成向量化，无需更新", roomID)
		return tasksvc.Result{
			Text:  &final,
			Final: &final,
			Payload: map[string]interface{}{
				"room_id":          roomID,
				"latest_seq":       latest.SequenceID,
				"last_indexed_seq": lastIndexedSeq,
				"indexed_segments": 0,
				"status":           "up_to_date",
			},
		}, nil
	}
	query := s.db.Where("room_id = ? AND sequence_id > ?", roomID, lastIndexedSeq)
	if payload.MinMessageSequenceID > 0 {
		query = query.Where("sequence_id >= ?", payload.MinMessageSequenceID)
	}
	query = query.Where("content_type IN ?", []models.ContentType{
		models.ContentTypeText,
		models.ContentTypeImage,
		models.ContentTypeFile,
	})
	var messages []models.Message
	if err := query.Order("sequence_id asc").Find(&messages).Error; err != nil {
		return tasksvc.Result{}, err
	}
	senderNames, err := s.loadRAGSenderNames(roomID, messages)
	if err != nil {
		return tasksvc.Result{}, err
	}
	lines := make([]ragSegmentLine, 0, maxMessages)
	segments := make([]models.ChatSegment, 0)
	flush := func() {
		if len(lines) == 0 {
			return
		}
		start := lines[0]
		end := lines[len(lines)-1]
		builder := strings.Builder{}
		for i, line := range lines {
			if i > 0 {
				builder.WriteString("\n")
			}
			builder.WriteString(line.formatted)
		}
		rawText := strings.TrimSpace(builder.String())
		if rawText == "" {
			return
		}
		sum := sha256.Sum256([]byte(rawText))
		segmentID := fmt.Sprintf("%s-%d-%d", roomID, start.seq, end.seq)
		pointID := fmt.Sprintf("seg_%s_%d_%d", roomID, start.seq, end.seq)
		segments = append(segments, models.ChatSegment{
			ID:            segmentID,
			RoomID:        roomID,
			SegmentID:     segmentID,
			StartSeq:      start.seq,
			EndSeq:        end.seq,
			StartAt:       start.created,
			EndAt:         end.created,
			MessageCount:  len(lines),
			RawText:       rawText,
			RawTextHash:   hex.EncodeToString(sum[:]),
			QdrantPointID: pointID,
			SummaryReady:  false,
		})
	}
	segmentGap := time.Duration(gapSeconds) * time.Second
	for _, msg := range messages {
		formatted, ok := formatRAGMessageLine(msg, stopPhrases, senderNames)
		if !ok {
			continue
		}
		if len(lines) > 0 {
			last := lines[len(lines)-1]
			needFlushByGap := msg.CreatedAt.Sub(last.created) > segmentGap
			chars := 0
			for _, line := range lines {
				chars += len(line.formatted)
			}
			needFlushByChar := chars+len(formatted) > maxChars
			needFlushByMsgCount := len(lines) >= maxMessages
			if needFlushByGap || needFlushByChar || needFlushByMsgCount {
				flush()
				if overlapMessages > 0 && len(lines) > 0 {
					start := len(lines) - overlapMessages
					if start < 0 {
						start = 0
					}
					lines = append([]ragSegmentLine(nil), lines[start:]...)
				} else {
					lines = lines[:0]
				}
			}
		}
		lines = append(lines, ragSegmentLine{
			seq:       msg.SequenceID,
			created:   msg.CreatedAt,
			formatted: formatted,
		})
	}
	flush()
	if len(segments) == 0 {
		now := time.Now()
		nextCursor := models.ChatSegmentCursor{
			RoomID:         roomID,
			LastIndexedSeq: latest.SequenceID,
			UpdatedAt:      now,
		}
		if err := s.db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "room_id"}},
			DoUpdates: clause.Assignments(map[string]interface{}{"last_indexed_seq": nextCursor.LastIndexedSeq, "updated_at": nextCursor.UpdatedAt}),
		}).Create(&nextCursor).Error; err != nil {
			return tasksvc.Result{}, err
		}
		final := fmt.Sprintf("群聊 %s 没有可用于 RAG 的有效消息", roomID)
		return tasksvc.Result{
			Text:  &final,
			Final: &final,
			Payload: map[string]interface{}{
				"room_id":          roomID,
				"latest_seq":       latest.SequenceID,
				"last_indexed_seq": nextCursor.LastIndexedSeq,
				"indexed_segments": 0,
				"status":           "no_valid_messages",
			},
		}, nil
	}
	now := time.Now()
	for i := range segments {
		segments[i].CreatedAt = now
		segments[i].UpdatedAt = now
	}
	if err := s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "segment_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"start_seq", "end_seq", "start_at", "end_at", "message_count", "raw_text", "raw_text_hash", "qdrant_point_id", "updated_at"}),
	}).Create(&segments).Error; err != nil {
		return tasksvc.Result{}, err
	}
	taskSegments := make([]tasksvc.RAGSegment, 0, len(segments))
	for _, seg := range segments {
		taskSegments = append(taskSegments, tasksvc.RAGSegment{
			SegmentID:    seg.SegmentID,
			RoomID:       seg.RoomID,
			StartSeq:     seg.StartSeq,
			EndSeq:       seg.EndSeq,
			StartUnixSec: seg.StartAt.Unix(),
			EndUnixSec:   seg.EndAt.Unix(),
			MessageCount: seg.MessageCount,
			RawText:      seg.RawText,
			TextPreview:  previewText(seg.RawText, 80),
		})
	}
	if err := s.upsertRAGPoints(ctx, taskSegments); err != nil {
		return tasksvc.Result{}, err
	}
	nextCursor := models.ChatSegmentCursor{
		RoomID:         roomID,
		LastIndexedSeq: latest.SequenceID,
		UpdatedAt:      now,
	}
	if err := s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "room_id"}},
		DoUpdates: clause.Assignments(map[string]interface{}{"last_indexed_seq": nextCursor.LastIndexedSeq, "updated_at": nextCursor.UpdatedAt}),
	}).Create(&nextCursor).Error; err != nil {
		return tasksvc.Result{}, err
	}
	final := fmt.Sprintf("群聊 %s RAG 索引完成，新增分段 %d 个", roomID, len(taskSegments))
	return tasksvc.Result{
		Text:  &final,
		Final: &final,
		Payload: map[string]interface{}{
			"room_id":              roomID,
			"latest_seq":           latest.SequenceID,
			"previous_indexed_seq": lastIndexedSeq,
			"indexed_segments":     len(taskSegments),
			"overlap_messages":     overlapMessages,
			"segment_gap_seconds":  gapSeconds,
		},
	}, nil
}

func formatRAGMessageLine(msg models.Message, stopPhrases map[string]struct{}, senderNames map[string]string) (string, bool) {
	sender := "系统"
	if msg.SenderID != nil && strings.TrimSpace(*msg.SenderID) != "" {
		senderID := strings.TrimSpace(*msg.SenderID)
		if name, ok := senderNames[senderID]; ok && strings.TrimSpace(name) != "" {
			sender = strings.TrimSpace(name)
		} else {
			sender = senderID
		}
	}
	text := ""
	switch msg.ContentType {
	case models.ContentTypeText:
		if msg.ContentText == nil {
			return "", false
		}
		cleaned, ok := cleanRAGText(*msg.ContentText, stopPhrases)
		if !ok {
			return "", false
		}
		text = cleaned
	case models.ContentTypeImage:
		text = "[图片]"
	case models.ContentTypeFile:
		text = "[文件]"
	default:
		return "", false
	}
	return fmt.Sprintf("%s说：%s", sender, text), true
}

func (s *Service) loadRAGSenderNames(roomID string, messages []models.Message) (map[string]string, error) {
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
	var members []models.RoomMember
	if err := s.db.Where("room_id = ? AND user_id IN ?", roomID, userIDs).Find(&members).Error; err != nil {
		return nil, err
	}
	for _, member := range members {
		if member.NicknameInRoom == nil {
			continue
		}
		nickname := strings.TrimSpace(*member.NicknameInRoom)
		if nickname == "" {
			continue
		}
		names[member.UserID] = nickname
	}
	missingUserIDs := make([]string, 0)
	for _, userID := range userIDs {
		if _, ok := names[userID]; ok {
			continue
		}
		missingUserIDs = append(missingUserIDs, userID)
	}
	if len(missingUserIDs) == 0 {
		return names, nil
	}
	var users []models.User
	if err := s.db.Where("id IN ?", missingUserIDs).Find(&users).Error; err != nil {
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

func cleanRAGText(raw string, stopPhrases map[string]struct{}) (string, bool) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return "", false
	}
	if strings.HasPrefix(text, "\\") {
		return "", false
	}
	text = ragSpaceRe.ReplaceAllString(text, " ")
	if text == "" {
		return "", false
	}
	if ragNoiseOnlyRe.MatchString(text) {
		return "", false
	}
	lower := strings.ToLower(text)
	if _, ok := stopPhrases[lower]; ok {
		return "", false
	}
	if isSingleRuneRepeat(lower, 3) {
		return "", false
	}
	if len([]rune(text)) <= 1 {
		return "", false
	}
	return text, true
}

func isSingleRuneRepeat(text string, minRepeat int) bool {
	runes := []rune(text)
	if len(runes) < minRepeat {
		return false
	}
	base := runes[0]
	for _, r := range runes[1:] {
		if r != base {
			return false
		}
	}
	return true
}

type ragStopPhraseConfig struct {
	StopPhrases []string `yaml:"stop_phrases"`
}

func loadRAGStopPhrases() map[string]struct{} {
	b, err := os.ReadFile(ragStopPhraseFilePath)
	if err != nil {
		return map[string]struct{}{}
	}
	var cfg ragStopPhraseConfig
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return map[string]struct{}{}
	}
	set := make(map[string]struct{}, len(cfg.StopPhrases))
	for _, item := range cfg.StopPhrases {
		phrase := strings.ToLower(strings.TrimSpace(item))
		if phrase == "" {
			continue
		}
		set[phrase] = struct{}{}
	}
	if len(set) == 0 {
		return map[string]struct{}{}
	}
	return set
}

func previewText(text string, maxLen int) string {
	trimmed := strings.TrimSpace(text)
	if maxLen <= 0 {
		maxLen = 80
	}
	runes := []rune(trimmed)
	if len(runes) <= maxLen {
		return trimmed
	}
	return string(runes[:maxLen])
}

func (s *Service) upsertRAGPoints(ctx context.Context, segments []tasksvc.RAGSegment) error {
	_ = ctx
	_ = segments
	return nil
}
