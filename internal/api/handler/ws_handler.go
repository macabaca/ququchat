package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"ququchat/internal/models"
	cachepkg "ququchat/internal/server/cache"
	taskservice "ququchat/internal/service"
	tasksvc "ququchat/internal/service/task"
)

const (
	wsWriteWait   = 10 * time.Second
	wsPongWait    = 60 * time.Second
	wsPingPeriod  = 50 * time.Second
	wsMaxMsgBytes = 64 * 1024
	wsRobotUserID = "00000000-0000-0000-0000-00000000a1b2"
)

type WsHandler struct {
	db              *gorm.DB
	hub             *Hub
	cacheClient     *cachepkg.RedisClient
	taskService     *taskservice.MainService
	streamHub       *taskservice.AgentStreamHub
	robotInit       sync.Once
	robotErr        error
	doneConsumerMu  sync.Mutex
	doneConsumerUp  bool
	doneConsumerErr error
}

func NewWsHandler(db *gorm.DB, hub *Hub, cacheClient *cachepkg.RedisClient, taskService *taskservice.MainService, streamHub *taskservice.AgentStreamHub) *WsHandler {
	if hub == nil {
		hub = NewHub()
	}
	if hub.db == nil {
		hub.db = db
	}
	if hub.cacheClient == nil {
		hub.cacheClient = cacheClient
	}
	return &WsHandler{
		db:          db,
		hub:         hub,
		cacheClient: cacheClient,
		taskService: taskService,
		streamHub:   streamHub,
	}
}

type Hub struct {
	clients       map[*Client]bool
	clientsByUser map[string]map[*Client]bool
	register      chan *Client
	unregister    chan *Client
	direct        chan DirectMessage
	broadcast     chan GroupMessage
	db            *gorm.DB
	cacheClient   *cachepkg.RedisClient
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

type SystemEvent struct {
	Type  string `json:"type"`
	Event string `json:"event"`
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

func (h *Hub) SendSystemEventToUser(userID string, event string) {
	if userID == "" {
		return
	}
	h.SendSystemEventToUsers([]string{userID}, event)
}

func (h *Hub) SendSystemEventToUsers(userIDs []string, event string) {
	if h == nil || len(userIDs) == 0 || event == "" {
		return
	}
	data, err := json.Marshal(SystemEvent{Type: "system_event", Event: event})
	if err != nil {
		log.Printf("failed to marshal system_event: %v", err)
		return
	}
	h.broadcast <- GroupMessage{UserIDs: userIDs, Data: data}
}

func (h *Hub) SendDataToUser(userID string, data []byte) {
	if userID == "" {
		return
	}
	h.SendDataToUsers([]string{userID}, data)
}

func (h *Hub) SendDataToUsers(userIDs []string, data []byte) {
	if h == nil || len(userIDs) == 0 || len(data) == 0 {
		return
	}
	h.broadcast <- GroupMessage{UserIDs: userIDs, Data: data}
}

func (h *Hub) run() {
	for {
		select {
		case c := <-h.register:
			h.handleRegister(c)
		case c := <-h.unregister:
			h.removeClient(c)
		case msg := <-h.direct:
			if set, ok := h.clientsByUser[msg.ToUserID]; ok {
				for c := range set {
					select {
					case c.send <- msg.Data:
					default:
						h.removeClient(c)
					}
				}
			}
			if set, ok := h.clientsByUser[msg.FromUserID]; ok {
				for c := range set {
					select {
					case c.send <- msg.Data:
					default:
						h.removeClient(c)
					}
				}
			}
		case msg := <-h.broadcast:
			for _, uid := range msg.UserIDs {
				if set, ok := h.clientsByUser[uid]; ok {
					for c := range set {
						select {
						case c.send <- msg.Data:
						default:
							h.removeClient(c)
						}
					}
				}
			}
		}
	}
}

func (h *Hub) handleRegister(c *Client) {
	h.clients[c] = true
	set, ok := h.clientsByUser[c.userID]
	if !ok {
		set = make(map[*Client]bool)
		h.clientsByUser[c.userID] = set
	}
	set[c] = true
	if len(set) == 1 {
		h.updateUserStatus(c.userID, "active")
	}
}

func (h *Hub) removeClient(c *Client) {
	if _, ok := h.clients[c]; !ok {
		return
	}
	delete(h.clients, c)
	if set, ok := h.clientsByUser[c.userID]; ok {
		delete(set, c)
		if len(set) == 0 {
			delete(h.clientsByUser, c.userID)
			h.updateUserStatus(c.userID, "offline")
		}
	}
	close(c.send)
}

func (h *Hub) updateUserStatus(userID, status string) {
	if h == nil || h.db == nil || userID == "" || status == "" {
		return
	}
	go func() {
		if err := h.db.Model(&models.User{}).Where("id = ?", userID).Update("status", status).Error; err != nil {
			log.Printf("ws update user status failed user=%s status=%s err=%v", userID, status, err)
			return
		}
		friendIDs, err := h.listFriendIDs(userID)
		if err != nil {
			log.Printf("ws list friends for status update failed user=%s err=%v", userID, err)
			return
		}
		if len(friendIDs) > 0 {
			h.SendSystemEventToUsers(friendIDs, "friend_list_updated")
		}
	}()
}

func (h *Hub) listFriendIDs(userID string) ([]string, error) {
	if h.cacheClient != nil {
		cacheKey := h.cacheClient.BuildKey(cachepkg.FriendIDsKey(userID)...)
		cacheCtx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
		var cached []string
		ok, err := h.cacheClient.GetJSON(cacheCtx, cacheKey, &cached)
		cancel()
		if err == nil && ok {
			return cached, nil
		}
	}
	var relations []models.Friendship
	if err := h.db.Where("user_id_a = ? OR user_id_b = ?", userID, userID).Find(&relations).Error; err != nil {
		return nil, err
	}
	friendIDs := make([]string, 0, len(relations))
	for _, r := range relations {
		if r.UserIDA == userID {
			friendIDs = append(friendIDs, r.UserIDB)
		} else {
			friendIDs = append(friendIDs, r.UserIDA)
		}
	}
	if h.cacheClient != nil {
		cacheKey := h.cacheClient.BuildKey(cachepkg.FriendIDsKey(userID)...)
		cacheCtx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
		_ = h.cacheClient.SetJSON(cacheCtx, cacheKey, friendIDs, cachepkg.FriendIDsTTL)
		cancel()
	}
	return friendIDs, nil
}

type Client struct {
	hub    *Hub
	conn   *websocket.Conn
	send   chan []byte
	userID string
}

type IncomingMessage struct {
	Type             string `json:"type"`
	ToUser           string `json:"to_user_id,omitempty"`
	RoomID           string `json:"room_id,omitempty"`
	Content          string `json:"content,omitempty"`
	AttachmentID     string `json:"attachment_id,omitempty"`
	ParentMessageID  string `json:"parent_message_id,omitempty"`
	ParentSequenceID *int64 `json:"parent_sequence_id,omitempty"`
}

type OutgoingMessage struct {
	ID               string             `json:"id"`
	Type             string             `json:"type"`
	FromUser         string             `json:"from_user_id"`
	ToUser           string             `json:"to_user_id,omitempty"`
	RoomID           string             `json:"room_id,omitempty"`
	Content          string             `json:"content,omitempty"`
	AttachmentID     string             `json:"attachment_id,omitempty"`
	Attachment       *AttachmentPayload `json:"attachment,omitempty"`
	PayloadJSON      datatypes.JSON     `json:"payload_json,omitempty"`
	ParentMessageID  string             `json:"parent_message_id,omitempty"`
	ParentSequenceID *int64             `json:"parent_sequence_id,omitempty"`
	Timestamp        int64              `json:"timestamp"`
	SequenceID       int64              `json:"sequence_id"`
}

type AgentCommandAck struct {
	Type             string `json:"type"`
	RequestID        string `json:"request_id"`
	TaskID           string `json:"task_id"`
	RoomID           string `json:"room_id"`
	ParentMessageID  string `json:"parent_message_id,omitempty"`
	ParentSequenceID int64  `json:"parent_sequence_id,omitempty"`
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
	_ = h.StartTaskDoneConsumer(context.Background())
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "未登录"})
		return
	}
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("ws upgrade failed user=%s ip=%s err=%v", userID, c.ClientIP(), err)
		return
	}
	log.Printf("ws connected user=%s ip=%s", userID, c.ClientIP())
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
			if ce, ok := err.(*websocket.CloseError); ok {
				log.Printf("ws read close user=%s code=%d text=%s", c.userID, ce.Code, ce.Text)
			} else {
				log.Printf("ws read error user=%s err=%v", c.userID, err)
			}
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
			savedMsg, err := h.saveDirectMessage(roomID, c.userID, msg.Content, strings.TrimSpace(msg.ParentMessageID), msg.ParentSequenceID)
			if err != nil {
				continue
			}
			out := OutgoingMessage{
				ID:       savedMsg.ID,
				Type:     "friend_message",
				FromUser: c.userID,
				ToUser:   msg.ToUser,
				RoomID:   roomID,
				Content:  msg.Content,
				ParentMessageID: func() string {
					if savedMsg.ParentMessageID == nil {
						return ""
					}
					return strings.TrimSpace(*savedMsg.ParentMessageID)
				}(),
				ParentSequenceID: savedMsg.ParentSequenceID,
				Timestamp:        savedMsg.CreatedAt.Unix(),
				SequenceID:       savedMsg.SequenceID,
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
			if strings.HasPrefix(strings.TrimSpace(msg.Content), "\\") {
				if err := h.checkGroupPostingPermission(msg.RoomID, c.userID); err != nil {
					continue
				}
				savedMsg, err := h.saveGroupMessage(msg.RoomID, c.userID, msg.Content, strings.TrimSpace(msg.ParentMessageID), msg.ParentSequenceID)
				if err != nil {
					continue
				}
				memberIDs, err := h.getGroupMemberIDs(msg.RoomID)
				if err != nil {
					continue
				}
				commandOut := OutgoingMessage{
					ID:       savedMsg.ID,
					Type:     "group_message",
					FromUser: c.userID,
					RoomID:   msg.RoomID,
					Content:  msg.Content,
					ParentMessageID: func() string {
						if savedMsg.ParentMessageID == nil {
							return ""
						}
						return strings.TrimSpace(*savedMsg.ParentMessageID)
					}(),
					ParentSequenceID: savedMsg.ParentSequenceID,
					Timestamp:        savedMsg.CreatedAt.Unix(),
					SequenceID:       savedMsg.SequenceID,
				}
				commandBroadcast, err := json.Marshal(commandOut)
				if err == nil {
					c.hub.broadcast <- GroupMessage{
						RoomID:  msg.RoomID,
						UserIDs: memberIDs,
						Data:    commandBroadcast,
					}
				}
				if h.taskService == nil {
					continue
				}
				userID := c.userID
				roomID := msg.RoomID
				requestID := taskservice.BuildWSCommandRequestID(userID, roomID, savedMsg.ID, savedMsg.SequenceID)
				taskID, err := h.taskService.SubmitCommand(taskservice.SubmitCommandRequest{
					RequestID:        requestID,
					UserID:           userID,
					RoomID:           roomID,
					Content:          msg.Content,
					ParentMessageID:  savedMsg.ID,
					ParentSequenceID: savedMsg.SequenceID,
				})
				if err != nil {
					h.publishAgentStreamEvent(taskservice.AgentStreamEvent{
						EventType:        "agent.error",
						RequestID:        requestID,
						RoomID:           roomID,
						UserID:           userID,
						Status:           "failed",
						Error:            err.Error(),
						ParentMessageID:  savedMsg.ID,
						ParentSequenceID: savedMsg.SequenceID,
					})
					log.Printf("submit command failed user=%s room=%s err=%v", userID, roomID, err)
					if sendErr := h.sendRobotGroupMessage(roomID, err.Error(), nil, savedMsg.ID, &savedMsg.SequenceID); sendErr != nil {
						log.Printf("send robot submit-failed message failed room=%s err=%v", roomID, sendErr)
					}
					continue
				}
				h.publishAgentStreamEvent(taskservice.AgentStreamEvent{
					EventType:        "agent.start",
					RequestID:        requestID,
					RoomID:           roomID,
					UserID:           userID,
					Status:           "running",
					Content:          strings.TrimSpace(msg.Content),
					ParentMessageID:  savedMsg.ID,
					ParentSequenceID: savedMsg.SequenceID,
				})
				h.sendAgentCommandAck(userID, roomID, requestID, taskID, savedMsg.ID, savedMsg.SequenceID)
				continue
			}
			// Check if user is a member of the group and not muted
			if err := h.checkGroupPostingPermission(msg.RoomID, c.userID); err != nil {
				// Optionally send error back to user
				continue
			}
			savedMsg, err := h.saveGroupMessage(msg.RoomID, c.userID, msg.Content, strings.TrimSpace(msg.ParentMessageID), msg.ParentSequenceID)
			if err != nil {
				continue
			}

			// Get all active members to broadcast
			memberIDs, err := h.getGroupMemberIDs(msg.RoomID)
			if err != nil {
				continue
			}

			out := OutgoingMessage{
				ID:       savedMsg.ID,
				Type:     "group_message",
				FromUser: c.userID,
				RoomID:   msg.RoomID,
				Content:  msg.Content,
				ParentMessageID: func() string {
					if savedMsg.ParentMessageID == nil {
						return ""
					}
					return strings.TrimSpace(*savedMsg.ParentMessageID)
				}(),
				ParentSequenceID: savedMsg.ParentSequenceID,
				Timestamp:        savedMsg.CreatedAt.Unix(),
				SequenceID:       savedMsg.SequenceID,
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
				savedMsg, err := h.saveAttachmentMessage(roomID, c.userID, attachment.ID, payloadJSON, contentType, strings.TrimSpace(msg.ParentMessageID), msg.ParentSequenceID)
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
					ParentMessageID: func() string {
						if savedMsg.ParentMessageID == nil {
							return ""
						}
						return strings.TrimSpace(*savedMsg.ParentMessageID)
					}(),
					ParentSequenceID: savedMsg.ParentSequenceID,
					Timestamp:        savedMsg.CreatedAt.Unix(),
					SequenceID:       savedMsg.SequenceID,
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
				savedMsg, err := h.saveAttachmentMessage(msg.RoomID, c.userID, attachment.ID, payloadJSON, contentType, strings.TrimSpace(msg.ParentMessageID), msg.ParentSequenceID)
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
					ParentMessageID: func() string {
						if savedMsg.ParentMessageID == nil {
							return ""
						}
						return strings.TrimSpace(*savedMsg.ParentMessageID)
					}(),
					ParentSequenceID: savedMsg.ParentSequenceID,
					Timestamp:        savedMsg.CreatedAt.Unix(),
					SequenceID:       savedMsg.SequenceID,
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
				log.Printf("ws write closed user=%s", c.userID)
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				if ce, ok := err.(*websocket.CloseError); ok {
					log.Printf("ws write close user=%s code=%d text=%s", c.userID, ce.Code, ce.Text)
				} else {
					log.Printf("ws write error user=%s err=%v", c.userID, err)
				}
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				if ce, ok := err.(*websocket.CloseError); ok {
					log.Printf("ws ping close user=%s code=%d text=%s", c.userID, ce.Code, ce.Text)
				} else {
					log.Printf("ws ping error user=%s err=%v", c.userID, err)
				}
				return
			}
		}
	}
}

func (h *WsHandler) areFriends(a, b string) bool {
	if a == "" || b == "" || a == b {
		return false
	}
	if h.cacheClient != nil {
		cacheKey := h.cacheClient.BuildKey(cachepkg.FriendshipKey(a, b)...)
		cacheCtx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
		val, ok, err := h.cacheClient.GetString(cacheCtx, cacheKey)
		cancel()
		if err == nil && ok {
			return val == "1"
		}
	}
	x, y := a, b
	if x > y {
		x, y = y, x
	}
	var f models.Friendship
	if err := h.db.Where("user_id_a = ? AND user_id_b = ?", x, y).First(&f).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) && h.cacheClient != nil {
			cacheKey := h.cacheClient.BuildKey(cachepkg.FriendshipKey(a, b)...)
			cacheCtx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
			_ = h.cacheClient.SetString(cacheCtx, cacheKey, "0", cachepkg.FriendshipTTL)
			cancel()
		}
		return false
	}
	if h.cacheClient != nil {
		cacheKey := h.cacheClient.BuildKey(cachepkg.FriendshipKey(a, b)...)
		cacheCtx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
		_ = h.cacheClient.SetString(cacheCtx, cacheKey, "1", cachepkg.FriendshipTTL)
		cancel()
	}
	return true
}

func (h *WsHandler) ensureDirectRoom(a, b string) (string, error) {
	x, y := a, b
	if x > y {
		x, y = y, x
	}
	name := x + ":" + y
	if h.cacheClient != nil {
		cacheKey := h.cacheClient.BuildKey(cachepkg.DirectRoomKey(a, b)...)
		cacheCtx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
		roomID, ok, err := h.cacheClient.GetString(cacheCtx, cacheKey)
		cancel()
		if err == nil && ok && strings.TrimSpace(roomID) != "" {
			var cachedRoom models.Room
			roomErr := h.db.Select("id").Where("id = ? AND room_type = ? AND name = ?", roomID, models.RoomTypeDirect, name).First(&cachedRoom).Error
			if roomErr == nil {
				h.ensureDirectRoomMembers(roomID, a, b)
				return roomID, nil
			}
			if errors.Is(roomErr, gorm.ErrRecordNotFound) {
				cacheCtx, delCancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
				_ = h.cacheClient.Del(cacheCtx, cacheKey)
				delCancel()
			} else {
				return "", roomErr
			}
		}
	}
	var room models.Room
	if err := h.db.Where("room_type = ? AND name = ?", models.RoomTypeDirect, name).First(&room).Error; err == nil {
		if h.cacheClient != nil {
			cacheKey := h.cacheClient.BuildKey(cachepkg.DirectRoomKey(a, b)...)
			cacheCtx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
			_ = h.cacheClient.SetString(cacheCtx, cacheKey, room.ID, cachepkg.DirectRoomTTL)
			cancel()
		}
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
	if h.cacheClient != nil {
		cacheKey := h.cacheClient.BuildKey(cachepkg.DirectRoomKey(a, b)...)
		cacheCtx, delCancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
		_ = h.cacheClient.Del(cacheCtx, cacheKey)
		delCancel()
		cacheCtx, setCancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
		_ = h.cacheClient.SetString(cacheCtx, cacheKey, room.ID, cachepkg.DirectRoomTTL)
		setCancel()
	}
	h.ensureDirectRoomMembers(room.ID, a, b)
	return room.ID, nil
}

type groupPostingPermissionCache struct {
	LeftAtUnix    int64 `json:"left_at_unix"`
	MuteUntilUnix int64 `json:"mute_until_unix"`
}

func (h *WsHandler) checkGroupPostingPermission(roomID, userID string) error {
	now := time.Now()
	if h.cacheClient != nil {
		cacheKey := h.cacheClient.BuildKey(cachepkg.GroupPostingPermissionKey(roomID, userID)...)
		cacheCtx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
		var cached groupPostingPermissionCache
		ok, err := h.cacheClient.GetJSON(cacheCtx, cacheKey, &cached)
		cancel()
		if err == nil && ok {
			if cached.LeftAtUnix > 0 {
				return errors.New("user has left the group")
			}
			if cached.MuteUntilUnix > now.Unix() {
				return errors.New("user is muted")
			}
			return nil
		}
	}
	var member models.RoomMember
	err := h.db.Where("room_id = ? AND user_id = ?", roomID, userID).First(&member).Error
	if err != nil {
		return err
	}
	cached := groupPostingPermissionCache{}
	if member.LeftAt != nil {
		cached.LeftAtUnix = member.LeftAt.Unix()
	}
	if member.MuteUntil != nil {
		cached.MuteUntilUnix = member.MuteUntil.Unix()
	}
	if h.cacheClient != nil {
		cacheKey := h.cacheClient.BuildKey(cachepkg.GroupPostingPermissionKey(roomID, userID)...)
		cacheCtx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
		_ = h.cacheClient.SetJSON(cacheCtx, cacheKey, cached, cachepkg.GroupPostingPermissionTTL)
		cancel()
	}
	if member.LeftAt != nil {
		return errors.New("user has left the group")
	}
	if member.MuteUntil != nil && member.MuteUntil.After(now) {
		return errors.New("user is muted")
	}
	return nil
}

func (h *WsHandler) getGroupMemberIDs(roomID string) ([]string, error) {
	if h.cacheClient != nil {
		cacheKey := h.cacheClient.BuildKey(cachepkg.GroupMemberIDsKey(roomID)...)
		cacheCtx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
		var cached []string
		ok, err := h.cacheClient.GetJSON(cacheCtx, cacheKey, &cached)
		cancel()
		if err == nil && ok {
			return cached, nil
		}
	}
	var userIDs []string
	err := h.db.Model(&models.RoomMember{}).
		Where("room_id = ? AND left_at IS NULL", roomID).
		Pluck("user_id", &userIDs).Error
	if err == nil && h.cacheClient != nil {
		cacheKey := h.cacheClient.BuildKey(cachepkg.GroupMemberIDsKey(roomID)...)
		cacheCtx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
		_ = h.cacheClient.SetJSON(cacheCtx, cacheKey, userIDs, cachepkg.GroupMemberIDsTTL)
		cancel()
	}
	return userIDs, err
}

func (h *WsHandler) ensureRobotUser() error {
	h.robotInit.Do(func() {
		var existing models.User
		if err := h.db.Where("id = ?", wsRobotUserID).First(&existing).Error; err == nil {
			return
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			h.robotErr = err
			return
		}
		now := time.Now()
		displayName := "Robot"
		robot := models.User{
			ID:           wsRobotUserID,
			Username:     "__ququchat_robot__",
			PasswordHash: "__robot__",
			Status:       "active",
			DisplayName:  &displayName,
			CreatedAt:    now,
			UpdatedAt:    now,
		}
		h.robotErr = h.db.Create(&robot).Error
	})
	return h.robotErr
}

func (h *WsHandler) StartTaskDoneConsumer(ctx context.Context) error {
	if h == nil || h.taskService == nil {
		return nil
	}
	h.doneConsumerMu.Lock()
	defer h.doneConsumerMu.Unlock()
	if h.doneConsumerUp {
		return h.doneConsumerErr
	}
	h.doneConsumerErr = h.taskService.StartDoneEventConsumer(ctx, func(handlerCtx context.Context, event taskservice.DoneEvent) error {
		eventType := strings.TrimSpace(event.EventType)
		if eventType == "agent.step" {
			h.publishAgentStreamEvent(taskservice.AgentStreamEvent{
				EventType: "agent.step",
				RequestID: strings.TrimSpace(event.RequestID),
				TaskID:    strings.TrimSpace(event.TaskID),
				RoomID:    strings.TrimSpace(event.RoomID),
				UserID:    strings.TrimSpace(event.UserID),
				Step:      event.Step,
				Role:      strings.TrimSpace(event.Role),
				Tool:      strings.TrimSpace(event.Tool),
				Status:    strings.TrimSpace(string(event.Status)),
				Content:   strings.TrimSpace(event.Content),
				Error:     strings.TrimSpace(event.ErrorMessage),
				TokenUsage: map[string]int{
					"prompt_tokens":     event.PromptTokens,
					"completion_tokens": event.CompletionTokens,
					"total_tokens":      event.TotalTokens,
				},
				ParentMessageID:  strings.TrimSpace(event.ParentMessageID),
				ParentSequenceID: event.ParentSequenceID,
			})
			return nil
		}
		replyText := strings.TrimSpace(event.Final)
		if replyText == "" {
			replyText = strings.TrimSpace(event.ErrorMessage)
		}
		if replyText == "" && event.Status == tasksvc.StatusFailed {
			replyText = "对话失败"
		}
		if strings.TrimSpace(replyText) == "" || strings.TrimSpace(event.RoomID) == "" {
			return nil
		}
		if eventType == "" {
			for _, line := range splitMemoryLines(event.Payload) {
				h.publishAgentStreamEvent(taskservice.AgentStreamEvent{
					EventType:        "agent.step",
					RequestID:        strings.TrimSpace(event.RequestID),
					TaskID:           strings.TrimSpace(event.TaskID),
					RoomID:           strings.TrimSpace(event.RoomID),
					UserID:           strings.TrimSpace(event.UserID),
					Status:           string(event.Status),
					Content:          line,
					ParentMessageID:  strings.TrimSpace(event.ParentMessageID),
					ParentSequenceID: event.ParentSequenceID,
				})
			}
		}
		doneType := "agent.done"
		if event.Status == tasksvc.StatusFailed {
			doneType = "agent.error"
		}
		h.publishAgentStreamEvent(taskservice.AgentStreamEvent{
			EventType:        doneType,
			RequestID:        strings.TrimSpace(event.RequestID),
			TaskID:           strings.TrimSpace(event.TaskID),
			RoomID:           strings.TrimSpace(event.RoomID),
			UserID:           strings.TrimSpace(event.UserID),
			Status:           string(event.Status),
			Content:          strings.TrimSpace(event.Final),
			Error:            strings.TrimSpace(event.ErrorMessage),
			Payload:          clonePayloadForStream(event.Payload),
			ParentMessageID:  strings.TrimSpace(event.ParentMessageID),
			ParentSequenceID: event.ParentSequenceID,
		})
		parentSequenceID := event.ParentSequenceID
		return h.sendRobotGroupMessage(event.RoomID, replyText, event.Payload, event.ParentMessageID, &parentSequenceID)
	})
	if h.doneConsumerErr == nil {
		h.doneConsumerUp = true
	}
	return h.doneConsumerErr
}

func (h *WsHandler) sendRobotGroupMessage(roomID, content string, payload map[string]interface{}, parentMessageID string, parentSequenceID *int64) error {
	text := strings.TrimSpace(content)
	if roomID == "" || text == "" {
		return nil
	}
	if err := h.ensureRobotUser(); err != nil {
		return err
	}
	payloadJSON, err := toJSONPayload(payload)
	if err != nil {
		return err
	}
	savedMsg, err := h.saveMessage(roomID, wsRobotUserID, models.ContentTypeText, &text, nil, payloadJSON, strings.TrimSpace(parentMessageID), parentSequenceID)
	if err != nil {
		return err
	}
	memberIDs, err := h.getGroupMemberIDs(roomID)
	if err != nil {
		return err
	}
	out := OutgoingMessage{
		ID:          savedMsg.ID,
		Type:        "group_message",
		FromUser:    wsRobotUserID,
		RoomID:      roomID,
		Content:     text,
		PayloadJSON: payloadJSON,
		ParentMessageID: func() string {
			if savedMsg.ParentMessageID == nil {
				return ""
			}
			return strings.TrimSpace(*savedMsg.ParentMessageID)
		}(),
		ParentSequenceID: savedMsg.ParentSequenceID,
		Timestamp:        savedMsg.CreatedAt.Unix(),
		SequenceID:       savedMsg.SequenceID,
	}
	b, err := json.Marshal(out)
	if err != nil {
		return err
	}
	h.hub.broadcast <- GroupMessage{
		RoomID:  roomID,
		UserIDs: memberIDs,
		Data:    b,
	}
	return nil
}

func (h *WsHandler) publishAgentStreamEvent(event taskservice.AgentStreamEvent) {
	if h == nil || h.streamHub == nil {
		return
	}
	h.streamHub.Publish(event)
}

func splitMemoryLines(payload map[string]interface{}) []string {
	if payload == nil {
		return nil
	}
	rawMemory, ok := payload["memory"]
	if !ok {
		return nil
	}
	memoryText := strings.TrimSpace(fmt.Sprint(rawMemory))
	if memoryText == "" {
		return nil
	}
	lines := strings.Split(memoryText, "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		result = append(result, trimmed)
	}
	return result
}

func clonePayloadForStream(payload map[string]interface{}) map[string]interface{} {
	if payload == nil {
		return nil
	}
	b, err := json.Marshal(payload)
	if err != nil {
		cloned := make(map[string]interface{}, len(payload))
		for k, v := range payload {
			cloned[k] = v
		}
		return cloned
	}
	var cloned map[string]interface{}
	if err := json.Unmarshal(b, &cloned); err != nil {
		cloned = make(map[string]interface{}, len(payload))
		for k, v := range payload {
			cloned[k] = v
		}
	}
	return cloned
}

func (h *WsHandler) sendAgentCommandAck(userID, roomID, requestID, taskID, parentMessageID string, parentSequenceID int64) {
	if h == nil || h.hub == nil {
		return
	}
	ack := AgentCommandAck{
		Type:             "agent_command_ack",
		RequestID:        strings.TrimSpace(requestID),
		TaskID:           strings.TrimSpace(taskID),
		RoomID:           strings.TrimSpace(roomID),
		ParentMessageID:  strings.TrimSpace(parentMessageID),
		ParentSequenceID: parentSequenceID,
	}
	b, err := json.Marshal(ack)
	if err != nil {
		return
	}
	h.hub.direct <- DirectMessage{
		FromUserID: wsRobotUserID,
		ToUserID:   strings.TrimSpace(userID),
		Data:       b,
	}
}

func toJSONPayload(payload map[string]interface{}) (datatypes.JSON, error) {
	if len(payload) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return datatypes.JSON(b), nil
}

func (h *WsHandler) saveGroupMessage(roomID, fromUserID, content string, parentMessageID string, parentSequenceID *int64) (*models.Message, error) {
	text := content
	return h.saveMessage(roomID, fromUserID, models.ContentTypeText, &text, nil, nil, parentMessageID, parentSequenceID)
}

func (h *WsHandler) saveDirectMessage(roomID, fromUserID, content string, parentMessageID string, parentSequenceID *int64) (*models.Message, error) {
	text := content
	return h.saveMessage(roomID, fromUserID, models.ContentTypeText, &text, nil, nil, parentMessageID, parentSequenceID)
}

func (h *WsHandler) saveAttachmentMessage(roomID, fromUserID, attachmentID string, payload datatypes.JSON, contentType models.ContentType, parentMessageID string, parentSequenceID *int64) (*models.Message, error) {
	aid := attachmentID
	return h.saveMessage(roomID, fromUserID, contentType, nil, &aid, payload, parentMessageID, parentSequenceID)
}

func (h *WsHandler) saveMessage(roomID, fromUserID string, contentType models.ContentType, contentText *string, attachmentID *string, payload datatypes.JSON, parentMessageID string, parentSequenceID *int64) (*models.Message, error) {
	now := time.Now()
	trimmedParentMessageID := strings.TrimSpace(parentMessageID)

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
			resolvedParentMessageID, resolvedParentSequenceID, resolveErr := h.resolveParentReference(tx, roomID, trimmedParentMessageID, parentSequenceID)
			if resolveErr != nil {
				return resolveErr
			}
			m.ParentMessageID = resolvedParentMessageID
			m.ParentSequenceID = resolvedParentSequenceID
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

func (h *WsHandler) resolveParentReference(tx *gorm.DB, roomID string, parentMessageID string, parentSequenceID *int64) (*string, *int64, error) {
	trimmedParentMessageID := strings.TrimSpace(parentMessageID)
	hasParentSequence := parentSequenceID != nil && *parentSequenceID > 0
	if trimmedParentMessageID == "" && !hasParentSequence {
		return nil, nil, nil
	}
	var parent models.Message
	if trimmedParentMessageID != "" {
		if err := tx.Where("id = ?", trimmedParentMessageID).First(&parent).Error; err != nil {
			return nil, nil, err
		}
	} else {
		if err := tx.Where("room_id = ? AND sequence_id = ?", roomID, *parentSequenceID).First(&parent).Error; err != nil {
			return nil, nil, err
		}
	}
	if strings.TrimSpace(parent.RoomID) != strings.TrimSpace(roomID) {
		return nil, nil, errors.New("parent message does not belong to room")
	}
	if hasParentSequence && parent.SequenceID != *parentSequenceID {
		return nil, nil, errors.New("parent sequence does not match parent message")
	}
	parentID := strings.TrimSpace(parent.ID)
	parentSeq := parent.SequenceID
	return &parentID, &parentSeq, nil
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
