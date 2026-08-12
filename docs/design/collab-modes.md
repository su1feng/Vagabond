# 5 种协作模式实现参考

> 对应 Guardrail 8、11；ADR 0001、0004。
> 本文档是骨架，列出每种模式的实现参考与 worktree 选择。协调逻辑由 agent/人，不是 Vagabond 基础设施。

## 模式与 worktree 选择总览

| 模式 | worktree | 业界最佳参考 |
|---|---|---|
| ① 任务分工（并行研究） | 共享（顺序写） | OpenHarness swarm |
| ① 任务分工（并行实现） | 隔离 | orca fan-out |
| ② 代码评审 | 共享 | claude-code `/code-review` |
| ③ 专家咨询 | 共享（helper 看 source cwd） | TermLoop `askAgent` |
| ④ 接力交接 | 共享（原地交接） | TermLoop `relay` / orca handoff |
| ⑤ 方案对决 | 隔离（各一树 + 沙箱） | orca `merge_ready` + claude-code sandbox |

---

## ① 任务分工

**并行研究**（共享 worktree）：
- 参考 OpenHarness swarm：leader/coordinator 分配研究子任务给多个 worker，worker 共享 leader cwd，靠 prompt 纪律（"写操作一次一个文件集"）防互踩。
- 通信：worker 完成后通过 wrapper 的 `broadcast`/`ask_agent` 向 coordinator 报告。

**并行实现**（隔离 worktree）：
- 参考 orca fan-out：coordinator 给每个 worker 一个独立 worktree（隔离模式 + 沙箱，见 [ADR 0005](../adr/0005-isolation-sandbox.md)）。
- 各自实现完成后，coordinator 读各自的 `merge_ready` 类消息，决定如何整合（系统不 merge）。

## ② 代码评审

- 参考 claude-code `/code-review`：reviewer 作为后台 agent，读当前 worktree 的改动，产评审意见。
- 共享 worktree：reviewer 看被评审方的当前状态。
- 触发：被评审方调 `ask_agent(reviewer, "review my changes", mode=sync)`；reviewer 完成后返回意见。

## ③ 专家咨询

- 参考 TermLoop `askAgent` bridge：source 调用 helper 问问题，helper 在**共享 cwd** 下回答（能看到 source 的代码状态）。
- 这是最典型的 A2A Task 用例：`ask_agent(expert, question, mode=sync, timeout=...)`，expert 处理后 completed + artifact=回答。
- 多轮澄清走 `input-required`（expert 反问）。

## ④ 接力交接

- 参考 TermLoop `relay` bridge：A 完成后把上下文交给 B，B 在同一 worktree 继续。
- 通信：A 调 `ask_agent(B, "handoff: <context>", mode=async)`，B 被唤醒（idle 注入）后接手。
- 共享 worktree：B 接手时看到 A 的改动。

## ⑤ 方案对决

- 参考 orca `merge_ready`：N 个 agent 各自在独立 worktree（隔离 + 沙箱）独立产出方案。
- 各自完成后发 `merge_ready` 类消息；coordinator agent（或人）比较各方案，挑一个。
- 系统不 merge——pick-one 决策由 coordinator/人，daemon 只提供并行 worktree + mailbox。
- 输方 worktree：由 coordinator/人决定保留还是清理（preserve-ref 兜底，见 [ADR 0004](../adr/0004-worktree-dual-mode-no-merge.md)）。

---

## 协调者（coordinator）的角色

- 协调逻辑不是 Vagabond 基础设施的职责（Guardrail 8）。
- 协调者可以是：
  - **某个 agent**（如用户指定 codex 当 coordinator，对标 orca）。
  - **人**（用户在 TUI/GUI 里指挥）。
- Vagabond 只提供通信原语（`ask_agent`/`broadcast`/`get_task`）+ worktree 管理 + discovery。

## 待细化

- [ ] 每种模式的典型 prompt/AGENTS.md 模板（教 agent 如何发起/响应）。
- [ ] coordinator 的选举/指定机制。
- [ ] 共享模式下文件级互踩防护（除 prompt 纪律外，是否要文件锁/声明机制）。
