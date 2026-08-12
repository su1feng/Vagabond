# 协作模型：双协调拓扑 × 五种场景

> 对应 Guardrail 8、11；ADR [0001](../adr/0001-peer-to-peer-wrapper.md)、[0004](../adr/0004-worktree-dual-mode-no-merge.md)、[0009](../adr/0009-coordination-topology.md)、[0010](../adr/0010-coordination-state.md)。
> 协调逻辑由 agent/人决定，不是 Vagabond 基础设施的职责（Guardrail 8）。本文档定义协调的「原语 + 框架」，具体编排交上层。

## 总览

Vagabond 的协作由两个**正交维度**组合而成：

- **协调拓扑**（谁统筹）：lead 模式（中心化）/ peer 模式（去中心化）——见 [ADR 0009](../adr/0009-coordination-topology.md)。
- **协作场景**（干什么）：任务分工 / 代码评审 / 专家咨询 / 接力交接 / 方案对决。

任一场景可在任一拓扑下进行。**拓扑不是 daemon 的功能**（daemon 只给原语），由人或 agent 选择。

## 一、协调拓扑

### lead 模式（中心化）
一个 agent 当 lead（"包工头"），统筹一次协作的完整生命周期。

- 流程：lead 拆活 → `ask_agent` fan-out 给 worker → `get_task` 收结果 → review（或发独立 reviewer）。
- 适用：大工程、子任务边界清晰、需要统一集成。
- worktree：倾向**隔离**（并行实现，各干各的，见 [ADR 0004](../adr/0004-worktree-dual-mode-no-merge.md)）。
- 代价：lead 多一层 token（拆活/集成/验证都它干）。

### peer 模式（去中心化）
人（或简单 coordinator）拆活，agent 之间直接对齐，没有一个 agent 居中统筹。

- 流程：人拆活派给几个 agent → agent 间用 `ask_agent`/`broadcast` + 共享文件直接对齐、互查、改 bug。
- 适用：小团队（2-3 个 agent）、用户想自控拆分和 review、省 lead token。
- worktree：倾向**共享**（互看互改 + 共享文件对齐）。

### 关键分层（重要）
拓扑选择是**上层（人/agent）的组织方式，不是 daemon 的功能**（呼应 Guardrail 8）。客户端"切换模式" = 改变给 agent 的初始角色/prompt；**daemon 只提供 `ask_agent`/`broadcast`/`get_task`/worktree/discovery 原语，不感知当前是哪种拓扑**。这保证协调策略可插拔、可演进，不锁死在基础设施里。

## 二、拓扑 × 五场景

| 场景 | lead 模式下的形态 | peer 模式下的形态 |
|---|---|---|
| ① 任务分工 | 并行实现：lead fan-out，**隔离** worktree（对标 orca fan-out） | 并行研究：**共享** worktree + prompt 纪律防互踩（对标 OpenHarness swarm） |
| ② 代码评审 | lead 或独立 reviewer 统筹评审（对标 claude-code `/code-review`） | source 直接请 expert 看改动 |
| ③ 专家咨询 | （天然 source→expert，通常无需 lead） | source 问 expert，共享 cwd（对标 TermLoop `askAgent`） |
| ④ 接力交接 | （通常无需 lead） | A→B 原地交接，共享 worktree（对标 TermLoop `relay` / orca handoff） |
| ⑤ 方案对决 | coordinator agent 统筹比较各方案（对标 orca `merge_ready`） | 各自独立产出，**人事后挑**（隔离+沙箱，对标 claude-code sandbox） |

## 三、对齐机制（peer 模式尤其关键）

### 1. A2A 直接对话
agent 通过 wrapper 端点面调 `ask_agent` / `broadcast`（见 [ADR 0001](../adr/0001-peer-to-peer-wrapper.md)、[0003](../adr/0003-a2a-task-model.md)）。适合：问答、纠正、协商、广播通知。

### 2. 共享文件对齐
对标 super.engineering 的 `FEATURE.md`：所有协作 agent 读写**同一份协调文件**，记录目标、范围、约束、接口契约、当前决定。共享 worktree 下天然可行——复用 `AGENTS.md` 或专用协调文件（如 `COORD.md`）。
- 适合：peer 模式下 agent 保持步调一致，不靠 lead 中转。
- 更新纪律：agent 发现范围/契约变化时更新该文件，其他 agent 下次读取即对齐。

两种机制**互补**：对话处理"过程"，文件固化"事实"。若需要"权威事实 + 并发安全"（多人同时改同一决定），进一步用 coordination state（见第五节）。

## 四、独立 review

lead 模式下**不要让 lead 自己验收自己的集成**——请 fresh reviewer（独立 session、未参与实现、无偏见）做代码评审。对标 super.engineering「独立视角比人多更重要」。

fresh reviewer 即 ② 代码评审的 lead 形态：`ask_agent(fresh_reviewer, "review changes", mode=sync)`，reviewer 只读 worktree、产出证据支撑的问题清单、不直接改文件；确认的问题再交回原实现者修。

## 五、coordination state（公告板）

当多个 agent 要对**同一决定**达成或修改权威事实（如统一 API 契约、声明 owner、方案对决对外齐方案）时，光靠对话或共享文件容易冲突。coordination state 提供 **versioned compare-and-set** 的机读权威状态（decisions / owners / locks / checkpoints）。

详见 [ADR 0010](../adr/0010-coordination-state.md)。要点：

- **与对话/Task 的区别**：state = 当前事实（短结构化数据），message/Task = 产生事实的过程。
- **与 Rule 5 的关系**：Rule 5 禁"对话内容落盘"，coordination state 是结构化决策数据、不是对话；已扩展 Rule 5 持久化范围容纳它（论证见 ADR 0010）。
- **实现分阶段**：v1 设计预留（本批文档 + ADR），v2 实现持久化与 compare-and-set。

## 六、协调者（coordinator）的角色

- 协调逻辑不是 Vagabond 基础设施的职责（Guardrail 8）。
- 协调者可以是：**某个 agent**（如用户指定 codex 当 lead，对标 orca）、或**人**（用户在 TUI/GUI 指挥）。
- Vagabond 只提供：通信原语（`ask_agent`/`broadcast`/`get_task`）+ worktree 管理 + discovery + coordination state。

## 业界对标汇总

| 项目 | 可借鉴点 |
|---|---|
| **super.engineering** | 双拓扑可切换（recipes 里中心化 vs peer-to-peer 两种 prompt 姿态）；shared context（`FEATURE.md` 共享文件对齐）；coordination state（公告板）；独立 review |
| **orca** | fan-out（lead 模式任务分工）；`merge_ready`（方案对决）；显式 no-merge 哲学 |
| **claude-code** | `/code-review`（独立评审）；worktree 隔离沙箱防逃逸 |
| **TermLoop** | `askAgent`（专家咨询，共享 cwd）；`relay`（接力交接） |
| **OpenHarness** | swarm（peer 模式并行研究，共享 worktree + prompt 纪律防互踩） |
| **agent-orchestrator** | session 级 worktree 绑定 + preserve-ref dirty 处理 |

## 待细化

- [ ] 每种「拓扑 × 场景」组合的典型 prompt / `AGENTS.md` 模板（教 agent 如何发起与响应）。
- [ ] 共享协调文件（`COORD.md`）的字段约定与更新纪律。
- [ ] coordination state 的数据模型与 compare-and-set API（v2）。
- [ ] lead 的选举/指定机制（人指定 vs agent 自荐）。
- [ ] 共享模式下文件级互踩防护（除 prompt 纪律外，是否要文件锁/声明机制）。
