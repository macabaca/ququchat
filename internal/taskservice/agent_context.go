package taskservice

import (
	"fmt"
	"strings"

	"ququchat/internal/models"
)

func (s *Service) loadAgentRecentMessages(roomID string, limit int) ([]string, error) {
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
