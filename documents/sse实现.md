如果改成 SSE 流式，你需要做什么

1. 定义事件模型（核心）
- 例如：
  - agent.start
  - agent.thought.delta
  - agent.tool.start
  - agent.tool.end
  - agent.answer.delta
  - agent.done
  - agent.error
- 每个事件都带 request_id / task_id / room_id / step / ts ，可选带 token_usage 。
2. 在 taskservice 内部产生过程事件
- 在 agent 节点执行处发事件（planner/coordinator/tool_response/final_think 等）。
- 你现在已经有 memory observation，可直接把 observation 增量事件化。
3. 做“事件总线”
- 可复用 MQ：新增 progress topic/queue（按 request_id 路由）。
- 或先内存通道（单机），后续再上 MQ。
4. 新增 HTTP SSE 接口
- 例如 GET /api/agent/stream?request_id=...
- 返回 Content-Type: text/event-stream
- 周期心跳，支持断线重连（ id: + Last-Event-ID ）。
5. 鉴权与权限
- 只能订阅自己发起的 request_id 或本房间可见事件。
6. 结束语义
- 明确何时 close：收到 agent.done/agent.error 后服务端主动结束流。
SSE vs WebSocket（你这个场景）

- SSE 优点
  
  - 单向推送天然匹配“agent 输出流”。
  - 基于 HTTP，网关/CDN/日志链路友好。
  - 浏览器 EventSource 简单，自动重连方便。
- SSE 缺点
  
  - 仅服务端→客户端，客户端回传控制要另开 HTTP。
  - 某些代理默认缓冲，需要明确关闭缓冲。
- WS 优点
  
  - 双向，控制消息（取消、继续、反馈）可同通道。
  - 你已有 WS 基础设施，改动可更小。
- WS 缺点
  
  - 连接管理、重连、扩缩容、粘性会话更复杂。
  - 对“纯流式输出 API”来说有点重。
