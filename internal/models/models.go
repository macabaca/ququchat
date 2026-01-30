package models

import (
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type RoomType string

const (
	RoomTypeGroup  RoomType = "group"
	RoomTypeDirect RoomType = "direct"
)

type MemberRole string

const (
	MemberRoleOwner  MemberRole = "owner"
	MemberRoleAdmin  MemberRole = "admin"
	MemberRoleMember MemberRole = "member"
)

type FriendRequestStatus string

const (
	FriendRequestPending  FriendRequestStatus = "pending"
	FriendRequestAccepted FriendRequestStatus = "accepted"
	FriendRequestRejected FriendRequestStatus = "rejected"
	FriendRequestCanceled FriendRequestStatus = "canceled"
)

type ContentType string

const (
	ContentTypeText   ContentType = "text"
	ContentTypeImage  ContentType = "image"
	ContentTypeFile   ContentType = "file"
	ContentTypeSystem ContentType = "system"
)

// Users
// 使用字符串 UUID 作为主键，可在应用层或数据库默认生成
// 如 Postgres 可使用: gorm:"type:uuid;default:gen_random_uuid()"
type User struct {
	ID           string    `gorm:"type:char(36);primaryKey" json:"id"`
	UserCode     int64     `gorm:"autoIncrement;uniqueIndex;not null" json:"user_code"`
	Username     string    `gorm:"size:64;uniqueIndex;not null" json:"username"`
	Email        *string   `gorm:"size:255;uniqueIndex" json:"email,omitempty"`
	Phone        *string   `gorm:"size:32;uniqueIndex" json:"phone,omitempty"`
	PasswordHash string    `gorm:"size:255;not null" json:"-"`
	Status       string    `gorm:"size:16;not null;default:active" json:"status"`
	DisplayName  *string   `gorm:"size:64" json:"display_name,omitempty"`
	AvatarURL    *string   `gorm:"size:512" json:"avatar_url,omitempty"`
	Bio          *string   `gorm:"size:1024" json:"bio,omitempty"`
	CreatedAt    time.Time `gorm:"not null" json:"created_at"`
	UpdatedAt    time.Time `gorm:"not null" json:"updated_at"`
}

// 登录会话/令牌
// RefreshToken 可以唯一索引以便快速撤销
// 建立外键到 users
// 若使用 JWT，可只存刷新令牌持久化
// Note: 关联字段使用指针以避免多余加载
type AuthSession struct {
	ID           string     `gorm:"type:char(36);primaryKey" json:"id"`
	UserID       string     `gorm:"type:char(36);not null;index" json:"user_id"`
	User         *User      `gorm:"foreignKey:UserID;constraint:OnUpdate:CASCADE,OnDelete:CASCADE" json:"-"`
	RefreshToken *string    `gorm:"size:255;uniqueIndex" json:"refresh_token,omitempty"`
	ExpiresAt    time.Time  `gorm:"not null" json:"expires_at"`
	RevokedAt    *time.Time `json:"revoked_at,omitempty"`
	IP           *string    `gorm:"size:64" json:"ip,omitempty"`
	UserAgent    *string    `gorm:"size:256" json:"user_agent,omitempty"`
	CreatedAt    time.Time  `gorm:"not null" json:"created_at"`
}

// 好友请求
// 使用三列唯一索引 (from, to, status) 以兼容多数据库
// 若使用 Postgres，可在迁移中改为部分唯一索引 (status='pending')
type FriendRequest struct {
	ID          string              `gorm:"type:char(36);primaryKey" json:"id"`
	FromUserID  string              `gorm:"type:char(36);not null;uniqueIndex:uidx_friend_req_triple,priority:1" json:"from_user_id"`
	ToUserID    string              `gorm:"type:char(36);not null;uniqueIndex:uidx_friend_req_triple,priority:2" json:"to_user_id"`
	Status      FriendRequestStatus `gorm:"type:varchar(16);not null;uniqueIndex:uidx_friend_req_triple,priority:3" json:"status"`
	FromUser    *User               `gorm:"foreignKey:FromUserID;constraint:OnDelete:CASCADE" json:"-"`
	ToUser      *User               `gorm:"foreignKey:ToUserID;constraint:OnDelete:CASCADE" json:"-"`
	Message     *string             `gorm:"size:512" json:"message,omitempty"`
	CreatedAt   time.Time           `gorm:"not null" json:"created_at"`
	RespondedAt *time.Time          `json:"responded_at,omitempty"`
}

// 好友关系（无向边），唯一对 (user_id_a, user_id_b)
// a<b 约束由业务层保证（或在迁移中加 CHECK）
type Friendship struct {
	ID        string    `gorm:"type:char(36);primaryKey" json:"id"`
	UserIDA   string    `gorm:"type:char(36);not null;uniqueIndex:uidx_friend_pair,priority:1" json:"user_id_a"`
	UserIDB   string    `gorm:"type:char(36);not null;uniqueIndex:uidx_friend_pair,priority:2" json:"user_id_b"`
	UserA     *User     `gorm:"foreignKey:UserIDA;constraint:OnDelete:CASCADE" json:"-"`
	UserB     *User     `gorm:"foreignKey:UserIDB;constraint:OnDelete:CASCADE" json:"-"`
	CreatedAt time.Time `gorm:"not null" json:"created_at"`
}

// 拉黑关系，(user_id, blocked_user_id) 唯一
type Block struct {
	ID            string    `gorm:"type:char(36);primaryKey" json:"id"`
	UserID        string    `gorm:"type:char(36);not null;uniqueIndex:uidx_block_pair,priority:1" json:"user_id"`
	BlockedUserID string    `gorm:"type:char(36);not null;uniqueIndex:uidx_block_pair,priority:2" json:"blocked_user_id"`
	User          *User     `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"-"`
	BlockedUser   *User     `gorm:"foreignKey:BlockedUserID;constraint:OnDelete:CASCADE" json:"-"`
	CreatedAt     time.Time `gorm:"not null" json:"created_at"`
}

// 房间，采用软删除；群聊/单聊通过 RoomType 区分
type Room struct {
	ID          string         `gorm:"type:char(36);primaryKey" json:"id"`
	RoomType    RoomType       `gorm:"type:varchar(16);not null;index" json:"room_type"`
	Name        string         `gorm:"size:128;not null" json:"name"`
	OwnerUserID string         `gorm:"type:char(36);not null;index" json:"owner_user_id"`
	OwnerUser   *User          `gorm:"foreignKey:OwnerUserID;constraint:OnDelete:CASCADE" json:"-"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
	CreatedAt   time.Time      `gorm:"not null" json:"created_at"`
	UpdatedAt   time.Time      `gorm:"not null" json:"updated_at"`
}

// 房间成员，复合主键 (room_id, user_id)
type RoomMember struct {
	RoomID         string     `gorm:"type:char(36);primaryKey" json:"room_id"`
	UserID         string     `gorm:"type:char(36);primaryKey" json:"user_id"`
	Room           *Room      `gorm:"foreignKey:RoomID;constraint:OnDelete:CASCADE" json:"-"`
	User           *User      `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"-"`
	Role           MemberRole `gorm:"type:varchar(16);not null;default:member" json:"role"`
	JoinedAt       time.Time  `gorm:"not null" json:"joined_at"`
	LeftAt         *time.Time `json:"left_at,omitempty"`
	InviteBy       *string    `gorm:"type:char(36)" json:"invite_by,omitempty"`
	MuteUntil      *time.Time `json:"mute_until,omitempty"`
	NicknameInRoom *string    `gorm:"size:64" json:"nickname_in_room,omitempty"`
}

// 消息，采用软删除；Payload 使用 GORM datatypes.JSON
// 复合唯一索引：(room_id, sequence_id) 保证房间内消息序号唯一且单调递增
// 辅助索引：(room_id, created_at) 用于基于时间的时间轴查询
type Message struct {
	ID              string         `gorm:"type:char(36);primaryKey" json:"id"`
	RoomID          string         `gorm:"type:char(36);not null;uniqueIndex:uidx_room_seq,priority:1;index:idx_room_created_at,priority:1" json:"room_id"`
	Room            *Room          `gorm:"foreignKey:RoomID;constraint:OnDelete:CASCADE" json:"-"`
	SenderID        *string        `gorm:"type:char(36);index" json:"sender_id,omitempty"`
	Sender          *User          `gorm:"foreignKey:SenderID;constraint:OnDelete:SET NULL" json:"-"`
	ContentType     ContentType    `gorm:"type:varchar(16);not null" json:"content_type"`
	ContentText     *string        `gorm:"type:text" json:"content_text,omitempty"`
	PayloadJSON     datatypes.JSON `gorm:"type:json" json:"payload_json,omitempty"`
	AttachmentID    *string        `gorm:"type:char(36)" json:"attachment_id,omitempty"`
	ParentMessageID *string        `gorm:"type:char(36);index" json:"parent_message_id,omitempty"`
	// SequenceID 房间内单调递增的序号，从1开始
	SequenceID int64          `gorm:"not null;uniqueIndex:uidx_room_seq,priority:2,sort:desc" json:"sequence_id"`
	CreatedAt  time.Time      `gorm:"not null;index:idx_room_created_at,priority:2,sort:desc" json:"created_at"`
	UpdatedAt  *time.Time     `json:"updated_at,omitempty"`
	DeletedAt  gorm.DeletedAt `gorm:"index" json:"deleted_at,omitempty"`
}

// 送达/已读回执，复合主键 (message_id, user_id)
type MessageReceipt struct {
	MessageID   string     `gorm:"type:char(36);primaryKey" json:"message_id"`
	UserID      string     `gorm:"type:char(36);primaryKey;index" json:"user_id"`
	Message     *Message   `gorm:"foreignKey:MessageID;constraint:OnDelete:CASCADE" json:"-"`
	User        *User      `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"-"`
	DeliveredAt *time.Time `json:"delivered_at,omitempty"`
	ReadAt      *time.Time `json:"read_at,omitempty"`
}

// 消息表情，唯一约束 (message_id, user_id, emoji)
type MessageReaction struct {
	MessageID string    `gorm:"type:char(36);not null;uniqueIndex:uidx_msg_reaction,priority:1" json:"message_id"`
	UserID    string    `gorm:"type:char(36);not null;uniqueIndex:uidx_msg_reaction,priority:2" json:"user_id"`
	Emoji     string    `gorm:"size:32;not null;uniqueIndex:uidx_msg_reaction,priority:3" json:"emoji"`
	Message   *Message  `gorm:"foreignKey:MessageID;constraint:OnDelete:CASCADE" json:"-"`
	User      *User     `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"-"`
	CreatedAt time.Time `gorm:"not null" json:"created_at"`
}

// 附件元数据
type Attachment struct {
	ID              string    `gorm:"type:char(36);primaryKey" json:"id"`
	UploaderUserID  *string   `gorm:"type:char(36);index" json:"uploader_user_id,omitempty"`
	UploaderUser    *User     `gorm:"foreignKey:UploaderUserID;constraint:OnDelete:SET NULL" json:"-"`
	URL             *string   `gorm:"size:512" json:"url,omitempty"`
	StorageKey      *string   `gorm:"size:512" json:"storage_key,omitempty"`
	MimeType        *string   `gorm:"size:128" json:"mime_type,omitempty"`
	SizeBytes       *int64    `json:"size_bytes,omitempty"`
	Hash            *string   `gorm:"size:128" json:"hash,omitempty"`
	StorageProvider *string   `gorm:"size:64" json:"storage_provider,omitempty"`
	CreatedAt       time.Time `gorm:"not null" json:"created_at"`
}
