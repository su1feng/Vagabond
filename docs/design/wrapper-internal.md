# Wrapper 内部结构

> 对应 Guardrail 8、9、10；ADR 0001、0003。
> 本文档是设计骨架，待实现时细化。

## 职责

每个 agent 套一个 wrapper goroutine（在 `internal/wrapper/`）。wrapper 是协作的基本单元，有两个面：

```
┌──────────────────────── wrapper goroutine ────────────────────────┐
│                                                                    │
│  终端面                          端点面                             │
│  ┌───────────────────┐          ┌────────────────────────────┐    │
│  │ 管理 agent PTY     │          │ A2A gRPC server            │    │
│  │ (creack/pty)       │          │ over Unix socket           │    │
│  │                    │          │ /run/vagabond/<agent>.sock │    │
│  │ 读 PTY 字节 ───────┼──────────┼─→ 推给 daemon（字节流渲染） │    │
│  │                    │          │                            │    │
│  │ 写 PTY（注入）←────┼──────────┼── 收到 peer 请求时注入      │    │
│  │   ↑                │          │   （等 idle，Rule 3+6）     │    │
│  │   VT 状态感知      │          │                            │    │
│  │   (working/idle)   │          │ A2A 方法:                   │    │
│  │                    │          │  message/send              │    │
│  └───────────────────┘          │  message/stream            │    │
│                                  │  task/get ...              │    │
│                                  └────────────────────────────┘    │
│                                                                    │
│  MCP server（给本 agent 调）                                       │
│  ┌──────────────────────────────────────────────────────────┐     │
│  │ agent 作为 MCP client 连这里，调:                          │     │
│  │   ask_agent(peer, q, mode)  ← 发起咨询（同步/异步）        │     │
│  │   get_task(task_id)         ← 拉任务结果/状态              │     │
│  │   broadcast(msg)            ← 广播通知                     │     │
│  │   list_peers()              ← 查 discovery 目录            │     │
│  └──────────────────────────────────────────────────────────┘     │
└────────────────────────────────────────────────────────────────────┘
```

## 三个接口

### 1. 终端面（PTY）
- 用 `internal/pty/` 启动 agent 子进程，拿 PTY。
- 读 PTY 字节 → 推给 daemon（走 channel，Rule 1）→ daemon 推给客户端渲染。
- VT 翻译器（Rule 3）感知 agent 状态（working/idle/blocked），供注入时机判断。

### 2. 端点面（A2A gRPC server）
- 监听 Unix socket `/run/vagabond/<agent-id>.sock`。
- 实现 A2A 方法（`message/send` 等），收到 peer 请求 → 注入 agent PTY → 等 agent 回应 → 返回。
- 注入时机：等 agent idle（Rule 3 检测 + Rule 6 wait），不打断输出。

### 3. MCP server（给本 agent 用）
- agent 作为 MCP client 连接本 wrapper 的 MCP server。
- 暴露 `ask_agent` / `get_task` / `broadcast` / `list_peers` 等工具。
- agent 通过这些工具发起协作。

## 与 daemon 的 channel 接口

wrapper 是 goroutine，不直接改 AppState（Rule 1）。通过 channel 与 daemon 核心 goroutine 通信：

- wrapper → daemon：字节流（渲染用）、状态变更（agent idle/working）、discovery 注册。
- daemon → wrapper：注入指令（罕见，通常 wrapper 自己决定注入）、生命周期控制（stop）。

## 待细化

- [ ] 注入文本格式约定（统一前缀标记，让 agent 识别为 peer 消息而非噪音）。
- [ ] agent 回应捕获：如何判断 agent 的哪段输出是"对 peer 请求的回答"（VT 状态 idle 边界？特定标记？）。
- [ ] Task store 在 wrapper 内还是 daemon 内（多 wrapper 共享需在 daemon）。
- [ ] wrapper 启动/停止生命周期与 agent 进程的绑定。
