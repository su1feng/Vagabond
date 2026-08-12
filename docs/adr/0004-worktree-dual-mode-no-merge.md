# ADR 0004: worktree 双模式 + 系统不参与 merge

- **状态**：Accepted
- **日期**：2026-08-11
- **对应 Guardrail**：11

## 背景（Context）

5 种协作模式对 worktree 的需求不同：

| 协作模式 | 接收/协作方状态 | worktree 倾向 |
|---|---|---|
| ① 任务分工（并行研究） | 活跃 | 共享（顺序写）或隔离（并行） |
| ② 代码评审 | idle | 共享（评审当前树） |
| ③ 专家咨询 | idle | 共享（helper 看 source 的 cwd） |
| ④ 接力交接 | idle | 共享（原地交接） |
| ⑤ 方案对决 | 活跃 | 隔离（各一树，比较） |

单一模式覆盖不了全部 5 种。研究多个多 agent 协作项目后发现：**没有一个项目做自动 merge**——merge 决策要么交人（PR），要么交 coordinator agent。

## 决策（Decision）

### 双模式

- **共享模式（默认）**：协作的 agent 在同一个 worktree。适用于 ①②③④。
- **隔离模式（可选）**：每个 agent 一个独立 worktree。适用于 ⑤ 方案对决，以及 ① 的并行实现分支。

### 系统不参与 merge

对标 orca 的显式 non-goal：

- daemon **不做** commit / branch / merge / integration / target-ref tracking。
- merge 决策交给 **agent**（通过 `merge_ready` 类消息由 coordinator agent 决定）或**人**。
- 这样系统才能支持多种策略：pick-one（对决）、human-merge（PR）、agent-merge（coordinator 综合）、handoff（接力），而不锁死在某一种。

### session 级绑定 + preserve-ref

- worktree 绑定粒度：session 级（对标 agent-orchestrator 的 `~/.ao/worktrees/<projectID>/<sessionID>`）。
- dirty worktree 处理：清理前捕获未提交内容到 `refs/vagabond/preserved/<sid>`（对标 agent-orchestrator 的 StashUncommitted preserve-ref 管道），避免误丢。

## 考虑过的替代方案（Alternatives Considered）

| 方案 | 为什么不选 |
|---|---|
| **只共享模式** | ⑤方案对决需要隔离（各自独立产出再比较）；并行实现分支也需隔离。 |
| **只隔离模式** | ②③④的接收方需要看到 source 的当前状态，共享更自然（对标 TermLoop askAgent：helper 用 source 的 cwd 以共享 worktree）。 |
| **自动 merge** | 业界无人做。maka-agent 最接近（捕获 patch 当 artifact）但不自动 apply。自动 merge 的冲突处理极复杂，且锁死策略。 |

## 业界对标

| 项目 | 模式 | 参考点 |
|---|---|---|
| **orca** | agent 自选（共享或隔离）；显式 no-merge | 模式无关原语 + `merge_ready` 邮箱消息由 coordinator agent 读 |
| **agent-orchestrator** | 隔离（`ao spawn` → `ao/<sid>/root` worktree + PR） | session 级绑定 + preserve-ref dirty 处理 |
| **claude-code** | 隔离（background agent / `isolation:worktree`） | 沙箱防逃逸（见 [ADR 0005](0005-isolation-sandbox.md)） |
| **TermLoop** | 共享（helper 用 source cwd） | `AskToBridgeLauncher.swift` 注释："shares the worktree" |
| **OpenHarness swarm** | 共享（worker 共享 leader cwd，prompt 纪律防踩） | "one at a time per set of files" |

**orca 的架构是最贴近"支持全部 5 种"的**：原语层（并行 worktree + mailbox + dispatch）+ 模式由 agent 决定。本 ADR 采纳其"系统不 merge"哲学。

## 结果（Consequences）

- 优点：覆盖全部 5 种协作模式；merge 策略不锁死；worktree 安全（preserve-ref）。
- 代价：共享模式下需防止 agent 互相踩（参考 OpenHarness 的 prompt 纪律，或文件级协调）；隔离模式需沙箱（见 [ADR 0005](0005-isolation-sandbox.md)）。
- 共享 vs 隔离的切换：由协作模式驱动（非用户手动选），见 docs/design/collab-modes.md。

## 参考（References）

- orca `ORCHESTRATION_IMPLEMENTATION_CHECKLIST.md`（non-goals 段）
- agent-orchestrator `gitworktree/workspace.go`（StashUncommitted preserve-ref）
- TermLoop `AskToBridgeLauncher.swift`（shared cwd）
- OpenHarness `coordinator/coordinator_mode.py`（prompt 纪律防踩）
