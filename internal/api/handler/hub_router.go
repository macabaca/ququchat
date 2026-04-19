package handler

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	cachepkg "ququchat/internal/server/cache"
)

type HubRouter struct {
	nodeID string
	hub    *Hub
	redis  *cachepkg.RedisClient

	mu                   sync.Mutex
	localUserConnCount   map[string]int
	localRoomMemberCount map[string]int
	localUserRooms       map[string]map[string]bool
}

type routerPayload struct {
	Type         string   `json:"type"`
	OriginNodeID string   `json:"origin_node_id"`
	FromUserID   string   `json:"from_user_id,omitempty"`
	ToUserID     string   `json:"to_user_id,omitempty"`
	RoomID       string   `json:"room_id,omitempty"`
	UserIDs      []string `json:"user_ids,omitempty"`
	Data         []byte   `json:"data"`
}

func NewHubRouter(nodeID string, hub *Hub, redis *cachepkg.RedisClient) *HubRouter {
	return &HubRouter{
		nodeID:               nodeID,
		hub:                  hub,
		redis:                redis,
		localUserConnCount:   make(map[string]int),
		localRoomMemberCount: make(map[string]int),
		localUserRooms:       make(map[string]map[string]bool),
	}
}

func (r *HubRouter) OnConnect(ctx context.Context, userID, connID string, roomIDs []string) {
	r.mu.Lock()
	r.localUserConnCount[userID]++
	if r.localUserRooms[userID] == nil {
		r.localUserRooms[userID] = make(map[string]bool)
	}
	for _, rid := range roomIDs {
		r.localUserRooms[userID][rid] = true
		r.localRoomMemberCount[rid]++
	}
	r.mu.Unlock()

	key := r.redis.BuildKey(cachepkg.WSUserNodesKey(userID)...)
	_ = r.redis.SAdd(ctx, key, r.nodeID)
	connKey := r.redis.BuildKey(cachepkg.WSUserConnsKey(userID)...)
	_ = r.redis.SAdd(ctx, connKey, connID)
	connHubKey := r.redis.BuildKey(cachepkg.WSConnHubKey(connID)...)
	_ = r.redis.SetString(ctx, connHubKey, r.nodeID, 24*time.Hour)
	for _, rid := range roomIDs {
		roomKey := r.redis.BuildKey(cachepkg.WSRoomNodesKey(rid)...)
		_ = r.redis.SAdd(ctx, roomKey, r.nodeID)
	}
}

func (r *HubRouter) OnDisconnect(ctx context.Context, userID, connID string) {
	r.mu.Lock()
	r.localUserConnCount[userID]--
	userGone := r.localUserConnCount[userID] <= 0
	if userGone {
		delete(r.localUserConnCount, userID)
	}
	var goneRooms []string
	if userGone {
		for rid := range r.localUserRooms[userID] {
			r.localRoomMemberCount[rid]--
			if r.localRoomMemberCount[rid] <= 0 {
				delete(r.localRoomMemberCount, rid)
				goneRooms = append(goneRooms, rid)
			}
		}
		delete(r.localUserRooms, userID)
	}
	r.mu.Unlock()

	connKey := r.redis.BuildKey(cachepkg.WSUserConnsKey(userID)...)
	_ = r.redis.SRem(ctx, connKey, connID)
	connHubKey := r.redis.BuildKey(cachepkg.WSConnHubKey(connID)...)
	_ = r.redis.Del(ctx, connHubKey)
	if userGone {
		userNodesKey := r.redis.BuildKey(cachepkg.WSUserNodesKey(userID)...)
		_ = r.redis.SRem(ctx, userNodesKey, r.nodeID)
	}
	for _, rid := range goneRooms {
		roomKey := r.redis.BuildKey(cachepkg.WSRoomNodesKey(rid)...)
		_ = r.redis.SRem(ctx, roomKey, r.nodeID)
	}
}

func (r *HubRouter) RouteDirectMessage(ctx context.Context, fromUserID, toUserID string, data []byte) {
	key := r.redis.BuildKey(cachepkg.WSUserNodesKey(toUserID)...)
	nodes, err := r.redis.SMembers(ctx, key)
	if err != nil || len(nodes) == 0 {
		r.hub.direct <- DirectMessage{FromUserID: fromUserID, ToUserID: toUserID, Data: data}
		return
	}
	localDelivered := false
	for _, n := range nodes {
		if n == r.nodeID {
			r.hub.direct <- DirectMessage{FromUserID: fromUserID, ToUserID: toUserID, Data: data}
			localDelivered = true
		} else {
			r.publish(ctx, n, routerPayload{
				Type: "direct", OriginNodeID: r.nodeID,
				FromUserID: fromUserID, ToUserID: toUserID, Data: data,
			})
		}
	}
	if !localDelivered {
		// also echo to sender on this node if present
		r.hub.direct <- DirectMessage{FromUserID: fromUserID, ToUserID: toUserID, Data: data}
	}
}

func (r *HubRouter) RouteBroadcast(ctx context.Context, roomID string, userIDs []string, data []byte) {
	key := r.redis.BuildKey(cachepkg.WSRoomNodesKey(roomID)...)
	nodes, err := r.redis.SMembers(ctx, key)
	if err != nil || len(nodes) == 0 {
		r.hub.broadcast <- GroupMessage{RoomID: roomID, UserIDs: userIDs, Data: data}
		return
	}
	localSent := false
	for _, n := range nodes {
		if n == r.nodeID {
			r.hub.broadcast <- GroupMessage{RoomID: roomID, UserIDs: userIDs, Data: data}
			localSent = true
		} else {
			r.publish(ctx, n, routerPayload{
				Type: "broadcast", OriginNodeID: r.nodeID,
				RoomID: roomID, UserIDs: userIDs, Data: data,
			})
		}
	}
	if !localSent {
		r.hub.broadcast <- GroupMessage{RoomID: roomID, UserIDs: userIDs, Data: data}
	}
}

func (r *HubRouter) publish(ctx context.Context, targetNode string, p routerPayload) {
	b, err := json.Marshal(p)
	if err != nil {
		return
	}
	channel := cachepkg.WSNodeChannel(r.redis.KeyPrefix(), targetNode)
	if err := r.redis.Publish(ctx, channel, string(b)); err != nil {
		log.Printf("hub_router publish to node=%s err=%v", targetNode, err)
	}
}

func (r *HubRouter) StartSubscriber(ctx context.Context) {
	channel := cachepkg.WSNodeChannel(r.redis.KeyPrefix(), r.nodeID)
	pubsub := r.redis.Subscribe(ctx, channel)
	go func() {
		defer pubsub.Close()
		ch := pubsub.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				var p routerPayload
				if err := json.Unmarshal([]byte(msg.Payload), &p); err != nil {
					continue
				}
				if p.OriginNodeID == r.nodeID {
					continue
				}
				switch p.Type {
				case "direct":
					r.hub.direct <- DirectMessage{FromUserID: p.FromUserID, ToUserID: p.ToUserID, Data: p.Data}
				case "broadcast":
					r.hub.broadcast <- GroupMessage{RoomID: p.RoomID, UserIDs: p.UserIDs, Data: p.Data}
				}
			}
		}
	}()
}
