# Redis 缓存键值规范（最小版）

## 1. 统一约定

- key 前缀：`ququchat`
- 分隔符：`:`
- 推荐完整 key 形态：`ququchat:{domain}:{id...}`
- 缓存策略：优先使用 Cache-Aside（先读缓存，miss 回源 DB，再回填缓存）

---

## 2. areFriends

### 2.1 key

- 逻辑函数：`FriendshipKey(userA, userB)`
- key 结构：`ququchat:friendship:{minUserID}:{maxUserID}`
- 说明：用户 ID 先按字典序排序，避免同一对用户出现两份缓存

### 2.2 value

- 类型：字符串
- 取值：
  - `"1"`：是好友
  - `"0"`：不是好友

### 2.3 TTL

- 常量：`FriendshipTTL`
- 默认值：`10m`

### 2.4 失效时机

- 接受好友请求（`/api/friends/requests/respond` 且 `action=accept`）后：删除该用户对 key
- 删除好友（`/api/friends/remove`）成功后：删除该用户对 key
- 仅发送好友请求（`/api/friends/add`）时：不删除该 key（因为尚未产生 Friendship 记录）
- 拒绝好友请求（`/api/friends/requests/respond` 且 `action=reject`）时：不删除该 key（关系状态未改变）
- 数据库层面对 Friendship 的人工修复、脚本导入导出、后台管理改动后：按用户对批量删除对应 key

### 2.5 与后端现状对应

- Friendship 新增发生在：接受好友请求事务中创建 `models.Friendship`
- Friendship 删除发生在：删除好友接口中删除 `models.Friendship`
- areFriends 判定来源：WebSocket 发私聊/文件前的 `areFriends` 查询

---

## 3. checkGroupPostingPermission

### 3.1 key

- 逻辑函数：`GroupPostingPermissionKey(roomID, userID)`
- key 结构：`ququchat:group_posting_permission:{roomID}:{userID}`

### 3.2 value

- 类型：JSON 字符串
- 推荐结构：

```json
{
  "allowed": true,
  "role": "member",
  "left_at_unix": 0,
  "mute_until_unix": 0
}
```

### 3.3 TTL

- 常量：`GroupPostingPermissionTTL`
- 默认值：`2m`

### 3.4 失效时机

- 成员被移除/退群后：删除对应 `roomID + userID` key
- 成员解除禁言/设置禁言后：删除对应 `roomID + userID` key
- 群解散后：删除该群下相关 key

---

## 4. getGroupMemberIDs

### 4.1 key

- 逻辑函数：`GroupMemberIDsKey(roomID)`
- key 结构：`ququchat:group_member_ids:{roomID}`

### 4.2 value

- 类型：JSON 数组
- 示例：

```json
["u1", "u2", "u3"]
```

### 4.3 TTL

- 常量：`GroupMemberIDsTTL`
- 默认值：`2m`

### 4.4 失效时机

- 拉人/踢人/退群：删除 `roomID` 对应 key
- 群解散：删除 `roomID` 对应 key

---

## 5. 直聊房间 ID 缓存（ensureDirectRoom）

### 5.1 现状

- 每次私聊都会按 `room_type + name` 查询直聊房间（`ensureDirectRoom`）

### 5.2 key

- key 结构：`ququchat:direct_room:{minUserID}:{maxUserID}`
- 说明：用户 ID 先按字典序排序，避免同一对用户出现两份缓存

### 5.3 value

- 类型：字符串
- 取值：`roomID`

### 5.4 TTL

- 建议：`24h`

### 5.5 失效时机

- 直聊房间被删除时：删除该用户对 key
- 直聊房间被重建时：删除旧 key 后写入新 roomID

---

## 6. 好友列表缓存（listFriendIDs）

### 6.1 现状

- 用户上下线通知时，会查询用户全量好友（`listFriendIDs`）

### 6.2 key

- key 结构：`ququchat:friend_ids:{userID}`

### 6.3 value

- 类型：JSON 数组
- 示例：

```json
["u2", "u3", "u9"]
```

### 6.4 TTL

- 建议：`10m ~ 30m`

### 6.5 失效时机

- 好友关系新增/删除时：双向删除
  - 删除 `ququchat:friend_ids:{userA}`
  - 删除 `ququchat:friend_ids:{userB}`

---

## 7. 最小调用示例

```go
ctx := context.Background()
rc := cache.NewRedisClient(cache.RedisOptions{
  Addr:      "127.0.0.1:6379",
  DB:        0,
  KeyPrefix: cache.DefaultKeyPrefix,
})

if err := rc.Ping(ctx); err != nil {
  return
}

parts := cache.FriendshipKey("u100", "u200")
key := rc.BuildKey(parts...)

value, ok, err := rc.GetString(ctx, key)
if err == nil && ok {
  _ = value
}
```
