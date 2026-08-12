# A2A-HTTP Gateway（未来，v2）

> 状态：**未实现 / 规划中**。
> 对应 ADR 0002 的"未来外部接入"部分。
> 本文档是占位骨架，v1 不实现。

## 目标

让**外部 agent**（不在本机、第三方系统的 agent）能用标准 A2A 协议接入 Vagabond 的协作。

## 为什么 v1 不做

- v1 场景全是同机、同用户、Vagabond 进程内的异构 agent 协作，wrapper 间走 Unix socket 足够。
- 外部接入需暴露网络端口 + 认证 + 安全考量，v1 不阻塞。

## 架构设想

```
外部 agent（A2A client，HTTP）
        │
        ▼ 标准 A2A JSON-RPC over HTTPS
 Vagabond 边缘 A2A-HTTP gateway
        │
        ▼ 协议互转（HTTP↔gRPC，都是 A2A binding）
 Vagabond 内部 wrapper（gRPC over socket）
```

- gateway 对外讲标准 A2A（JSON-RPC/HTTPS），对内转发到 wrapper 的 A2A gRPC over socket。
- 这是标准协议间的互转（两种 A2A binding），不是自定义翻译，干净。
- 认证：复用移动端的 bearer/E2EE 模型，或 A2A Agent Card 的 OAuth/API key。

## 待细化（未来）

- [ ] gateway 暴露的端口与认证。
- [ ] 外部 agent 在 discovery 目录中的注册方式。
- [ ] 安全边界：外部 agent 能访问哪些内部 agent/worktree。
