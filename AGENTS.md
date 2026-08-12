# Vagabond

## Working Principles

- 从第一性原理思考。从真实需求、代码事实和验证结果出发；目标不清晰时先与用户讨论。
- 代码是事实来源，文档不是。除非用户明确要求，不要为了理解实现而去读普通 Markdown。
- 改动代码前，先读相关代码和最近的约束，并遵循目录树中最近的 AGENTS.md。
- 保持改动聚焦。不要顺带做无关的重构。
- 提交时不要添加 co-author 署名，也不要在 commit message、PR 描述或任何说明文字中暴露 agent 身份。
- 如果在代码实现过程中发现和约束冲突，先与用户讨论，再决定是否修改约束。
- 在每次代码实现前，先与用户确认方案，确保理解无误。
- 承重假设先验证，细节再与用户确认，确保理解无误，再进行实现。
## Architecture Guardrails

1. **单持有者状态**：AppState 树只被核心 goroutine 持有；PTY 读取、客户端连接、API 处理器只能通过 channel 发消息，绝不直接修改状态。
2. **daemon 拥有一切，客户端什么都不拥有**：所有状态和 PTY 都在 daemon；TUI/桌面/移动客户端不持有会话状态，重连时从 daemon 重新同步。
3. **字节流渲染**：daemon 推原始字节流，由客户端渲染；daemon 内部的 VT 翻译器只用于 agent 状态感知（working/blocked/idle），不参与推流。该状态感知同时服务于 wrapper 的注入时机判断（见 Guardrail 9）。
4. **双 socket**：JSON API socket（agent/CLI 用）与二进制 client socket（UI 用）分离；协议版本用整数递增（PROTOCOL_VERSION）；禁止 protobuf / gob / base64-in-JSON。
5. **轻量快照**：持久化只存布局 + cwd + agent 会话引用 + 协调状态（coordination state：决策/归属/锁/检查点等机读权威事实）；agent 对话内容绝不落盘，恢复交给 agent 自己的 resume。详见 [ADR 0010](docs/adr/0010-coordination-state.md)。
6. **等待而不是轮询**：agent 接口的核心是阻塞 wait 原语（idle/ready/exited/blocked + 超时），禁止 sleep-and-poll。
7. **生命周期用 context**：每个 goroutine 通过 context 取消；客户端断开只清理自己的资源，不影响 daemon。
8. **wrapper peer-to-peer 协作**：每个 agent 套一个 wrapper goroutine；wrapper 之间直接通信，不走中心 relay。daemon 只提供 wrapper 宿主、终端渲染和 discovery（agent 目录），不做路由或协调决策——协调（谁干啥、谁 review 谁）交给 coordinator agent 或人。
9. **wrapper 双面**：终端面（PTY，字节流渲染，给用户看）+ 端点面（A2A gRPC server over Unix socket，给 peer 调）。端点面收到 peer 请求 → 注入 agent PTY → 等回应 → 返回。注入时机由 Rule 3 状态检测 + Rule 6 wait 原语决定（idle 才注入，不打断）。
10. **通信借 A2A Task 模型**：wrapper 间通信采用 A2A 的 Task 生命周期（submitted/working/input-required/completed/failed/canceled/rejected）+ 异步语义；同步/异步两种调用模式（对应 A2A 的 `returnImmediately`）。该 wrapper 间通道是内部 IPC，与 Rule 4 的两个客户端 socket 分离；此处使用 protobuf（A2A gRPC 标准定义）不违反 Rule 4（Rule 4 的 protobuf 禁令只针对客户端 socket）。禁止 sleep-and-poll（呼应 Rule 6）。
11. **worktree 双模式，系统不 merge**：共享模式（默认，协作场景）+ 隔离模式（可选，如方案对决）。merge 决策交 agent 或人，daemon 不参与 commit/branch/merge/integration。隔离模式必须沙箱化：阻止 agent 用 `git -C` / `GIT_DIR` / `GIT_WORK_TREE` 逃逸回主 checkout。

## Project Map & Responsibilities

- `cmd/vagabond/` — 入口分发（检测 daemon → 拉起 → attach）。只做分发，不放业务逻辑。
- `internal/daemon/` — 事件循环 + 双 socket 监听 + 客户端连接管理。daemon 内禁止 UI 渲染逻辑。
- `internal/app/` — AppState 状态树（workspace/tab/pane）+ 变更动作。状态只归核心 goroutine。
- `internal/pty/` — 统一 PTY 接口 + unix（creack/pty）+ windows（ConPTY）。其他模块不得直接依赖平台 pty 库。
- `internal/protocol/` — 协议编解码（长度前缀 + 版本协商）。数据路径禁止 base64 或逐块 utf8 解码。
- `internal/api/` — JSON API + wait 原语 + 事件订阅。agent 接口只在这一层暴露。
- `internal/render/` — 视图纯函数 Draw(state, canvas)。只能画，不能改状态。
- `internal/client/` — bubbletea TUI 前端。不持有会话状态，不直接读 AppState。
- `internal/persist/` — 轻量快照保存/恢复（布局 + cwd + agent 会话引用 + 协调状态）。不存终端内容与对话。
- `internal/agent/` — Agent 适配器（Start/Stop/Status/SendInput）。适配器是叶子，不反向依赖。
- `internal/platform/` — 平台能力抽象。平台差异只收敛于此。
- `internal/wrapper/` — agent wrapper（终端面 PTY 管理 + 端点面 A2A gRPC server + peer 请求→PTY 注入逻辑）。wrapper 是协作的基本单元。
- `internal/a2a/` — A2A 协议（gRPC over Unix socket 编解码 + Task/Message/Part/Artifact 数据模型 + Task 生命周期状态机）。
- `internal/discovery/` — agent 目录（wrapper socket 路径 + Agent Card 注册/查询/心跳）。
- `internal/worktree/` — worktree 双模式管理（共享/隔离）+ 隔离沙箱 + preserve-ref dirty 处理。merge 逻辑不在此处。

## Commands

```bash
gofmt -w .        # 格式化（CI 最先检查）
go vet ./...      # 静态检查
go test ./...     # 全量测试
```

- 平台相关代码放 internal/platform 或 build tag（unix.go / windows.go）。
- 保持 daemon 可无头构建和测试：测试不得依赖真实终端。

## Git & PR

- **不主动合并到 main**：只提 PR（`gh pr create`），合并到 main（`gh pr merge` / `git merge main` / `git push main`）一律由人决定，agent 绝不自动执行。
- Conventional Commits（feat: / fix: / docs: / test: / chore:），主题描述性、聚焦单一改动。
- 不加 co-author，不透露 agent 身份。
- 一轮 review 反馈一次 force-push；review 反馈用 amend 而不是追加 commit。
- PR 保持最小 diff，只含与目的相关的文件。

## Further Docs

- `docs/adr/` — 架构决策记录（每个 Guardrail 的"为什么"，含被排除方案的对比）。
- `docs/design/` — 详细设计（wrapper 内部结构、双拓扑协作模型、discovery、客户端）。

> Vagabond 的定位：多 agent 协作平台——同一项目内，异构 agent（codex/reasonix/kimi-code/claude-code 等）以 peer-to-peer 方式协作。协调支持双拓扑（lead 中心化 / peer 去中心化，可切换，见 [ADR 0009](docs/adr/0009-coordination-topology.md)），覆盖 5 种协作场景：任务分工、代码评审、专家咨询、接力交接、方案对决。
