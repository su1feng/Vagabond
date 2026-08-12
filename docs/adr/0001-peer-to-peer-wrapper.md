# ADR 0001: Peer-to-peer wrapper 协作，而非中心 broker relay

- **状态**：Accepted
- **日期**：2026-08-11
- **对应 Guardrail**：8

## 背景（Context）

Vagabond 要让同一项目内的多个异构 agent（codex/reasonix/kimi-code/claude-code 等）互相协作。核心问题是：这些 agent 之间以什么拓扑通信？

两种候选拓扑：

- **星型 broker**：所有消息经 daemon 转发，daemon 决定路由与投递。
- **peer-to-peer（wrapper 直连）**：每个 agent 套一个 wrapper，wrapper 之间直接通信。

终端形态带来一个硬约束：只有拥有某 agent PTY 的进程才能往它的输入里写字。纯终端的 agent 没有"端点"，无法被 peer 直接调用，因此一定需要"拥有 PTY 的那方"帮忙注入。但这不等于必须有一个"主动路由决策"的中心 broker。

## 决策（Decision）

采用 **peer-to-peer wrapper 拓扑**：

- 每个 agent 套一个 wrapper goroutine，wrapper 同时提供：
  - **终端面**：管理 agent 的 PTY（字节流渲染，给用户看）。
  - **端点面**：暴露 A2A gRPC server（给 peer 调）。
- wrapper 之间直接通信，**不走中心 relay**。
- daemon 的角色收窄为：wrapper 宿主（生命周期）+ 终端渲染 + discovery（agent 目录）。daemon **不做路由或协调决策**。
- 协调（谁干啥、谁 review 谁）交给 **coordinator agent** 或**人**，不是基础设施的职责。

## 与"broker"的本质区别

broker 是**主动路由决策者**：每条消息由它决定往哪转。wrapper 直连里，daemon 只是 wrapper goroutine 的**宿主**——wrapper A 直接调 wrapper B，如同进程内两个函数互调。物理上消息在 daemon 进程内经过，但 daemon 不做任何路由判断。

## 考虑过的替代方案（Alternatives Considered）

| 方案 | 为什么不选 |
|---|---|
| **纯终端 + broker relay** | 每条消息过中心，daemon 是瓶颈 + 单点故障 + 能偷听一切；不是真正意义上的 A2A（是 A→broker→B）。比 orca/Claude Code Teams 的实际做法都重。 |
| **纯 peer-to-peer（agent 暴露端点，无任何中间层）** | agent 跑成交互终端时是 MCP client（连出去用工具），不是 server（没有端点）。要让 codex 直接被 kimi-code 调用，codex 必须额外开服务，破坏终端形态。 |
| **coordinator-agent 唯一中心（orca 式）** | 可作为协调层的一种实现，但通信原语仍应是 peer-to-peer；coordinator 是"角色"而非"消息总线"。本 ADR 把协调与通信分开：通信 peer-to-peer，协调可选地由某 agent 担任。 |

## 业界对标

- **Claude Code Agent Teams**：lead agent 负责分任务；teammate 之间**直接按名字发消息**（经共享邮箱文件），没有 daemon relay。本 ADR 与其同构，只是把"Claude runtime 自动轮询"换成"wrapper 端点 + PTY 注入"，以支持异构 agent。
- **orca**：coordinator agent fan-out worker，worker 经 mailbox 报告；coordinator 是 agent 不是基础设施。显式 non-goal："No commit, branch, merge, integration, or target-ref tracking"，"Agents choose decomposition, topology, placement"。
- **agent-orchestrator**：daemon 只转发外部事件（GitHub/CI），不 relay agent 间消息。

## 结果（Consequences）

- 优点：拓扑更接近"真 A2A"；daemon 不是消息瓶颈；协调逻辑可插拔（agent 或人都行）。
- 代价：每个 agent 需要 wrapper（终端面 + 端点面），增加一层抽象。
- 端点面如何注入 PTY、何时注入 → 见 [ADR 0003](0003-a2a-task-model.md)。
- wrapper 之间用什么协议 → 见 [ADR 0002](0002-a2a-grpc-unix-socket.md)。

## 参考（References）

- Claude Code Agent Teams docs：https://code.claude.com/docs/en/agent-teams
- orca `ORCHESTRATION_IMPLEMENTATION_CHECKLIST.md`（non-goals 段）
- agent-orchestrator spawn 命令文档
