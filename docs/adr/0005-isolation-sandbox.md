# ADR 0005: 隔离模式 worktree 沙箱（防 git 逃逸）

- **状态**：Accepted
- **日期**：2026-08-11
- **对应 Guardrail**：11

## 背景（Context）

[ADR 0004](0004-worktree-dual-mode-no-merge.md) 决定隔离模式下每个 agent 一个独立 worktree。但仅"分配独立 worktree"不够——agent 可能通过 git 命令的参数逃逸回主 checkout，对主仓库执行写操作，破坏隔离。

claude-code 踩过这个坑并修复：

> "Fixed `isolation: 'worktree'` subagents being able to run git-mutating commands against the main repo checkout instead of their own isolated worktree"
> "Fixed worktree-isolated subagents redirecting git into the shared checkout via `git -C`, `--git-dir`, or `GIT_DIR`/`GIT_WORK_TREE`"

## 决策（Decision）

隔离模式的 worktree 必须**沙箱化**，阻止 agent 通过以下方式逃逸回主 checkout（或其他 worktree）：

- `git -C <other-path> ...`
- `git --git-dir=<other> ...`
- 环境变量 `GIT_DIR` / `GIT_WORK_TREE` 指向其他位置
- 任何会修改 worktree 之外仓库状态的 git 子命令

实现方向（见 docs/design/，待细化）：

- 命令拦截：在 wrapper/PTY 层检测 git 命令，校验目标路径是否属于当前 worktree。
- 环境变量清理：启动隔离 agent 前清除/覆盖 `GIT_DIR` / `GIT_WORK_TREE`。
- 参考数据：claude-code 的修复方式（CHANGELOG L147, L330）。

## 范围限定

- 沙箱**只在隔离模式**生效；共享模式的 agent 本就共享 worktree，不需沙箱。
- 沙箱针对 **git 逃逸**；文件系统级强制隔离（chroot/namespace）是更重的可选项，v1 不做。

## 考虑过的替代方案（Alternatives Considered）

| 方案 | 为什么不选 |
|---|---|
| **不沙箱，靠 prompt 纪律** | OpenHarness swarm 用 prompt（"one at a time per set of files"）防共享模式互踩；但隔离模式的逃逸是技术性问题，prompt 挡不住 `git -C`。 |
| **文件系统级隔离（chroot/namespace/container）** | 最强但最重；v1 过度工程。git 命令拦截已覆盖主要逃逸路径。 |

## 结果（Consequences）

- 优点：隔离模式真正隔离，agent 不会误伤主 checkout 或其他 agent 的 worktree。
- 代价：需实现 git 命令拦截逻辑；可能有边缘逃逸路径需持续补。
- 共享模式不沙箱：互踩靠协调（prompt 纪律或文件级协调），见 docs/design/collab-modes.md。

## 参考（References）

- claude-code CHANGELOG L147、L330（worktree 隔离逃逸修复）
- agent-orchestrator 的 managedPath（`~/.ao/worktrees/<projectID>/<sessionID>`）
