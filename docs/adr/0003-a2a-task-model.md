# ADR 0003: 借 A2A Task 模型 + 同步/异步双模式 + PTY 注入唤醒 idle

- **状态**：Accepted
- **日期**：2026-08-11
- **对应 Guardrail**：9、10

## 背景（Context）

wrapper 之间用什么**语义**通信？这决定了一个关键问题：agent A 问 agent B 一个问题，A 会不会卡死？

朴素方案（fire-and-forget `send_message`）在"需要回答的咨询"场景会出问题：

- 若 `send_message` 立即返回 → A 拿不到答案，得反复 poll。
- 若 `send_message` 阻塞等回复 → B 在 working 时 A **卡死**。

此外，idle 的 agent 不会主动调任何工具（它在等输入，没有"轮次"），所以纯 pull 送不到 idle agent——评审/咨询/接力三种模式的接收方常是 idle。

## 决策（Decision）

采用 **A2A 的 Task 数据模型**作为 wrapper 通信的语义层：

### Task 生命周期

`submitted → working → (input-required) → completed / failed / canceled / rejected`

- 需要回答的咨询 = 一个 Task（有生命周期）。
- 广播/通知 = 一次性 Message（无生命周期）。

### 同步/异步双模式（对应 A2A `returnImmediately`）

- **同步模式**：`ask_agent(B, q, mode=sync, timeout=300s)` —— A 的调用阻塞，最多等 timeout 拿 B 的回答；超时返回"B 忙"。适合 A 真需要答案才能继续。
- **异步模式**：`ask_agent(B, q, mode=async)` —— 立即返回 task_id，A 继续干，结果到了再通知 A（通过 wrapper 端点面回调，或 A 下一轮 `get_task` 拉）。

### input-required（多轮澄清）

被咨询方中途可反问澄清：把 Task 状态设为 `input-required`，原提问方收到通知并回答，Task 继续。（这是 IBM ACP `Await` 原语并入 A2A 后的遗产。）

### 投递：按接收方状态分工（关键）

| 接收方状态 | 投递方式 | 理由 |
|---|---|---|
| working | 留在 Task store，接收方下一轮 `get_task`/`check_inbox` 自己拉 | 不打断 |
| idle / blocked | wrapper 往接收方 PTY 注入（Rule 2 + Rule 3） | idle agent 不主动调工具，只能注入唤醒 |
| 历史查询（任何时候） | `read_inbox` / `get_task` | 翻旧账 |

注入时机由 Rule 3 状态检测 + Rule 6 wait 原语决定（等 idle 再注入，不打断正在生成的输出）。

## 通信路径示意

```
agent A 调 MCP 工具 ask_agent(B, q)  [A 是 wrapper 的 MCP client]
   │
   ▼
A 的 wrapper 创建 Task（submitted→working），通过 A2A gRPC 调 B 的 wrapper
   │
   ▼
B 的 wrapper 检查 B 状态（Rule 3）:
   ├─ B working → Task 留存，B 下一轮自己 get_task 拉到
   └─ B idle    → 注入 B 的 PTY（Rule 2），B 被唤醒、处理
   │
   ▼
B 产出回答 → B 的 wrapper 更新 Task（completed + artifact=回答）
   │
   ▼
A 拿结果:
   ├─ 同步模式 → A 的原调用返回回答（或超时）
   └─ 异步模式 → A 的 wrapper 通知 A（注入 PTY 或下一轮 pull）
```

## 考虑过的替代方案（Alternatives Considered）

| 方案 | 为什么不选 |
|---|---|
| **纯 fire-and-forget mailbox** | 咨询场景拿不到答案/卡死；无法区分"需回答的咨询"与"广播通知"。 |
| **纯 MCP pull** | idle agent 不会主动调工具，评审/咨询/接力的接收方送不到。 |
| **纯 PTY 注入** | 注入时机/格式/语义都有代价（打断输出、格式各异、被当用户聊天、污染画面、状态干扰）；只适合唤醒，不适合结构化通信。 |
| **阻塞式同步唯一模式** | B 忙时 A 卡死，无超时无逃生。 |

## 业界对标

- **Claude Code Agent Teams**：轮询邮箱 + 唤醒卡住的 teammate（"messaging a stuck teammate wakes it to retry immediately"），两条腿走路，不是纯 pull。本 ADR 同构。
- **A2A spec**：`returnImmediately` 标志 + SSE/push notification/polling 三种异步机制 + `input-required` 状态。

## 结果（Consequences）

- 优点：咨询不卡死（同步带超时 / 异步不阻塞）；idle agent 能被唤醒；多轮澄清有标准状态。
- 代价：wrapper 需维护 Task store + 状态检测驱动的注入逻辑；需定义注入文本格式（让 agent 把它当输入而非噪音）。
- Task store 持久化：对标 mailbox 持久化，agent 重启可 `get_task`/`read_inbox` 回放。

## 待细化（见 docs/design/）

- 注入文本格式约定（如统一前缀标记 `[peer-msg from <agent> @ <ts>]`）。
- Task store 的存储结构与清理策略。
- 同步模式的超时默认值与可配置性。

## 参考（References）

- A2A spec：Task lifecycle、`SendMessageConfiguration.returnImmediately`、streaming-and-async
- ACP `Await` → A2A `input-required` 的并入记录（见 [ADR 0008](0008-excluded-protocols.md)）
- Claude Code Agent Teams CHANGELOG（teammate wake 行为）
