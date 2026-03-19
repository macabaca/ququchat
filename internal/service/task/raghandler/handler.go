package raghandler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"ququchat/internal/models"
	tasksvc "ququchat/internal/service/task"
)

const defaultSegmentGapSeconds = 300
const defaultMaxCharsPerSegment = 2000
const defaultMaxMessagesPerSeg = 50
const defaultMaxSummaryChars = 2000
const defaultMinSummaryMessageCount = 5
const defaultRAGSearchTopK = 5
const maxRAGSearchTopK = 20
const ragSearchVectorRaw = "raw"
const ragSearchVectorSummary = "summary"

type Handler struct {
	db                    *gorm.DB
	llmClient             tasksvc.LLMClient
	embeddingProvider     tasksvc.EmbeddingProvider
	vectorStore           tasksvc.VectorStore
	embeddingModelRaw     string
	embeddingModelSummary string
	summaryVectorDim      int
}

type Options struct {
	DB                    *gorm.DB
	LLMClient             tasksvc.LLMClient
	EmbeddingProvider     tasksvc.EmbeddingProvider
	VectorStore           tasksvc.VectorStore
	EmbeddingModelRaw     string
	EmbeddingModelSummary string
	SummaryVectorDim      int
}

type ragSegmentLine struct {
	seq       int64
	created   time.Time
	formatted string
}

type ragStopPhraseConfig struct {
	StopPhrases []string `yaml:"stop_phrases"`
}

var ragSpaceRe = regexp.MustCompile(`\s+`)
var ragNoiseOnlyRe = regexp.MustCompile(`^[\p{P}\p{S}\s]+$`)
var ragStopPhraseFilePath = filepath.Join("internal", "config", "rag_stop_phrases.yaml")

func New(opts Options) *Handler {
	return &Handler{
		db:                    opts.DB,
		llmClient:             opts.LLMClient,
		embeddingProvider:     opts.EmbeddingProvider,
		vectorStore:           opts.VectorStore,
		embeddingModelRaw:     strings.TrimSpace(opts.EmbeddingModelRaw),
		embeddingModelSummary: strings.TrimSpace(opts.EmbeddingModelSummary),
		summaryVectorDim:      opts.SummaryVectorDim,
	}
}

func (h *Handler) ExecuteRAG(ctx context.Context, payload *tasksvc.RAGPayload) (tasksvc.Result, error) {
	if h == nil || h.db == nil {
		return tasksvc.Result{}, errors.New("rag handler db is not initialized")
	}
	if payload == nil || strings.TrimSpace(payload.RoomID) == "" {
		return tasksvc.Result{}, tasksvc.ErrInvalidRAGRoomID
	}
	roomID := strings.TrimSpace(payload.RoomID)
	gapSeconds := payload.SegmentGapSeconds
	if gapSeconds <= 0 {
		gapSeconds = defaultSegmentGapSeconds
	}
	maxChars := payload.MaxCharsPerSegment
	if maxChars <= 0 {
		maxChars = defaultMaxCharsPerSegment
	}
	maxMessages := payload.MaxMessagesPerSeg
	if maxMessages <= 0 {
		maxMessages = defaultMaxMessagesPerSeg
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
	if err := h.db.Where("room_id = ?", roomID).Order("sequence_id desc").Take(&latest).Error; err != nil {
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
	cursorErr := h.db.Where("room_id = ?", roomID).Take(&cursor).Error
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
	query := h.db.Where("room_id = ? AND sequence_id > ?", roomID, lastIndexedSeq)
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
	senderNames, err := h.loadRAGSenderNames(roomID, messages)
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
		pointID := buildQdrantPointID(segmentID)
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
	segments, skippedByRange, err := h.filterExistingSegmentsBySeqRange(roomID, segments)
	if err != nil {
		return tasksvc.Result{}, err
	}
	if len(segments) == 0 {
		now := time.Now()
		nextCursor := models.ChatSegmentCursor{
			RoomID:         roomID,
			LastIndexedSeq: latest.SequenceID,
			UpdatedAt:      now,
		}
		if err := h.db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "room_id"}},
			DoUpdates: clause.Assignments(map[string]interface{}{"last_indexed_seq": nextCursor.LastIndexedSeq, "updated_at": nextCursor.UpdatedAt}),
		}).Create(&nextCursor).Error; err != nil {
			return tasksvc.Result{}, err
		}
		final := fmt.Sprintf("群聊 %s 没有可用于 RAG 的有效消息", roomID)
		if skippedByRange > 0 {
			final = fmt.Sprintf("群聊 %s RAG 索引无新增分段，跳过重复区间 %d 个", roomID, skippedByRange)
		}
		return tasksvc.Result{
			Text:  &final,
			Final: &final,
			Payload: map[string]interface{}{
				"room_id":          roomID,
				"latest_seq":       latest.SequenceID,
				"last_indexed_seq": nextCursor.LastIndexedSeq,
				"indexed_segments": 0,
				"skipped_segments": skippedByRange,
				"status":           "no_valid_messages",
			},
		}, nil
	}
	now := time.Now()
	for i := range segments {
		segments[i].CreatedAt = now
		segments[i].UpdatedAt = now
	}
	if err := h.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "segment_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"start_seq", "end_seq", "start_at", "end_at", "message_count", "raw_text", "raw_text_hash", "qdrant_point_id", "updated_at"}),
	}).Create(&segments).Error; err != nil {
		return tasksvc.Result{}, err
	}
	taskSegments := make([]tasksvc.RAGSegment, 0, len(segments))
	for _, seg := range segments {
		taskSegments = append(taskSegments, tasksvc.RAGSegment{
			SegmentID:    seg.SegmentID,
			PointID:      seg.QdrantPointID,
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
	if err := h.upsertRAGPoints(ctx, taskSegments); err != nil {
		return tasksvc.Result{}, err
	}
	nextCursor := models.ChatSegmentCursor{
		RoomID:         roomID,
		LastIndexedSeq: latest.SequenceID,
		UpdatedAt:      now,
	}
	if err := h.db.Clauses(clause.OnConflict{
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
			"skipped_segments":     skippedByRange,
			"overlap_messages":     overlapMessages,
			"segment_gap_seconds":  gapSeconds,
		},
	}, nil
}

func (h *Handler) ExecuteRAGSearch(ctx context.Context, payload *tasksvc.RAGSearchPayload) (tasksvc.Result, error) {
	if h == nil {
		return tasksvc.Result{}, errors.New("rag handler is not initialized")
	}
	if h.embeddingProvider == nil {
		return tasksvc.Result{}, errors.New("embedding provider is not configured")
	}
	if h.vectorStore == nil {
		return tasksvc.Result{}, errors.New("vector store is not configured")
	}
	if payload == nil || strings.TrimSpace(payload.RoomID) == "" {
		return tasksvc.Result{}, tasksvc.ErrInvalidRAGRoomID
	}
	query := strings.TrimSpace(payload.Query)
	if query == "" {
		return tasksvc.Result{}, tasksvc.ErrInvalidRAGSearchQuery
	}
	topK := payload.TopK
	if topK <= 0 {
		topK = defaultRAGSearchTopK
	}
	if topK > maxRAGSearchTopK {
		topK = maxRAGSearchTopK
	}
	vectors, err := h.embeddingProvider.EmbedTexts(ctx, []string{query})
	if err != nil {
		return tasksvc.Result{}, err
	}
	if len(vectors) == 0 || len(vectors[0]) == 0 {
		return tasksvc.Result{}, errors.New("rag search embedding result is empty")
	}
	vectorMode := strings.ToLower(strings.TrimSpace(payload.Vector))
	if vectorMode == "" {
		vectorMode = ragSearchVectorRaw
	}
	var hits []tasksvc.VectorSearchHit
	switch vectorMode {
	case ragSearchVectorSummary:
		hits, err = h.vectorStore.SearchSummary(ctx, strings.TrimSpace(payload.RoomID), vectors[0], topK)
	case ragSearchVectorRaw:
		hits, err = h.vectorStore.SearchRaw(ctx, strings.TrimSpace(payload.RoomID), vectors[0], topK)
	default:
		return tasksvc.Result{}, fmt.Errorf("invalid rag search vector: %s", vectorMode)
	}
	if err != nil {
		return tasksvc.Result{}, err
	}
	segmentIDSet := make(map[string]struct{}, len(hits))
	segmentIDs := make([]string, 0, len(hits))
	for _, hit := range hits {
		segmentID := strings.TrimSpace(fmt.Sprint(hit.Payload["segment_id"]))
		if segmentID == "" {
			continue
		}
		if _, exists := segmentIDSet[segmentID]; exists {
			continue
		}
		segmentIDSet[segmentID] = struct{}{}
		segmentIDs = append(segmentIDs, segmentID)
	}
	rawTextBySegmentID := make(map[string]string, len(segmentIDs))
	if h.db != nil && len(segmentIDs) > 0 {
		var segments []models.ChatSegment
		if err := h.db.Select("segment_id", "raw_text").Where("segment_id IN ?", segmentIDs).Find(&segments).Error; err != nil {
			return tasksvc.Result{}, err
		}
		for _, seg := range segments {
			rawTextBySegmentID[seg.SegmentID] = seg.RawText
		}
	}
	results := make([]map[string]interface{}, 0, len(hits))
	for _, hit := range hits {
		segmentID := strings.TrimSpace(fmt.Sprint(hit.Payload["segment_id"]))
		rawText := strings.TrimSpace(rawTextBySegmentID[segmentID])
		if rawText == "" {
			rawText = strings.TrimSpace(fmt.Sprint(hit.Payload["raw_text"]))
		}
		results = append(results, map[string]interface{}{
			"point_id":      hit.PointID,
			"score":         hit.Score,
			"segment_id":    segmentID,
			"start_seq":     hit.Payload["start_seq"],
			"end_seq":       hit.Payload["end_seq"],
			"message_count": hit.Payload["message_count"],
			"text_preview":  hit.Payload["text_preview"],
			"summary":       hit.Payload["summary"],
			"summary_ready": hit.Payload["summary_ready"],
			"raw_text":      rawText,
		})
	}
	resultsJSONBytes, err := json.Marshal(results)
	if err != nil {
		return tasksvc.Result{}, err
	}
	resultsJSON := string(resultsJSONBytes)
	final := fmt.Sprintf("RAG 检索完成，命中 %d 条", len(results))
	return tasksvc.Result{
		Text:  &final,
		Final: &final,
		Payload: map[string]interface{}{
			"room_id":      strings.TrimSpace(payload.RoomID),
			"query":        query,
			"vector":       vectorMode,
			"top_k":        topK,
			"result_count": len(results),
			"results":      results,
			"results_json": resultsJSON,
		},
	}, nil
}

func (h *Handler) ExecuteRAGAddMemory(ctx context.Context, payload *tasksvc.RAGAddMemoryPayload) (tasksvc.Result, error) {
	if h == nil || h.db == nil {
		return tasksvc.Result{}, errors.New("rag handler db is not initialized")
	}
	if payload == nil || strings.TrimSpace(payload.RoomID) == "" {
		return tasksvc.Result{}, tasksvc.ErrInvalidRAGRoomID
	}
	if payload.StartSequenceID <= 0 || payload.EndSequenceID <= 0 || payload.StartSequenceID > payload.EndSequenceID {
		return tasksvc.Result{}, tasksvc.ErrInvalidRAGMemorySequenceRange
	}
	roomID := strings.TrimSpace(payload.RoomID)
	gapSeconds := payload.SegmentGapSeconds
	if gapSeconds <= 0 {
		gapSeconds = defaultSegmentGapSeconds
	}
	maxChars := payload.MaxCharsPerSegment
	if maxChars <= 0 {
		maxChars = defaultMaxCharsPerSegment
	}
	maxMessages := payload.MaxMessagesPerSeg
	if maxMessages <= 0 {
		maxMessages = defaultMaxMessagesPerSeg
	}
	overlapMessages := payload.OverlapMessages
	if overlapMessages < 0 {
		overlapMessages = 0
	}
	if overlapMessages > 3 {
		overlapMessages = 3
	}
	stopPhrases := loadRAGStopPhrases()
	var messages []models.Message
	if err := h.db.Where("room_id = ? AND sequence_id >= ? AND sequence_id <= ?", roomID, payload.StartSequenceID, payload.EndSequenceID).
		Where("content_type IN ?", []models.ContentType{
			models.ContentTypeText,
			models.ContentTypeImage,
			models.ContentTypeFile,
		}).
		Order("sequence_id asc").
		Find(&messages).Error; err != nil {
		return tasksvc.Result{}, err
	}
	if len(messages) == 0 {
		final := fmt.Sprintf("群聊 %s 在序列号区间 [%d, %d] 没有消息", roomID, payload.StartSequenceID, payload.EndSequenceID)
		return tasksvc.Result{
			Text:  &final,
			Final: &final,
			Payload: map[string]interface{}{
				"room_id":           roomID,
				"start_sequence_id": payload.StartSequenceID,
				"end_sequence_id":   payload.EndSequenceID,
				"indexed_segments":  0,
				"status":            "empty_range",
			},
		}, nil
	}
	senderNames, err := h.loadRAGSenderNames(roomID, messages)
	if err != nil {
		return tasksvc.Result{}, err
	}
	lines := make([]ragSegmentLine, 0, maxMessages)
	segments := make([]models.ChatSegment, 0)
	memoryBatchID := strings.ReplaceAll(uuid.NewString(), "-", "")
	if len(memoryBatchID) > 12 {
		memoryBatchID = memoryBatchID[:12]
	}
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
		segmentID := fmt.Sprintf("mem_%s_%d_%d_%s", roomID, start.seq, end.seq, memoryBatchID)
		pointID := buildQdrantPointID(segmentID)
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
	segments, skippedByRange, err := h.filterExistingSegmentsBySeqRange(roomID, segments)
	if err != nil {
		return tasksvc.Result{}, err
	}
	if len(segments) == 0 {
		final := fmt.Sprintf("群聊 %s 在区间 [%d, %d] 无新增分段，跳过重复区间 %d 个", roomID, payload.StartSequenceID, payload.EndSequenceID, skippedByRange)
		return tasksvc.Result{
			Text:  &final,
			Final: &final,
			Payload: map[string]interface{}{
				"room_id":           roomID,
				"start_sequence_id": payload.StartSequenceID,
				"end_sequence_id":   payload.EndSequenceID,
				"indexed_segments":  0,
				"skipped_segments":  skippedByRange,
				"status":            "no_new_segments",
			},
		}, nil
	}
	now := time.Now()
	for i := range segments {
		segments[i].CreatedAt = now
		segments[i].UpdatedAt = now
	}
	if err := h.db.Create(&segments).Error; err != nil {
		return tasksvc.Result{}, err
	}
	taskSegments := make([]tasksvc.RAGSegment, 0, len(segments))
	for _, seg := range segments {
		taskSegments = append(taskSegments, tasksvc.RAGSegment{
			SegmentID:    seg.SegmentID,
			PointID:      seg.QdrantPointID,
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
	if err := h.upsertRAGPoints(ctx, taskSegments); err != nil {
		return tasksvc.Result{}, err
	}
	final := fmt.Sprintf("群聊 %s 添加记忆完成，区间 [%d, %d] 新增分段 %d 个", roomID, payload.StartSequenceID, payload.EndSequenceID, len(taskSegments))
	return tasksvc.Result{
		Text:  &final,
		Final: &final,
		Payload: map[string]interface{}{
			"room_id":             roomID,
			"start_sequence_id":   payload.StartSequenceID,
			"end_sequence_id":     payload.EndSequenceID,
			"indexed_segments":    len(taskSegments),
			"skipped_segments":    skippedByRange,
			"overlap_messages":    overlapMessages,
			"segment_gap_seconds": gapSeconds,
			"cursor_updated":      false,
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
		raw := strings.TrimSpace(*msg.ContentText)
		if tool, desc, ok := parseRAGToolCall(raw); ok {
			return fmt.Sprintf("%s调用了[%s]工具[%s]", sender, tool, desc), true
		}
		cleaned, ok := cleanRAGText(raw, stopPhrases)
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

func (h *Handler) loadRAGSenderNames(roomID string, messages []models.Message) (map[string]string, error) {
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
	if err := h.db.Where("room_id = ? AND user_id IN ?", roomID, userIDs).Find(&members).Error; err != nil {
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
	if err := h.db.Where("id IN ?", missingUserIDs).Find(&users).Error; err != nil {
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

func (h *Handler) filterExistingSegmentsBySeqRange(roomID string, segments []models.ChatSegment) ([]models.ChatSegment, int, error) {
	if h == nil || h.db == nil || len(segments) == 0 {
		return segments, 0, nil
	}
	type seqRange struct {
		start int64
		end   int64
	}
	startSeqs := make([]int64, 0, len(segments))
	endSeqs := make([]int64, 0, len(segments))
	startSet := make(map[int64]struct{}, len(segments))
	endSet := make(map[int64]struct{}, len(segments))
	for _, seg := range segments {
		if _, ok := startSet[seg.StartSeq]; !ok {
			startSet[seg.StartSeq] = struct{}{}
			startSeqs = append(startSeqs, seg.StartSeq)
		}
		if _, ok := endSet[seg.EndSeq]; !ok {
			endSet[seg.EndSeq] = struct{}{}
			endSeqs = append(endSeqs, seg.EndSeq)
		}
	}
	var existing []models.ChatSegment
	if err := h.db.Select("start_seq", "end_seq").Where("room_id = ?", strings.TrimSpace(roomID)).
		Where("start_seq IN ?", startSeqs).
		Where("end_seq IN ?", endSeqs).
		Find(&existing).Error; err != nil {
		return nil, 0, err
	}
	existingSet := make(map[seqRange]struct{}, len(existing))
	for _, seg := range existing {
		existingSet[seqRange{start: seg.StartSeq, end: seg.EndSeq}] = struct{}{}
	}
	filtered := make([]models.ChatSegment, 0, len(segments))
	skipped := 0
	selectedSet := make(map[seqRange]struct{}, len(segments))
	for _, seg := range segments {
		key := seqRange{start: seg.StartSeq, end: seg.EndSeq}
		if _, exists := existingSet[key]; exists {
			skipped++
			continue
		}
		if _, exists := selectedSet[key]; exists {
			skipped++
			continue
		}
		selectedSet[key] = struct{}{}
		filtered = append(filtered, seg)
	}
	return filtered, skipped, nil
}

func buildQdrantPointID(segmentID string) string {
	trimmed := strings.TrimSpace(segmentID)
	if trimmed == "" {
		return uuid.NewString()
	}
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(trimmed)).String()
}

func cleanRAGText(raw string, stopPhrases map[string]struct{}) (string, bool) {
	text := strings.TrimSpace(raw)
	if text == "" {
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

func parseRAGToolCall(text string) (string, string, bool) {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "\\") {
		return "", "", false
	}
	body := strings.TrimSpace(strings.TrimPrefix(trimmed, "\\"))
	if body == "" {
		return "命令", fmt.Sprintf("执行命令参数：%s", body), true
	}
	parts := strings.Fields(body)
	cmd := strings.TrimSpace(parts[0])
	arg := ""
	if len(parts) > 1 {
		arg = strings.TrimSpace(strings.Join(parts[1:], " "))
	}
	invalid := func(v string) string { return fmt.Sprintf("执行命令参数：%s", v) }
	switch cmd {
	case "task:fake_llm":
		if arg == "" {
			return "fake_llm", invalid(arg), true
		}
		return "fake_llm", fmt.Sprintf("使用假LLM处理提示词：%s", arg), true
	case "task:llm":
		if arg == "" {
			return "LLM", invalid(arg), true
		}
		return "LLM", fmt.Sprintf("发起LLM对话：%s", arg), true
	case "对话":
		if arg == "" {
			return "对话", invalid(arg), true
		}
		return "对话", fmt.Sprintf("发起对话请求：%s", arg), true
	case "生成摘要", "摘要", "summary":
		n, err := strconv.Atoi(arg)
		if err != nil || n <= 0 || n > 1000 {
			return "摘要", invalid(arg), true
		}
		return "摘要", fmt.Sprintf("总结前%d条消息", n), true
	case "agent", "智能体":
		if arg == "" {
			return "智能体", invalid(arg), true
		}
		return "智能体", fmt.Sprintf("以目标“%s”执行任务", arg), true
	case "rag", "生成rag":
		if arg != "" {
			return "RAG", invalid(arg), true
		}
		return "RAG", "触发群聊向量索引构建", true
	case "rag检索":
		if arg == "" {
			return "RAG检索", invalid(arg), true
		}
		return "RAG检索", fmt.Sprintf("检索相关群聊片段：%s", arg), true
	case "添加记忆":
		args := strings.Fields(arg)
		if len(args) < 2 {
			return "添加记忆", invalid(arg), true
		}
		startSeq, startErr := strconv.ParseInt(strings.TrimSpace(args[0]), 10, 64)
		endSeq, endErr := strconv.ParseInt(strings.TrimSpace(args[1]), 10, 64)
		if startErr != nil || endErr != nil || startSeq <= 0 || endSeq <= 0 || startSeq > endSeq {
			return "添加记忆", invalid(arg), true
		}
		return "添加记忆", fmt.Sprintf("将序列号区间[%d, %d]分段并写入RAG记忆", startSeq, endSeq), true
	default:
		return cmd, invalid(arg), true
	}
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

func (h *Handler) upsertRAGPoints(ctx context.Context, segments []tasksvc.RAGSegment) error {
	if len(segments) == 0 {
		return nil
	}
	if h.embeddingProvider == nil {
		return errors.New("embedding provider is not configured")
	}
	if h.vectorStore == nil {
		return errors.New("vector store is not configured")
	}
	rawVectors, err := h.embeddingProvider.EmbedRawSegments(ctx, segments)
	if err != nil {
		return err
	}
	if len(rawVectors) != len(segments) {
		return fmt.Errorf("embedding vector count mismatch: want=%d got=%d", len(segments), len(rawVectors))
	}
	summaryTexts := make([]string, len(segments))
	summaryReady := make([]bool, len(segments))
	summaryInputIndexes := make([]int, 0, len(segments))
	summaryInputs := make([]string, 0, len(segments))
	if h.llmClient != nil {
		for i, seg := range segments {
			if seg.MessageCount < defaultMinSummaryMessageCount {
				continue
			}
			summary, summaryErr := h.generateSegmentSummary(ctx, seg.RawText)
			if summaryErr != nil {
				continue
			}
			summaryTexts[i] = summary
			summaryInputIndexes = append(summaryInputIndexes, i)
			summaryInputs = append(summaryInputs, summary)
		}
	}
	summaryVectorsByIndex := make(map[int][]float32, len(summaryInputIndexes))
	if len(summaryInputs) > 0 {
		summaryVectors, summaryVecErr := h.embeddingProvider.EmbedTexts(ctx, summaryInputs)
		if summaryVecErr == nil && len(summaryVectors) == len(summaryInputIndexes) {
			for i, idx := range summaryInputIndexes {
				summaryVectorsByIndex[idx] = summaryVectors[i]
				summaryReady[idx] = true
			}
		}
	}
	points := make([]tasksvc.VectorPoint, 0, len(segments))
	for i, seg := range segments {
		pointID := strings.TrimSpace(seg.PointID)
		if pointID == "" {
			sourceID := strings.TrimSpace(seg.SegmentID)
			if sourceID == "" {
				sourceID = fmt.Sprintf("%s-%d-%d", seg.RoomID, seg.StartSeq, seg.EndSeq)
			}
			pointID = buildQdrantPointID(sourceID)
		}
		summaryVec := make([]float32, 0)
		if vec, ok := summaryVectorsByIndex[i]; ok && len(vec) > 0 {
			summaryVec = vec
		} else if h.summaryVectorDim > 0 {
			summaryVec = make([]float32, h.summaryVectorDim)
		}
		embeddingModelSummary := ""
		if summaryReady[i] {
			embeddingModelSummary = h.embeddingModelSummary
		}
		payload := map[string]interface{}{
			"room_id":                 seg.RoomID,
			"segment_id":              seg.SegmentID,
			"start_seq":               seg.StartSeq,
			"end_seq":                 seg.EndSeq,
			"start_ts":                seg.StartUnixSec,
			"end_ts":                  seg.EndUnixSec,
			"message_count":           seg.MessageCount,
			"summary_ready":           summaryReady[i],
			"embedding_model_raw":     h.embeddingModelRaw,
			"embedding_model_summary": embeddingModelSummary,
			"text_preview":            seg.TextPreview,
		}
		if summaryReady[i] {
			payload["summary"] = summaryTexts[i]
		}
		points = append(points, tasksvc.VectorPoint{
			PointID: pointID,
			Raw:     rawVectors[i],
			Summary: summaryVec,
			Payload: payload,
		})
	}
	if err := h.vectorStore.UpsertPoints(ctx, points); err != nil {
		return err
	}
	successSegmentIDs := make([]string, 0, len(segments))
	failedSegmentIDs := make([]string, 0, len(segments))
	for i, seg := range segments {
		if summaryReady[i] {
			successSegmentIDs = append(successSegmentIDs, seg.SegmentID)
		} else {
			failedSegmentIDs = append(failedSegmentIDs, seg.SegmentID)
		}
	}
	if len(successSegmentIDs) > 0 {
		if err := h.db.Model(&models.ChatSegment{}).Where("segment_id IN ?", successSegmentIDs).Updates(map[string]interface{}{
			"summary_ready": true,
			"updated_at":    time.Now(),
		}).Error; err != nil {
			return err
		}
	}
	if len(failedSegmentIDs) > 0 {
		if err := h.db.Model(&models.ChatSegment{}).Where("segment_id IN ?", failedSegmentIDs).Updates(map[string]interface{}{
			"summary_ready": false,
			"updated_at":    time.Now(),
		}).Error; err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) generateSegmentSummary(ctx context.Context, rawText string) (string, error) {
	if h == nil || h.llmClient == nil {
		return "", errors.New("llm client is not configured")
	}
	cleaned := strings.TrimSpace(rawText)
	if cleaned == "" {
		return "", errors.New("segment raw text is empty")
	}
	prompt := strings.Join([]string{
		"你是一个“群聊总结AI”。请基于给定聊天内容，生成一份可读性强、信息完整的聊天总结。",
		"请使用专业中文编辑风格，表达自然、准确、克制。",
		"目标是让没看原聊天的人，能快速理解这段时间群里到底讨论了什么、怎么讨论的、最后形成了什么结论。",
		"",
		"写作约束与要求：",
		"1) 只基于提供内容，不得编造人物、时间、结论。",
		"2) 语言自然、具体，有画面感，避免口号和空话。",
		"3) 信息优先级：具体事实 > 观点判断 > 修饰表达。",
		"4) 若结论或后续动作不明确，直接写“尚无明确结论”或“暂无明确后续动作”。",
		"5) 不要输出解释过程。",
		fmt.Sprintf("6) 严格控制总长度：全文不超过%d个中文字符。", defaultMaxSummaryChars),
		"",
		"输出格式（严格按此顺序）：",
		"[整体总结]",
		"{用1-2句概括这段聊天主要讨论内容与整体氛围/进展}",
		"",
		"[连贯叙述]",
		"{写成一段连贯叙述，完整说明讨论如何展开，包含关键细节、主要观点、他人补充与争议点（如有）}",
		"",
		"[最终结论和后续方向]",
		"{给出阶段性结论与后续方向；若不明确，写“尚无明确结论”或“暂无明确后续动作”}",
		"",
		"以下是聊天内容：",
		cleaned,
	}, "\n")
	result, err := h.llmClient.Chat(ctx, prompt)
	if err != nil {
		return "", err
	}
	summary := strings.TrimSpace(result)
	if summary == "" {
		return "", errors.New("summary result is empty")
	}
	summary = previewText(summary, defaultMaxSummaryChars)
	return summary, nil
}
