# SSE 最小改造方案（Agent 任务）

目标：在不推翻现有任务系统与 WebSocket 的前提下，新增一条基于 HTTP SSE 的最小流式输出链路，支持 Agent 任务的过程事件实时回传。

## 1. 接口定义（最小可用）

### 1.1 提交任务接口（复用现有命令提交）

- 继续使用现有提交命令逻辑（例如 `\agent ...`），返回 `task_id`。
- 服务端需保证可拿到对应 `request_id`，SSE 订阅以 `request_id` 为主键。

### 1.2 SSE 订阅接口（新增）

- Endpoint：`GET /api/agent/stream?request_id=<request_id>`
- 鉴权：Bearer Token（沿用现有中间件）
- Headers：
  - `Content-Type: text/event-stream`
  - `Cache-Control: no-cache`
  - `Connection: keep-alive`
  - `X-Accel-Buffering: no`
- 行为：
  - 连接建立后立即推送 `agent.start`
  - 按事件顺序持续推送
  - 心跳：每 15s 推送一次 `event: ping`
  - 收到 `agent.done` 或 `agent.error` 后关闭连接

## 2. 事件 JSON 结构

### 2.1 SSE 帧格式

```text
id: <event_id>
event: <event_type>
data: <json-string>

```

### 2.2 data JSON 统一结构

```json
{
  "event_id": "evt_000001",
  "event_type": "agent.tool.end",
  "request_id": "ws_command|user|room|parent_msg|120|uuid",
  "task_id": "task-uuid",
  "room_id": "room-uuid",
  "user_id": "user-uuid",
  "step": 2,
  "ts": 1775655000,
  "role": "ToolResponse",
  "tool": "summarize_tool_result",
  "status": "succeeded",
  "content": "工具结果摘要文本",
  "token_usage": {
    "prompt_tokens": 120,
    "completion_tokens": 30,
    "total_tokens": 150
  },
  "error": ""
}
```

### 2.3 事件类型最小集合

- `agent.start`：任务开始
- `agent.step`：每个节点完成后发一次（先不拆 delta）
- `agent.done`：最终成功结果
- `agent.error`：流程失败
- `ping`：心跳

## 3. 后端改造点（按最小实现）

### 3.1 事件总线（进程内）

先做单机最小版：内存 pub/sub，不改 RabbitMQ 结构。

- 新增 `internal/service/agent_stream_hub.go`
  - `Subscribe(requestID string) (<-chan AgentStreamEvent, func())`
  - `Publish(event AgentStreamEvent)`
  - 每个 request_id 一个订阅列表，支持多个监听端

### 3.2 task 完成事件扩展（已有 done event 复用）

- 文件：`internal/service/done_event_mq.go`
  - `DoneEvent` 增加 `parent_message_id`、`parent_sequence_id`（已在本轮完成）
  - `BuildDoneEvent` 解析 request_id 后写入引用字段（已在本轮完成）

- 文件：`internal/taskservice/done_event_mq.go`
  - 生产侧 `DoneEvent` 与 request_id 编解码保持一致（已在本轮完成）

### 3.3 在 Agent 执行过程中发布 step 事件

- 文件：`internal/taskservice/task/agent/memory/memory.go`
  - 在 `AppendObservation` 后调用回调钩子（新增可选 hook）
  - 把 Observation 转成 `agent.step` 事件推到 stream hub

- 文件：`internal/taskservice/task/agent/engine.go`
  - 给 session/context 注入 `request_id/task_id/room_id/user_id`，供事件构造使用

### 3.4 新增 SSE Handler

- 新增文件：`internal/api/handler/agent_stream_handler.go`
  - 参数校验：`request_id` 必填
  - 权限校验：request_id 必须属于当前 user_id
  - 建立 SSE 连接并持续写出事件
  - 处理 client disconnect

- 文件：`internal/api/router.go`
  - 注册路由：`GET /api/agent/stream`

### 3.5 与现有 WS 并存

- 文件：`internal/api/handler/ws_handler.go`
  - 保持现有机器人消息广播逻辑不变
  - 在 done event 回调时，除发送机器人消息外，再向 stream hub 发布 `agent.done/agent.error`

## 4. 最小时序

1. 客户端提交 `\agent ...`，拿到 `task_id` 和 `request_id`（或通过 task 查询拿 request_id）。
2. 客户端连接 `GET /api/agent/stream?request_id=...`。
3. 服务端推送：
   - `agent.start`
   - 多个 `agent.step`（Planner/Coordinator/ToolResponse/FinalThink/FinalJudge）
   - `agent.done` 或 `agent.error`
4. SSE 连接关闭。
5. 原有 WS 房间消息继续正常广播机器人最终回复。

## 5. 最小验收标准

- 能通过同一 `request_id` 稳定收到有序事件流。
- `agent.step` 至少包含：`step/role/tool/status/content`。
- `agent.done` 包含最终文本与 payload（含 memory）。
- 错误场景返回 `agent.error`，并在 1s 内关闭连接。
- 不影响现有 WebSocket 聊天主链路。
