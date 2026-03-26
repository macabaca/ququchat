package cache

import (
	"strings"
	"time"
)

const DefaultKeyPrefix = "ququchat"

const (
	FriendshipTTL             = 10 * time.Minute
	GroupPostingPermissionTTL = 2 * time.Minute
	GroupMemberIDsTTL         = 2 * time.Minute
	DirectRoomTTL             = 24 * time.Hour
	FriendIDsTTL              = 15 * time.Minute
)

func FriendshipKey(userA string, userB string) []string {
	a := strings.TrimSpace(userA)
	b := strings.TrimSpace(userB)
	if a > b {
		a, b = b, a
	}
	return []string{"friendship", a, b}
}

func GroupPostingPermissionKey(roomID string, userID string) []string {
	return []string{"group_posting_permission", strings.TrimSpace(roomID), strings.TrimSpace(userID)}
}

func GroupMemberIDsKey(roomID string) []string {
	return []string{"group_member_ids", strings.TrimSpace(roomID)}
}

func DirectRoomKey(userA string, userB string) []string {
	a := strings.TrimSpace(userA)
	b := strings.TrimSpace(userB)
	if a > b {
		a, b = b, a
	}
	return []string{"direct_room", a, b}
}

func FriendIDsKey(userID string) []string {
	return []string{"friend_ids", strings.TrimSpace(userID)}
}
