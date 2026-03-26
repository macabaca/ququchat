package taskservice

import (
	"fmt"
	"strconv"
	"strings"

	"ququchat/internal/models"
)

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
