# task_service 任务调用说明

本文根据 `internal/server/task/task_service.go` 的当前实现整理，描述每种任务在命令入口的调用方式与参数规则。

## 1. 通用调用入口

- 统一入口：`SubmitCommand(req SubmitCommandRequest, cb TaskCallback)`
- `req.Content` 必须以反斜杠 `\` 开头，否则返回 `unsupported command`
- 命令会去掉首个 `\` 后进行前缀匹配
- 成功提交后返回 `taskID`，并注册回调 `cb`

## 2. 各任务命令格式

### 2.1 Fake LLM 任务

- 命令前缀：`\task:fake_llm`
- 语法：
  - `\task:fake_llm <prompt>`
- 实际提交：
  - `SubmitFakeLLM`
  - `Priority = PriorityNormal`
  - `SleepMs = 800`

示例：

```text
\task:fake_llm 你好，生成一个测试回复
```

---

### 2.2 LLM 任务（英文前缀）

- 命令前缀：`\task:llm`
- 语法：
  - `\task:llm <prompt>`
- 实际提交：
  - `SubmitLLM`
  - `Priority = PriorityNormal`

示例：

```text
\task:llm 请帮我总结今天会议重点
```

---

### 2.3 LLM 任务（中文前缀）

- 命令前缀：`\对话`
- 语法：
  - `\对话 <prompt>`
- 约束：
  - prompt 不能为空，否则返回 `command required`
- 实际提交：
  - `SubmitLLM`
  - `Priority = PriorityNormal`

示例：

```text
\对话 帮我把下面内容润色成正式通知
```

---

### 2.4 摘要任务

- 命令前缀：`\生成摘要`
- 语法：
  - `\生成摘要 <n>`
- 约束：
  - 必须有 `RoomID`，否则 `summary room id is required`
  - `n` 必须是正整数，且 `n <= 1000`
- 实际提交流程：
  1. `parseSummaryCount` 解析 `n`
  2. `buildSummaryPrompt(roomID, n)` 生成 prompt
  3. `SubmitSummary`
- 提交参数：
  - `Priority = PriorityNormal`

示例：

```text
\生成摘要 50
```

---

### 2.5 Agent 任务

- 命令前缀：
  - `\agent`
  - `\智能体`
- 语法：
  - `\agent <goal>`
  - `\智能体 <goal>`
- 约束：
  - 必须有 `RoomID`，否则 `agent room id is required`
  - goal 不能为空
- 实际提交流程：
  1. `parseAgentGoal` 解析目标
  2. `loadAgentRecentMessages(roomID, 12)` 拉取上下文消息
  3. `SubmitAgent`
- 提交参数：
  - `Priority = PriorityNormal`
  - `MaxSteps = 5`

示例：

```text
\智能体 帮我整理今天群里待办并给出执行建议
```

---

### 2.6 RAG 建库任务

- 命令前缀：
  - `\生成rag`
  - `\rag`
- 语法：
  - `\生成rag`
  - `\rag`
- 约束：
  - 必须有 `RoomID`，否则 `rag room id is required`
- 实际提交：
  - `SubmitRAG`
  - `Priority = PriorityNormal`
  - 固定分段参数：
    - `SegmentGapSeconds = 600`
    - `MaxCharsPerSegment = 2000`
    - `MaxMessagesPerSeg = 24`
    - `OverlapMessages = 3`

示例：

```text
\生成rag
```

---

### 2.7 添加记忆任务（区间重建）

- 命令前缀：`\添加记忆`
- 语法：
  - `\添加记忆 <startSeq> <endSeq>`
- 参数规则：
  - `startSeq` 和 `endSeq` 必须是正整数
  - `startSeq <= endSeq`
- 约束：
  - 必须有 `RoomID`，否则 `rag room id is required`
  - 缺少参数时返回 `rag memory start/end sequence ids are required`
  - 区间非法时返回 `rag memory sequence range is invalid`
- 实际提交：
  - `SubmitRAGAddMemory`
  - `Priority = PriorityNormal`
  - 固定分段参数：
    - `SegmentGapSeconds = 600`
    - `MaxCharsPerSegment = 2000`
    - `MaxMessagesPerSeg = 24`
    - `OverlapMessages = 3`
- 行为说明：
  - 只处理 `[startSeq, endSeq]` 区间内消息
  - 会分段写入 `chat_segments` 并写入向量库
  - 不更新 `ChatSegmentCursor`，因此允许与既有 RAG 分段重复

示例：

```text
\添加记忆 1200 1800
```

---

### 2.8 RAG 检索任务

- 命令前缀：`\rag检索`
- 语法：
  - `\rag检索 <query>`
  - `\rag检索 <topK> <query>`
  - `\rag检索 <vector> <query>`
  - `\rag检索 <topK> <vector> <query>`
  - `\rag检索 <vector> <topK> <query>`
- vector 规则：
  - 可选值：`raw` / `summary`
  - 默认值：`raw`
  - 非法值返回：`rag search vector must be raw or summary`
- topK 规则：
  - 默认值：`5`
  - 必须大于 `0`
  - 最大值：`20`
- 约束：
  - 必须有 `RoomID`，否则 `rag room id is required`
  - query 不能为空
- 实际提交：
  - `SubmitRAGSearch`
  - `Priority = PriorityNormal`

示例：

```text
\rag检索 项目A本周进展
\rag检索 10 项目A延期原因
\rag检索 summary 项目A最终结论
\rag检索 8 summary 项目A本周共识
```

## 3. 不支持命令

未匹配以上前缀的命令统一返回：

```text
unsupported command
```
