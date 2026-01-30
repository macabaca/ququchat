# WebSocket 接口文档

本文档总结了 `d:\study\project\ququchat\internal\api\handler\ws_handler.go` 中定义的 WebSocket 实时通信接口及相关错误处理机制。

## 1. 连接建立

- **URL**: `/ws`
- **Protocol**: `ws://` 或 `wss://`
- **鉴权**: 需要在 Query 参数中携带 `token`（JWT Token）。
- **示例 URL**:
  ```
  ws://localhost:8080/ws?token=eyJhbGciOiJIUzI1Ni...
  ```

## 2. 消息协议 (JSON)

所有 WebSocket 消息均为 JSON 格式字符串。

### 2.1 客户端 -> 服务端 (发送消息)

#### A. 发送私聊消息
```json
{
  "type": "friend_message",
  "to_user_id": "target-uuid-string", // 必填，接收者用户ID
  "content": "你好，朋友"              // 必填，消息内容
}
```

#### B. 发送群聊消息
```json
{
  "type": "group_message",
  "room_id": "group-uuid-string",    // 必填，群组ID
  "content": "大家好"                 // 必填，消息内容
}
```

### 2.2 服务端 -> 客户端 (接收消息)

#### A. 接收私聊消息 (或发送确认)
```json
{
  "type": "friend_message",
  "from_user_id": "sender-uuid-string",
  "to_user_id": "receiver-uuid-string",
  "content": "你好，朋友",
  "timestamp": 1698372000 // Unix 时间戳 (秒)
}
```

#### B. 接收群聊消息
```json
{
  "type": "group_message",
  "from_user_id": "sender-uuid-string",
  "room_id": "group-uuid-string",
  "content": "大家好",
  "timestamp": 1698372000 // Unix 时间戳 (秒)
}
```

## 3. 错误码与异常情况总结

WebSocket 的错误处理分为两个阶段：**握手阶段**（HTTP 协议）和**通信阶段**（WebSocket 协议）。

### 3.1 握手阶段 (HTTP Status Codes)

在建立连接时，如果鉴权失败，服务端会返回 HTTP 状态码和 JSON 错误信息。

| HTTP 状态码 | 错误信息 (JSON) | 情况说明 |
| :--- | :--- | :--- |
| **401 Unauthorized** | `{"error": "未登录"}` | 请求中未包含 `user_id` 上下文（通常被中间件拦截前就已失败，但在 handler 内部也有兜底检查）。 |
| **401 Unauthorized** | `{"error": "缺少访问令牌"}` | Query 参数中没有 `token` 字段，且 Header 中也没有 Bearer Token。 |
| **401 Unauthorized** | `{"error": "访问令牌已过期"}` | Token 的有效期已过。 |
| **401 Unauthorized** | `{"error": "访问令牌无效"}` | Token 签名验证失败或格式错误。 |

### 3.2 通信阶段 (Silent Failures)

连接建立成功后，**服务端采用“静默丢弃”（Silent Drop）策略**处理大部分错误。这意味着如果客户端发送了非法消息，服务端通常**不会**返回 JSON 错误包，而是直接忽略该请求。

以下情况会导致消息发送失败且无回执：

| 错误类型 | 具体情况 | 服务端行为 |
| :--- | :--- | :--- |
| **格式错误** | 消息不是合法的 JSON 格式。 | **忽略** (Continue) |
| **参数缺失** | `friend_message` 缺少 `to_user_id` 或 `content`。 | **忽略** (Continue) |
| **参数缺失** | `group_message` 缺少 `room_id` 或 `content`。 | **忽略** (Continue) |
| **非好友关系** | 尝试给非好友用户发送私聊消息 (`areFriends` 检查失败)。 | **忽略** (Continue) |
| **群组权限** | 尝试向不存在的群组发送消息。 | **忽略** (Continue) |
| **群组权限** | 发送者不是该群组成员。 | **忽略** (Continue) |
| **群组权限** | 发送者已退出该群 (`LeftAt != nil`)。 | **忽略** (Continue) |
| **禁言状态** | 发送者在群内处于禁言状态 (`MuteUntil > Now`)。 | **忽略** (Continue) |
| **系统错误** | 数据库操作失败（如创建房间失败、保存消息失败）。 | **忽略** (Continue) |

**开发建议**：
由于服务端不返回明确的 WebSocket 错误帧，前端应当在发送消息前进行充分的本地校验（如：检查输入是否为空、当前是否已选中合法的会话对象）。同时，前端可以通过“发送后是否收到自己的消息回执”来判断发送是否成功。
