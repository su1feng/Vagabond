# ADR 0010: coordination state（公告板）—— versioned compare-and-set 共享权威状态

- **状态**：Accepted（设计Accepted，实现分阶段：v1 设计预留，v2 持久化与 compare-and-set）
- **日期**：2026-08-12
- **对应 Guardrail**：5（本 ADR 扩展 Rule 5 持久化范围）、8（协调交 agent/人）
- **关联**：[ADR 0001](0001-peer-to-peer-wrapper.md)、[0003](0003-a2a-task-model.md)、[0009](0009-coordination-topology.md)

## 背景（Context）

[ADR 0009](0009-coordination-topology.md) 定了双协调拓扑。peer 模式下尤其会出现一类硬问题：**多个 agent 要对同一决定达成或修改"权威事实"**——

- 方案对决：N 个 agent 各出一套方案，要对外对齐"已采纳的是哪一个"。
- 多 agent 改同一接口：A 和 B 都要改 `user.proto`，得先锁定"当前契约是什么"，避免各自改出冲突。
- 共享 owner：声明"auth 模块归 agent-A，别人别动"。

光靠 A2A 对话（[ADR 0003](0003-a2a-task-model.md)）或共享文件（collab-modes 第三节）不够：
- 对话是过程性、易流失、无并发控制——两个 agent 同时"宣布"不同决定会冲突。
- 共享文件（`COORD.md`）是文本，无 compare-and-set，并发写同样冲突。

super.engineering 的 coordination state 机制正是为解决这个问题，且明确把它和 messaging 分离：

> "state 放当前事实，message 放产生事实的对话。"

## 决策（Decision）

引入 **coordination state（公告板）**：一条**versioned、机读、权威**的共享状态，承载协作过程中的"当前事实"。

### 数据形态（key→value，versioned）
每个条目是机读的短结构化数据，例如：

| kind | 示例 |
|---|---|
| decision | `{key: "api.contract", value: "REST+JSON, /v2/users", owner, reason, version}` |
| owner | `{key: "owner:auth", value: "agent-A", version}` |
| lock | `{key: "lock:user.proto", value: "agent-B", acquired_at, version}`（advisory，不锁 git/fs） |
| checkpoint | `{key: "checkpoint:integration", value: {revision, verified, risks}, version}` |

### 并发：compare-and-set
写入须带"期望的旧 version"（CAS）。若服务端当前 version 已变（别人改过）→ 写入失败、返回新值，调用方 reconcile 后重试。冲突由 lead 或人 reconcile（呼应 [ADR 0009](0009-coordination-topology.md)：协调交 agent/人，daemon 不替人决策）。

### 与对话 / A2A Task 的区别（关键）
- **coordination state** = 当前**事实**（短结构化数据）：定的契约、归属、检查点。
- **agent message / Task** = 产生事实的**过程**（长文本）：讨论、问答、协商、任务执行。

两者**互补不重叠**：Task（[ADR 0003](0003-a2a-task-model.md)）管"有生命周期的任务"（咨询/评审/接力），coordination state 管"持久共享事实"。一次协作里 Task 产生结论 → 落成 coordination state 条目；后续 agent 读 state 拿权威事实，不必翻 Task 历史。

## 与 Rule 5 的关系（本 ADR 的硬约束修改）

**Rule 5 原文**：
> 轻量快照：持久化只存布局 + cwd + agent 会话引用；agent 对话内容绝不落盘，恢复交给 agent 自己的 resume。

**Rule 5 修改后**：
> 轻量快照：持久化只存布局 + cwd + agent 会话引用 + **协调状态（coordination state：决策/归属/锁/检查点等机读权威事实）**；**agent 对话内容绝不落盘**，恢复交给 agent 自己的 resume。

### 为什么这么改是自洽的（不是推翻）

Rule 5 的**本意**是禁"对话内容落盘"——理由有三：(1) 对话是长文本、占空间；(2) 对话恢复靠 agent 自己的 resume（agent 有自己的 session history）；(3) 避免平台变成"聊天记录仓库"。

coordination state **不触碰这三条**：

1. 它是**结构化短数据**（一条决策/一个 owner/一把锁），不是长文本对话。
2. 它恢复的是"**当前权威决定**"（如 API 契约），**不是对话上下文**——agent resume 恢复对话，coordination state 恢复决定，两者正交。
3. 它体量小、机读、有明确 schema，不会膨胀成聊天仓库。

**类比**：Rule 5 像"不存聊天记录"，coordination state 像"存一份当前的会议决议表"——决议表不是聊天记录，存它不违反"不存聊天记录"的本意。

因此扩展 Rule 5 持久化范围容纳 coordination state，**对话内容的边界原封不动**（仍是绝不落盘）。这是对 Rule 5 的**精确扩展**，不是放松。

### 为什么必须落盘（不能只存内存）

coordination state 是"权威事实"。agent 重启/重连、daemon 重启后，必须能恢复"当前契约/owner/lock"——否则协作状态丢失，多个 agent 会基于过期事实各自行动、互相破坏。这和"对话不落盘"（对话可由 agent resume 重建）性质不同：决定不能靠回放对话重建（对话本身没落盘），必须有独立的持久权威来源。

## 考虑过的替代方案（Alternatives Considered）

| 方案 | 为什么不选 |
|---|---|
| **只用共享文件（`COORD.md`）做权威** | 文本无 CAS，并发写冲突；无 schema，agent 解析易碎；无版本，无法检测并发改。 |
| **只用 A2A Task 协商权威** | Task 是过程性、有生命周期、完成后归档；拿"当前事实"得翻历史，且 Task 内容不落盘（Rule 5），重启后拿不到。 |
| **coordination state 不落盘（纯内存）** | daemon/agent 重启即丢权威事实，协作状态崩溃。见上"为什么必须落盘"。 |
| **改用 git 本身存协调状态** | 把决定塞进 git 历史增加噪音；merge 决策本就排除在系统外（[ADR 0004](0004-worktree-dual-mode-no-merge.md)）；CAS 语义不自然。 |

## 业界对标

- **super.engineering**：coordination state 是其协作模型的核心组件之一，明确与 messaging 分离，versioned + compare-and-set，冲突由 lead reconcile。本 ADR 直接对标。
- **etcd / Consul**：分布式 KV 的 CAS（`prevValue`/`cas`）是成熟并发原语，coordination state 的 CAS 语义同构（单机版，无需 Raft）。
- **ACP `Await` / A2A `input-required`**：多轮澄清的状态机，但不承载"持久共享事实"，与 coordination state 正交。

## 结果（Consequences）

- 优点：peer 模式/方案对决/多 agent 改接口有干净的并发权威机制；agent 重启可恢复决定；与对话边界清晰（不污染 Rule 5 的对话禁令）。
- 代价：扩展 Rule 5（已论证自洽）；需实现 KV + CAS + 版本（v2）；需定义 schema 与清理策略。
- **协调决策仍交 agent/人**：CAS 冲突的 reconcile 由 lead 或人决定，daemon 只提供 CAS 原语、不替人决策（呼应 Guardrail 8）。

## 实现分阶段

- **v1（本批）**：设计预留——文档 + 本 ADR + Rule 5 修改 + collab-modes 引用。不写实现。
- **v2**：实现持久化 KV + versioned CAS + 基础 kind（decision/owner/lock/checkpoint）+ discovery 集成（agent 可 `get_state`/`set_state`）。具体 API 与存储结构届时细化（见 collab-modes 待细化）。

## 待细化

- [ ] 数据模型 schema（每种 kind 的字段）。
- [ ] compare-and-set API 形态（MCP 工具 `get_state`/`set_state`？还是 A2A 方法？）。
- [ ] 存储位置（与 persist 模块的关系，见 [Project Map](../../AGENTS.md) `internal/persist/`）。
- [ ] 清理策略（条目 TTL？协作结束后归档？）。
- [ ] workspace 隔离（不同 workspace 的 state 是否可见）。

## 参考（References）

- super.engineering coordination state：https://super.engineering/docs/orchestration-coordination-state/
- etcd CAS 语义
- [ADR 0003](0003-a2a-task-model.md)（A2A Task 模型，与本 state 互补）
