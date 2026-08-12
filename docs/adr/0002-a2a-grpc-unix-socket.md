# ADR 0002: wrapper 间通信用 A2A gRPC over Unix socket

- **状态**：Accepted
- **日期**：2026-08-11
- **对应 Guardrail**：10

## 背景（Context）

[ADR 0001](0001-peer-to-peer-wrapper.md) 决定 wrapper 之间直接通信。本 ADR 决定它们用什么协议对话。

两个独立的选型维度：

- **维度 1（传输）**：Unix socket（无端口管理）vs TCP/HTTP（要管端口分配/冲突/生命周期）。
- **维度 2（协议）**：自定义协议 vs 标准 A2A（Task/Message/Part/Artifact + 生命周期）。

## 决策（Decision）

采用 **A2A gRPC binding，跑在 Unix socket 上**。

- 协议层：标准 A2A v1.0 的 gRPC binding（`spec/a2a.proto` 是 normative 定义）。
- 传输层：Unix domain socket，路径如 `/run/vagabond/<agent-id>.sock`。
- 不跑 TCP，不管端口。

## 为什么是这两个维度的最优组合

| | Unix socket（无端口） | TCP/HTTP（要管端口） |
|---|---|---|
| **标准 A2A** | ✅ **本 ADR（选项 C）** | B：A2A over HTTP |
| **自定义协议** | A：自定义 + socket | — |

C = A 的传输 + B 的协议。把两边的好处都拿了：

- 比 A（自定义）强：用标准 A2A 数据模型与官方 Go SDK，不自己维护一套语义，未来外部对接不用翻译。
- 比 B（A2A+HTTP）强：不跑 HTTP server，无端口管理（分配/冲突/生命周期）。8 个 agent 就是 8 个 socket 文件，天然隔离。

## gRPC over Unix socket 可行性

gRPC-Go 原生支持 Unix socket 作为 transport：`grpc.NewClient("unix:///run/vagabond/x.sock")`（target 形式为 `unix://` + 绝对路径）。containerd、kubelet 都用这个模式做本地 IPC，成熟可靠。A2A v1.0 的 gRPC binding 是正式规范，官方 Go SDK（`github.com/a2aproject/a2a-go`）支持。

## 考虑过的替代方案（Alternatives Considered）

| 方案 | 为什么不选 |
|---|---|
| **A：自定义协议 + Unix socket** | 传输同样轻，但协议是自己发明的；未来每个客户端/外部对接都得按自定义协议来，改协议=全部返工。被 C 严格优于（除非有必须偏离 A2A 数据模型的特殊需求）。 |
| **B：A2A over HTTP（localhost）** | 协议标准，但每个 wrapper 跑 HTTP server，要伺候端口（分配/防冲突/生命周期）。外部接入本来就该走 Vagabond 边缘 gateway，不是直连裸端口。 |
| **全盘 A2A wire protocol（含对外 HTTP）** | 见上；同机太重。对外用 gateway 转接即可。 |
| **纯 MCP（wrapper 间）** | MCP 是 request/response，异步不顺（无法干净表达"立即返回 task_id，结果到了再推送"）；MCP 的 notification 不保证能唤醒 agent 注意力。 |
| **纯 PTY 注入做唯一通道** | 注入时机/格式/语义都有代价（见 [ADR 0003](0003-a2a-task-model.md)）；只适合唤醒 idle agent，不适合做结构化通信。 |

## 与 Rule 4（双 socket）的关系

Rule 4 禁止 protobuf，针对的是**客户端 socket**（JSON API socket + 二进制 client socket）。本 ADR 的 A2A gRPC 是 **wrapper 间的内部 IPC 通道**，与客户端 socket 分离。此处用 protobuf（A2A gRPC 标准定义）不违反 Rule 4——Rule 4 的禁令范围明确限定在客户端 socket。

## 结果（Consequences）

- 优点：标准 A2A 协议（可用官方 SDK、未来互通）+ 无端口管理；进程内 gRPC 开销可忽略。
- 代价：引入 gRPC/protobuf machinery；gRPC + Unix socket 落地已验证（见"已验证"）。
- 外部 agent 接入：未来在 Vagabond 边缘加 A2A-HTTP gateway（HTTP↔gRPC 协议互转），外部用标准 A2A 接入，内部仍走 socket。

## 已验证（2026-08-12）

写最小验证程序确认 A2A 官方 Go SDK 的 gRPC binding 能跑在 Unix socket 上（多次稳定复现）：

- **server**：`a2agrpc.NewHandler` 注册到 `grpc.Server`，`Serve(net.Listen("unix", path))`——收的是 `net.Listener`，换 Unix socket 零成本。
- **client**：`grpc.NewClient("unix://"+absPath)` + `a2aclient.NewGRPCTransport(conn)`，**可绕过 HTTP `.well-known` AgentCard 解析直连 socket**——契合纯本地 discovery 场景。
- unary `SendMessage` 与流式 `SendStreamingMessage` 均通过。

验证记下三点：

1. **SDK 路径订正**：`a2aproject/A2A`（规范主仓）→ [`a2aproject/a2a-go`](https://github.com/a2aproject/a2a-go)（Go SDK）。原文 References 写错。
2. **Unix socket target 格式**：`"unix://" + 绝对路径`（如 `unix:///tmp/x.sock`，三个斜杠）。常见踩坑点，实现 `internal/a2a/` 时写进注释。
3. **成熟度**：a2a-go 当前 **v0.3.15，pre-1.0**，API 可能 breaking change。可用，但需锁版本；消费方不需装 protoc（SDK 自带预生成 `a2apb/`）。

## 待验证

- Agent Card 在纯本地（非 `.well-known` HTTP）场景下的发现方式（`list_peers` 目录机制，见 `docs/design/discovery.md`）。已验证程序确认 client 可绕过 HTTP card 直连 socket，但目录注册/查询/心跳本身尚未实现。

## 参考（References）

- A2A spec v1.0：https://a2a-protocol.org/latest/specification/
- A2A Go SDK：https://github.com/a2aproject/a2a-go
- gRPC-Go Unix socket 支持
