# ADR 0009: 双协调拓扑（lead / peer 可切换）

- **状态**：Accepted
- **日期**：2026-08-12
- **对应 Guardrail**：8（协调交 agent/人，daemon 不做协调决策）

## 背景（Context）

[ADR 0001](0001-peer-to-peer-wrapper.md) 定了通信是 peer-to-peer wrapper（wrapper 间直连，daemon 不 relay）。但"通信拓扑"≠"协调拓扑"：通信是 wrapper 直连，**协调**（谁干啥、谁 review 谁、谁集成）依然需要一个组织方式。

朴素想法是只支持一种协调方式。但实际需求分裂：

- **大工程**：子任务多、边界清晰、要统一集成——需要一个"统筹者"拆活、派活、验收。
- **小团队（2-3 个 agent）**：用户想自己拆、自己 review，多个一个"lead"既浪费 token、又碍事。

super.engineering 的 recipes 文档证实这两种姿态都真实存在，且应可切换：

> "Route architecture and scope decisions through the lead."（中心化）
> "Let the frontend and backend roles resolve questions about their shared interface directly."（去中心化）

## 决策（Decision）

支持**两种协调拓扑，可切换**：

### lead 模式（中心化）
一个 agent 当 lead（"包工头"），统筹一次协作的完整生命周期：拆活 → `ask_agent` fan-out → `get_task` 收结果 → review。
- 适用：大工程、子任务边界清晰、需统一集成。
- worktree 倾向**隔离**（并行实现，各干各的）。
- 代价：lead 额外 token。

### peer 模式（去中心化）
人（或简单 coordinator）拆活，agent 之间直接对齐，没有 agent 居中统筹。
- 适用：小团队（2-3 agent）、用户自控拆分与 review、省 lead token。
- worktree 倾向**共享**（互看互改 + 共享文件对齐）。
- 对齐靠：A2A 直接对话 + 共享文件（见 [collab-modes](../design/collab-modes.md) 第三节）。

### 关键分层（最重要的约束）

**协调拓扑不是 daemon 的功能**（呼应 Guardrail 8）。daemon 只提供原语：

- 通信：`ask_agent` / `broadcast` / `get_task`（[ADR 0003](0003-a2a-task-model.md)）。
- 隔离：worktree 双模式（[ADR 0004](0004-worktree-dual-mode-no-merge.md)）。
- 发现：discovery 目录。
- 状态：coordination state（[ADR 0010](0010-coordination-state.md)，v2）。

**daemon 不感知"当前是哪种拓扑"**。客户端"切换模式" = 改变给 agent 的初始角色/prompt（谁当 lead、谁是 worker、谁 review）。两种拓扑走**同一套原语**，区别仅在于"有没有一个 agent 站出来当 lead"。

这保证协调策略可插拔、可演进——未来还能加新拓扑（如投票式、市场式），不用动基础设施。

## 考虑过的替代方案（Alternatives Considered）

| 方案 | 为什么不选 |
|---|---|
| **只 lead 模式** | 小团队（2-3 agent）多耗一层 lead token；用户想自控时 lead 反而碍事。super 自己也提供 peer 姿态。 |
| **只 peer 模式** | 大工程缺统一集成者，多 agent 各干各的易碎、难收口。 |
| **daemon 内置模式开关** | 违反 Guardrail 8（daemon 不做协调决策）；把协调策略锁死在基础设施，不可演进。 |
| **每种协作场景一种拓扑** | 把"拓扑"和"场景"耦合死。实际任一场景都能在任一拓扑下进行（见 collab-modes 拓扑×场景表），正交组合更灵活。 |

## 业界对标

- **super.engineering**：recipes 文档里明确两种 prompt 姿态（中心化 lead / peer-to-peer），由用户用 prompt 切换——本 ADR 与其同构，且把"切换"显式化为客户端可选的两模式。
- **orca**：coordinator agent fan-out worker（lead 模式）；但显式声明"Agents choose decomposition, topology, placement"——拓扑是 agent 选择，不是基础设施。
- **OpenHarness swarm**：worker 共享 leader cwd + prompt 纪律（peer 模式并行研究）。
- **Claude Code Agent Teams**：lead 分任务，teammate 直接互发消息（同一产品里两种姿态都有）。

共性：**没有项目把协调拓扑硬编码进基础设施**——都是上层（prompt / agent / 人）决定。本 ADR 与业界一致。

## 结果（Consequences）

- 优点：覆盖大工程（lead）和小团队（peer）；省 token（小团队不必养 lead）；协调策略可演进（未来加新拓扑不动 daemon）。
- 代价：客户端需提供"拓扑选择"的 UX（给 agent 不同角色/prompt）；peer 模式下需 prompt 纪律 + 共享文件防止互踩（无 lead 居中）。
- 不影响通信层：两种拓扑都走 peer-to-peer wrapper（ADR 0001），不引入中心 relay。

## 待细化（见 docs/design/）

- 每种「拓扑 × 场景」的典型 prompt / `AGENTS.md` 模板。
- lead 的选举/指定机制（人指定 vs agent 自荐）。
- 客户端"切换拓扑"的具体 UX 形态。

## 参考（References）

- super.engineering orchestration recipes：https://super.engineering/docs/orchestration-recipes/
- orca `ORCHESTRATION_IMPLEMENTATION_CHECKLIST.md`（topology 由 agent 选）
- Claude Code Agent Teams docs
