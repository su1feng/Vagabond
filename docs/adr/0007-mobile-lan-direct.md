# ADR 0007: 移动端 LAN-direct + AO bearer 认证起步，可升级 Orca E2EE

- **状态**：Accepted
- **日期**：2026-08-11
- **对应**：Rule 2（daemon 拥有一切）、Rule 3（字节流渲染）

## 背景（Context）

Vagabond 需要移动端，让用户在手机上看/控 agent 会话。两个关键问题：连什么（网络拓扑）、怎么认证/加密。

## 决策（Decision）

### 网络拓扑：LAN-direct

手机与 daemon 直接在同一局域网通信，**不经云端中转**：

- daemon 在本机监听 LAN 端口（对标 agent-orchestrator 的 mobile LAN listener ADR，端口 3011）。
- 手机直连 daemon 的 IP:port。
- 全字节流推到手机，用 **xterm.js-in-WebView** 渲染（对标 Orca/AO/TermLoop 的移动终端渲染）。

### 认证：AO bearer 起步 → 可升级 Orca E2EE

- **v1：AO bearer 模式**——daemon 启动时生成轮换的 8 字符 bearer token，手机配对时输入。对标 agent-orchestrator。
- **未来升级：Orca E2EE**——Curve25519 ECDH 密钥协商 + XSalsa20-Poly1305 加密。对标 Orca 的 `mobile/src/transport/e2ee.ts`。

升级路径：bearer（防局域网他人偷连）→ E2EE（防局域网嗅探/中间人）。

## 为什么是这套

- **LAN-direct**：无云依赖、零延迟、无订阅费；agent 数据不出局域网。对标 AO/Orca/TermLoop 的实际做法。
- **全字节流到手机**：研究确认业界主流就是全字节流 + xterm.js WebView，手机端渲染终端可行（Orca/AO/TermLoop 验证过）。
- **bearer→E2EE 渐进**：v1 用最简认证先跑通；E2EE 作为安全升级，不阻塞 v1。

## 考虑过的替代方案（Alternatives Considered）

| 方案 | 为什么不选 |
|---|---|
| **云中转** | 延迟、订阅费、数据出本地、依赖云可用性。LAN-direct 足够且更简单。 |
| **只推"派生事件流"（监控摘要）而非全字节流** | 早期曾考虑，但研究显示业界主流就是全字节流到手机，xterm.js WebView 能跑。派生流是额外工作量，无必要。 |
| **一步到位 E2EE** | 增加复杂度（密钥协商/轮换/撤销），推迟 v1。bearer 先跑通，E2EE 作为升级路径。 |

## 安全模型

- bearer 防的：局域网内其他人未经授权连接 daemon。
- E2EE 防的：局域网嗅探、中间人、WiFi 窃听——bearer token 本身在传输中可能被嗅到，E2EE 从根上加密。

## 远程部署扩展（v2 场景，当前不实现）

E2EE 除了上面说的局域网安全，还**启用远程访问**：daemon 跑在一台常开的服务器上（自己的服务器 / VPS / 家里常开的小机器），人在外地用手机连。

这场景需要三层：

| 层 | 内容 | 属于 |
|---|---|---|
| 常开服务器 | daemon + agent CLI + 项目仓库跑在一台不关机的机器上 | ops |
| 网络可达 | Tailscale（推荐）/ VPN / 公网 IP——让手机能连到服务器 | ops / 外部工具 |
| 加密认证 | E2EE（Orca 模式）从"可选"变成"必须"（直接暴露公网时还要 TLS） | Vagabond 代码 |

关键：**架构不用改**（Rule 2 已决定 daemon 是中心，客户端是瘦的），远程部署主要是 ops + E2EE 升级，不是新架构。LAN-direct 是 v1（同网），远程部署是 v2（任意位置），E2EE 是 v2 的入场券。

> v1 不实现远程部署。详细的三种接入方式（tunnel / VPN / 直暴露）+ 工作环境要求（agent/仓库搬到服务器）+ 结果如何回看（终端 + git/PR），待需要时写 `docs/design/remote-deploy.md`。

## 结果（Consequences）

- 优点：无云依赖；手机端能看到与桌面一致的终端体验；认证可平滑升级。
- 代价：仅限同一局域网（跨网络需 VPN/未来云relay）；WebView 性能弱于原生终端。
- 移动端代码位置：独立目录（如 `mobile/`），不进 `internal/`。

## 参考（References）

- agent-orchestrator `docs/adr/0001-lan-listener-for-mobile.md`（端口 3011，8 字符 bearer）
- Orca `mobile/src/transport/e2ee.ts`（Curve25519 + XSalsa20-Poly1305）
