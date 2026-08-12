# ADR 0008: 被排除的协议与方案（决策记录）

- **状态**：Accepted
- **日期**：2026-08-11
- **用途**：记录调研过但被排除的协议/方案及理由，避免日后重复讨论。

## 一、三个 ACP（缩写撞车，互不相干）

业界有三个都叫 "ACP" 的协议，完全不同：

| 全称 | 维护方 | 用途 | 拓扑 | 现状 | Vagabond 用吗 |
|---|---|---|---|---|---|
| **Agent Client Protocol** | Zed + JetBrains | 编辑器驱动单个 coding agent | IDE ↔ agent（stdio JSON-RPC） | ✅ 活；kimi-code/reasonix/claude-code/codex 全用它 | ❌ 是 IDE↔agent，不是 agent↔agent。Wrong layer。 |
| **Agent Connect Protocol** | Cisco/AGNTCY | 远程调用 agent（REST API） | client ↔ remote agent server | ❌ 2025-05 归档 | ❌ 已死。 |
| **Agent Communication Protocol** | IBM BeeAI → Linux Foundation | agent 间通信 | agent ↔ agent（RESTful HTTP） | 🔄 已并入 A2A（2025-08） | ❌ 作为独立协议已终结；其遗产（`Await` 原语）已成为 A2A 的 `input-required` 状态。 |

**关键澄清**：harness 里 kimi-code/claude-code/codex 用的 ACP **都是第一个**（Agent Client Protocol，IDE↔agent），与 agent 间通信无关。

- 本地验证：`kimi-code/packages/acp-adapter/` → `@agentclientprotocol/sdk`；`DeepSeek-Reasonix/docs/ACP.md` → "Agent Client Protocol"。

业界原话："Agent Communication Protocol and Agent Client Protocol share an abbreviation (ACP) and nothing else."

## 二、A2A wire protocol（全盘 HTTP）——被排除

- A2A v1.0.1 是成熟标准（Linux Foundation，150+ orgs，Task 生命周期 + 异步机制是亮点）。
- 但**没有 coding agent 原生暴露 A2A server**；全盘上 HTTP 意味着同机跑 N 个 localhost HTTP server + 端口管理，对 Vagabond 同机场景太重。
- **采纳的是 A2A 的数据模型（Task/Message/Part/Artifact + 生命周期），不是它的 HTTP 传输**。见 [ADR 0002](0002-a2a-grpc-unix-socket.md)：用 A2A gRPC binding over Unix socket，拿标准协议 + 无端口。
- 未来外部 agent 接入时，再在 Vagabond 边缘加 A2A-HTTP gateway。

## 三、纯 MCP pull ——被排除

- MCP 是 agent↔tool，官方明确"MCP 不是为 agent 间通信设计"。
- 纯 pull 送不到 idle agent（idle 不主动调工具），评审/咨询/接力三种模式的接收方常是 idle。
- MCP 的 notification 不保证能唤醒 agent 注意力（host 是否据此 re-prompt 是宿主行为，不可跨 agent 依赖）。
- Vagabond 用 MCP 的位置：**agent 与其 wrapper 之间**（agent 作为 MCP client 调 wrapper 暴露的 `ask_agent`/`get_task`/`broadcast` 工具）。这是 MCP 的正确用法（agent↔tool），不是把它当消息总线。

## 四、纯 broker relay（Vagabond 当中心总机）——被排除

- 每条消息过中心，daemon 是瓶颈 + 单点故障 + 偷听面；不是真正意义上的 A2A。
- 比 orca/Claude Code Teams 的实际做法都重（它们都是 coordinator agent + 共享邮箱/peer 直连，不是 daemon relay）。
- 见 [ADR 0001](0001-peer-to-peer-wrapper.md)：peer-to-peer wrapper 拓扑。

## 五、自定义协议（wrapper 间）——被排除

- 传输同样轻（Unix socket），但协议是自己发明的，未来外部对接要翻译，改协议=全部返工。
- 被 A2A gRPC over socket 严格优于（除非有必须偏离 A2A 数据模型的特殊需求）。见 [ADR 0002](0002-a2a-grpc-unix-socket.md)。

## 参考（References）

- A2A spec：https://a2a-protocol.org/latest/specification/
- Agent Client Protocol：https://agentclientprotocol.com/
- IBM ACP 并入 A2A 公告（2025-08-29，LF AI & Data）
- MCP：https://modelcontextprotocol.io/ （"MCP is not for agent-to-agent communication"）
