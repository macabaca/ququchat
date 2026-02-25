package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"ququchat/internal/models"
)

const (
	wsWriteWait   = 10 * time.Second
	wsPongWait    = 60 * time.Second
	wsPingPeriod  = 50 * time.Second
	wsMaxMsgBytes = 64 * 1024
)

type WsHandler struct {
	db  *gorm.DB
	hub *Hub
}

func NewWsHandler(db *gorm.DB) *WsHandler {
	return &WsHandler{
		db:  db,
		hub: NewHub(),
	}
}

type Hub struct {
	clients       map[*Client]bool
	clientsByUser map[string]map[*Client]bool
	register      chan *Client
	unregister    chan *Client
	direct        chan DirectMessage
	broadcast     chan GroupMessage
}

type DirectMessage struct {
	FromUserID string
	ToUserID   string
	Data       []byte
}

type GroupMessage struct {
	RoomID  string
	UserIDs []string
	Data    []byte
}

func NewHub() *Hub {
	h := &Hub{
		clients:       make(map[*Client]bool),
		clientsByUser: make(map[string]map[*Client]bool),
		register:      make(chan *Client),
		unregister:    make(chan *Client),
		direct:        make(chan DirectMessage),
		broadcast:     make(chan GroupMessage),
	}
	go h.run()
	return h
}

func (h *Hub) run() {
	for {
		select {
		case c := <-h.register:
			h.clients[c] = true
			if _, ok := h.clientsByUser[c.userID]; !ok {
				h.clientsByUser[c.userID] = make(map[*Client]bool)
			}
			h.clientsByUser[c.userID][c] = true
		case c := <-h.unregister:
			if _, ok := h.clients[c]; ok {
				delete(h.clients, c)
				if set, ok := h.clientsByUser[c.userID]; ok {
					delete(set, c)
					if len(set) == 0 {
						delete(h.clientsByUser, c.userID)
					}
				}
				close(c.send)
			}
		case msg := <-h.direct:
			if set, ok := h.clientsByUser[msg.ToUserID]; ok {
				for c := range set {
					select {
					case c.send <- msg.Data:
					default:
						close(c.send)
						delete(h.clients, c)
						delete(set, c)
					}
				}
				if len(set) == 0 {
					delete(h.clientsByUser, msg.ToUserID)
				}
			}
			if set, ok := h.clientsByUser[msg.FromUserID]; ok {
				for c := range set {
					select {
					case c.send <- msg.Data:
					default:
						close(c.send)
						delete(h.clients, c)
						delete(set, c)
					}
				}
				if len(set) == 0 {
					delete(h.clientsByUser, msg.FromUserID)
				}
			}
		case msg := <-h.broadcast:
			for _, uid := range msg.UserIDs {
				if set, ok := h.clientsByUser[uid]; ok {
					for c := range set {
						select {
						case c.send <- msg.Data:
						default:
							close(c.send)
							delete(h.clients, c)
							delete(set, c)
						}
					}
					if len(set) == 0 {
						delete(h.clientsByUser, uid)
					}
				}
			}
		}
	}
}

type Client struct {
	hub    *Hub
	conn   *websocket.Conn
	send   chan []byte
	userID string
}

type IncomingMessage struct {
	Type         string `json:"type"`
	ToUser       string `json:"to_user_id,omitempty"`
	RoomID       string `json:"room_id,omitempty"`
	Content      string `json:"content,omitempty"`
	AttachmentID string `json:"attachment_id,omitempty"`
}

type OutgoingMessage struct {
	ID           string             `json:"id"`
	Type         string             `json:"type"`
	FromUser     string             `json:"from_user_id"`
	ToUser       string             `json:"to_user_id,omitempty"`
	RoomID       string             `json:"room_id,omitempty"`
	Content      string             `json:"content,omitempty"`
	AttachmentID string             `json:"attachment_id,omitempty"`
	Attachment   *AttachmentPayload `json:"attachment,omitempty"`
	Timestamp    int64              `json:"timestamp"`
	SequenceID   int64              `json:"sequence_id"`
}

type AttachmentPayload struct {
	AttachmentID      string  `json:"attachment_id"`
	FileName          *string `json:"file_name,omitempty"`
	MimeType          *string `json:"mime_type,omitempty"`
	SizeBytes         *int64  `json:"size_bytes,omitempty"`
	Hash              *string `json:"hash,omitempty"`
	StorageProvider   *string `json:"storage_provider,omitempty"`
	ImageWidth        *int    `json:"image_width,omitempty"`
	ImageHeight       *int    `json:"image_height,omitempty"`
	ThumbAttachmentID *string `json:"thumb_attachment_id,omitempty"`
	ThumbWidth        *int    `json:"thumb_width,omitempty"`
	ThumbHeight       *int    `json:"thumb_height,omitempty"`
	CreatedAt         int64   `json:"created_at"`
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func (h *WsHandler) Handle(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	client := &Client{
		hub:    h.hub,
		conn:   conn,
		send:   make(chan []byte, 256),
		userID: userID,
	}
	client.hub.register <- client

	go client.writeLoop()
	go client.readLoop(h)
}

func (c *Client) readLoop(h *WsHandler) {
	defer func() {
		c.hub.unregister <- c
		_ = c.conn.Close()
	}()
	c.conn.SetReadLimit(wsMaxMsgBytes)
	_ = c.conn.SetReadDeadline(time.Now().Add(wsPongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(wsPongWait))
	})
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
		var msg IncomingMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		if msg.Type == "ping" {
			resp, err := json.Marshal(map[string]interface{}{
				"type": "pong",
				"ts":   time.Now().Unix(),
			})
			if err == nil {
				select {
				case c.send <- resp:
				default:
				}
			}
			continue
		}
		if msg.Type == "pong" {
			continue
		}
		if msg.Type == "friend_message" {
			if msg.ToUser == "" || msg.Content == "" {
				continue
			}
			if !h.areFriends(c.userID, msg.ToUser) {
				continue
			}
			roomID, err := h.ensureDirectRoom(c.userID, msg.ToUser)
			if err != nil {
				continue
			}
			savedMsg, err := h.saveDirectMessage(roomID, c.userID, msg.Content)
			if err != nil {
				continue
			}
			out := OutgoingMessage{
				ID:         savedMsg.ID,
				Type:       "friend_message",
				FromUser:   c.userID,
				ToUser:     msg.ToUser,
				RoomID:     roomID,
				Content:    msg.Content,
				Timestamp:  savedMsg.CreatedAt.Unix(),
				SequenceID: savedMsg.SequenceID,
			}
			b, err := json.Marshal(out)
			if err != nil {
				continue
			}
			c.hub.direct <- DirectMessage{
				FromUserID: c.userID,
				ToUserID:   msg.ToUser,
				Data:       b,
			}
		} else if msg.Type == "group_message" {
			if msg.RoomID == "" || msg.Content == "" {
				continue
			}
			// Check if user is a member of the group and not muted
			if err := h.checkGroupPostingPermission(msg.RoomID, c.userID); err != nil {
				// Optionally send error back to user
				continue
			}
			savedMsg, err := h.saveGroupMessage(msg.RoomID, c.userID, msg.Content)
			if err != nil {
				continue
			}

			// Get all active members to broadcast
			memberIDs, err := h.getGroupMemberIDs(msg.RoomID)
			if err != nil {
				continue
			}

			out := OutgoingMessage{
				ID:         savedMsg.ID,
				Type:       "group_message",
				FromUser:   c.userID,
				RoomID:     msg.RoomID,
				Content:    msg.Content,
				Timestamp:  savedMsg.CreatedAt.Unix(),
				SequenceID: savedMsg.SequenceID,
			}
			b, err := json.Marshal(out)
			if err != nil {
				continue
			}
			c.hub.broadcast <- GroupMessage{
				RoomID:  msg.RoomID,
				UserIDs: memberIDs,
				Data:    b,
			}
		} else if msg.Type == "file_message" || msg.Type == "image_message" {
			if msg.AttachmentID == "" {
				continue
			}
			attachment, payload, payloadJSON, err := h.loadAttachmentPayload(c.userID, msg.AttachmentID)
			if err != nil {
				continue
			}
			contentType := models.ContentTypeFile
			outType := "file_message"
			if isImageAttachment(attachment) {
				contentType = models.ContentTypeImage
				outType = "image_message"
			}
			if msg.ToUser != "" {
				if !h.areFriends(c.userID, msg.ToUser) {
					continue
				}
				roomID, err := h.ensureDirectRoom(c.userID, msg.ToUser)
				if err != nil {
					continue
				}
				savedMsg, err := h.saveAttachmentMessage(roomID, c.userID, attachment.ID, payloadJSON, contentType)
				if err != nil {
					continue
				}
				out := OutgoingMessage{
					ID:           savedMsg.ID,
					Type:         outType,
					FromUser:     c.userID,
					ToUser:       msg.ToUser,
					RoomID:       roomID,
					AttachmentID: attachment.ID,
					Attachment:   payload,
					Timestamp:    savedMsg.CreatedAt.Unix(),
					SequenceID:   savedMsg.SequenceID,
				}
				b, err := json.Marshal(out)
				if err != nil {
					continue
				}
				c.hub.direct <- DirectMessage{
					FromUserID: c.userID,
					ToUserID:   msg.ToUser,
					Data:       b,
				}
			} else if msg.RoomID != "" {
				if err := h.checkGroupPostingPermission(msg.RoomID, c.userID); err != nil {
					continue
				}
				savedMsg, err := h.saveAttachmentMessage(msg.RoomID, c.userID, attachment.ID, payloadJSON, contentType)
				if err != nil {
					continue
				}
				memberIDs, err := h.getGroupMemberIDs(msg.RoomID)
				if err != nil {
					continue
				}
				out := OutgoingMessage{
					ID:           savedMsg.ID,
					Type:         outType,
					FromUser:     c.userID,
					RoomID:       msg.RoomID,
					AttachmentID: attachment.ID,
					Attachment:   payload,
					Timestamp:    savedMsg.CreatedAt.Unix(),
					SequenceID:   savedMsg.SequenceID,
				}
				b, err := json.Marshal(out)
				if err != nil {
					continue
				}
				c.hub.broadcast <- GroupMessage{
					RoomID:  msg.RoomID,
					UserIDs: memberIDs,
					Data:    b,
				}
			}
		}
	}
}

func (c *Client) writeLoop() {
	defer func() {
		_ = c.conn.Close()
	}()
	ticker := time.NewTicker(wsPingPeriod)
	defer ticker.Stop()
	for {
		select {
		case msg, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (h *WsHandler) areFriends(a, b string) bool {
	if a == "" || b == "" || a == b {
		return false
	}
	x, y := a, b
	if x > y {
		x, y = y, x
	}
	var f models.Friendship
	if err := h.db.Where("user_id_a = ? AND user_id_b = ?", x, y).First(&f).Error; err != nil {
		return false
	}
	return true
}

func (h *WsHandler) ensureDirectRoom(a, b string) (string, error) {
	x, y := a, b
	if x > y {
		x, y = y, x
	}
	name := x + ":" + y
	var room models.Room
	if err := h.db.Where("room_type = ? AND name = ?", models.RoomTypeDirect, name).First(&room).Error; err == nil {
		h.ensureDirectRoomMembers(room.ID, a, b)
		return room.ID, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", err
	}
	now := time.Now()
	room = models.Room{
		ID:          uuid.NewString(),
		RoomType:    models.RoomTypeDirect,
		Name:        name,
		OwnerUserID: x,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := h.db.Create(&room).Error; err != nil {
		return "", err
	}
	h.ensureDirectRoomMembers(room.ID, a, b)
	return room.ID, nil
}

func (h *WsHandler) checkGroupPostingPermission(roomID, userID string) error {
	var member models.RoomMember
	err := h.db.Where("room_id = ? AND user_id = ?", roomID, userID).First(&member).Error
	if err != nil {
		return err
	}
	if member.LeftAt != nil {
		return errors.New("user has left the group")
	}
	if member.MuteUntil != nil && member.MuteUntil.After(time.Now()) {
		return errors.New("user is muted")
	}
	return nil
}

func (h *WsHandler) getGroupMemberIDs(roomID string) ([]string, error) {
	var userIDs []string
	err := h.db.Model(&models.RoomMember{}).
		Where("room_id = ? AND left_at IS NULL", roomID).
		Pluck("user_id", &userIDs).Error
	return userIDs, err
}

func (h *WsHandler) saveGroupMessage(roomID, fromUserID, content string) (*models.Message, error) {
	text := content
	return h.saveMessage(roomID, fromUserID, models.ContentTypeText, &text, nil, nil)
}

func (h *WsHandler) saveDirectMessage(roomID, fromUserID, content string) (*models.Message, error) {
	text := content
	return h.saveMessage(roomID, fromUserID, models.ContentTypeText, &text, nil, nil)
}

func (h *WsHandler) saveAttachmentMessage(roomID, fromUserID, attachmentID string, payload datatypes.JSON, contentType models.ContentType) (*models.Message, error) {
	aid := attachmentID
	return h.saveMessage(roomID, fromUserID, contentType, nil, &aid, payload)
}

func (h *WsHandler) saveMessage(roomID, fromUserID string, contentType models.ContentType, contentText *string, attachmentID *string, payload datatypes.JSON) (*models.Message, error) {
	now := time.Now()

	// 重试逻辑：处理高并发下的 SequenceID 冲突（尤其是在 Postgres 无间隙锁的情况下）
	maxRetries := 3
	for i := 0; i < maxRetries; i++ {
		m := models.Message{
			ID:           uuid.NewString(),
			RoomID:       roomID,
			SenderID:     &fromUserID,
			ContentType:  contentType,
			ContentText:  contentText,
			AttachmentID: attachmentID,
			PayloadJSON:  payload,
			CreatedAt:    now,
		}

		err := h.db.Transaction(func(tx *gorm.DB) error {
			var lastMsg models.Message
			// 尝试锁定最新的一条消息
			// 注意：如果房间为空，First 会返回 RecordNotFound，此时无法加锁，依赖唯一索引冲突重试
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("room_id = ?", roomID).
				Order("sequence_id desc").
				First(&lastMsg).Error; err != nil {
				if !errors.Is(err, gorm.ErrRecordNotFound) {
					return err
				}
				m.SequenceID = 1
			} else {
				m.SequenceID = lastMsg.SequenceID + 1
			}
			return tx.Create(&m).Error
		})

		if err == nil {
			return &m, nil
		}
		// 如果是唯一索引冲突，稍微等待后重试
		time.Sleep(time.Duration(10*(i+1)) * time.Millisecond)
	}
	// TODO: 记录重试失败日志
	return nil, errors.New("failed to save message after retries")
}

func (h *WsHandler) loadAttachmentPayload(userID, attachmentID string) (*models.Attachment, *AttachmentPayload, datatypes.JSON, error) {
	var attachment models.Attachment
	if err := h.db.Where("id = ?", attachmentID).First(&attachment).Error; err != nil {
		return nil, nil, nil, err
	}
	if attachment.UploaderUserID != nil && *attachment.UploaderUserID != userID {
		return nil, nil, nil, errors.New("attachment not owned by user")
	}
	payload := &AttachmentPayload{
		AttachmentID:      attachment.ID,
		FileName:          attachment.FileName,
		MimeType:          attachment.MimeType,
		SizeBytes:         attachment.SizeBytes,
		Hash:              attachment.Hash,
		StorageProvider:   attachment.StorageProvider,
		ImageWidth:        attachment.ImageWidth,
		ImageHeight:       attachment.ImageHeight,
		ThumbAttachmentID: attachment.ThumbAttachmentID,
		ThumbWidth:        attachment.ThumbWidth,
		ThumbHeight:       attachment.ThumbHeight,
		CreatedAt:         attachment.CreatedAt.Unix(),
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, nil, err
	}
	return &attachment, payload, datatypes.JSON(b), nil
}

func isImageAttachment(attachment *models.Attachment) bool {
	if attachment == nil || attachment.MimeType == nil {
		return false
	}
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(*attachment.MimeType)), "image/")
}

func (h *WsHandler) ensureDirectRoomMembers(roomID, a, b string) {
	now := time.Now()
	ids := []string{a, b}
	for _, uid := range ids {
		if uid == "" {
			continue
		}
		var m models.RoomMember
		if err := h.db.Where("room_id = ? AND user_id = ?", roomID, uid).First(&m).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				_ = h.db.Create(&models.RoomMember{
					RoomID:   roomID,
					UserID:   uid,
					Role:     models.MemberRoleMember,
					JoinedAt: now,
				}).Error
			}
		}
	}
}
