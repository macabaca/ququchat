package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"gorm.io/gorm"

	"ququchat/internal/models"
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
}

type DirectMessage struct {
	FromUserID string
	ToUserID   string
	Data       []byte
}

func NewHub() *Hub {
	h := &Hub{
		clients:       make(map[*Client]bool),
		clientsByUser: make(map[string]map[*Client]bool),
		register:      make(chan *Client),
		unregister:    make(chan *Client),
		direct:        make(chan DirectMessage),
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
	Type    string `json:"type"`
	ToUser  string `json:"to_user_id,omitempty"`
	Content string `json:"content,omitempty"`
}

type OutgoingMessage struct {
	Type      string `json:"type"`
	FromUser  string `json:"from_user_id"`
	ToUser    string `json:"to_user_id"`
	Content   string `json:"content"`
	Timestamp int64  `json:"timestamp"`
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
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
		var msg IncomingMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		if msg.Type != "friend_message" || msg.ToUser == "" || msg.Content == "" {
			continue
		}
		if !h.areFriends(c.userID, msg.ToUser) {
			continue
		}
		roomID, err := h.ensureDirectRoom(c.userID, msg.ToUser)
		if err != nil {
			continue
		}
		h.saveDirectMessage(roomID, c.userID, msg.Content)
		out := OutgoingMessage{
			Type:      "friend_message",
			FromUser:  c.userID,
			ToUser:    msg.ToUser,
			Content:   msg.Content,
			Timestamp: time.Now().Unix(),
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
	}
}

func (c *Client) writeLoop() {
	defer func() {
		_ = c.conn.Close()
	}()
	for msg := range c.send {
		if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			break
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

func (h *WsHandler) saveDirectMessage(roomID, fromUserID, content string) {
	now := time.Now()
	text := content
	m := models.Message{
		ID:          uuid.NewString(),
		RoomID:      roomID,
		SenderID:    &fromUserID,
		ContentType: models.ContentTypeText,
		ContentText: &text,
		CreatedAt:   now,
	}
	_ = h.db.Create(&m).Error
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
