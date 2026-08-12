# Discovery / Agent 目录

> 对应 Guardrail 8、10。
> 本文档是骨架，待实现时细化。

## 职责

agent 需要找到其他 agent（peer）才能发起协作。discovery 维护一个"agent 目录"，让 wrapper 注册、让 agent 查询。

## 目录内容

每个 agent 在目录里登记一条记录（对标 A2A Agent Card）：

| 字段 | 说明 |
|---|---|
| `agent_id` | 唯一标识（如 `codex-session-3`） |
| `socket_path` | wrapper 端点面的 Unix socket 路径（`/run/vagabond/<agent-id>.sock`） |
| `display_name` | 给人看的名字（如 `codex`） |
| `kind` | agent 类型（codex / kimi-code / reasonix / claude-code / ...） |
| `status` | working / idle / blocked（来自 Rule 3 状态感知） |
| `skills` | 该 agent 擅长什么（可选，供 coordinator 分配参考） |
| `workspace` | 所属 workspace/worktree（共享模式下的协作范围） |

## 注册与查询

- **注册**：wrapper 启动时向 discovery 注册一条记录；状态变更时更新（heartbeat 或事件驱动）。
- **查询**：agent 通过 wrapper 的 MCP 工具 `list_peers()` 查目录，拿到 peer 的 socket_path，即可发起 A2A 调用。
- **注销**：wrapper 停止时注销；crash 时由 heartbeat 超时清理。

## 与 A2A Agent Card 的关系

A2A 标准的 Agent Card 通常通过 HTTP `/.well-known/agent-card.json` 发现。Vagabond 是同机本地场景，`.well-known` HTTP 发现不适用（见 A2A Discussion #914）。因此：

- v1：用 daemon 内的目录结构（不走 HTTP），`list_peers` MCP 工具直接返回。
- 未来（外部 agent 接入）：Vagabond 边缘 gateway 把内部目录映射成标准 A2A Agent Card 暴露出去。

## 待细化

- [ ] 目录存储：daemon 内存 + 持久化（轻量快照，Rule 5）。
- [ ] heartbeat 间隔与超时阈值。
- [ ] workspace 隔离：不同 workspace 的 agent 互相不可见？
- [ ] `list_peers` 的过滤（按 workspace / 按 status / 按 kind）。
