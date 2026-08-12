# ADR 0006: GUI 用 Electron + React + xterm.js

- **状态**：Accepted
- **日期**：2026-08-11
- **对应**：Rule 3（字节流渲染）

## 背景（Context）

Vagabond 需要桌面 GUI。核心约束：Rule 3 字节流渲染——daemon 推原始字节流，客户端用终端模拟器渲染。GUI 必须能消费这串字节并画成终端。

## 决策（Decision）

桌面 GUI 采用 **Electron + React + xterm.js**：

- **Electron**：跨平台桌面壳（Windows/macOS/Linux）。
- **React**：UI 框架（多 pane 布局、会话列表、控制面板）。
- **xterm.js**：终端模拟器组件，直接消费 daemon 推来的字节流，画成终端。

## 为什么是这套

| 组件 | 角色 | 与架构的契合 |
|---|---|---|
| xterm.js | 把字节流画成终端 | 直接对应 Rule 3——daemon 推字节，xterm.js 渲染，零中间解析 |
| Electron | 跨平台桌面容器 | 对标 orca/lody/cumora/agent-orchestrator 的桌面形态 |
| React | 围绕终端的 UI（布局/会话管理/控制） | 多 session 管理是 herdr 形态的骨架 |

xterm.js 是关键：它就是一个终端模拟器，吃的正是 VT100/ANSI 字节流。daemon 推什么它画什么，跟用户直接跑 agent 看到的一模一样。

## 考虑过的替代方案（Alternatives Considered）

| 方案 | 为什么不选 |
|---|---|
| **Tauri + xterm.js** | 更轻（不用 Chromium），但 Tauri 生态/稳定性弱于 Electron，且 xterm.js 集成成熟度不如 Electron。可作为未来轻量化选项。 |
| **原生 + 自研终端渲染** | 重新发明 xterm.js，无收益。 |
| **结构化渲染（解析 agent 输出为事件，自绘 UI）** | 放弃字节流渲染（违反 Rule 3），且需为每个 agent 写解析器，违背"零适配"。 |

## 与 TUI（bubbletea）的关系

TUI（`internal/client/`，bubbletea）和 GUI（Electron）都消费同一份字节流，只是渲染器不同：

- TUI：bubbletea + 终端 VT 渲染（本地终端里跑）。
- GUI：Electron + xterm.js（桌面窗口里跑）。

两者都是 Rule 2 意义上的"不持有状态的客户端"，重连从 daemon 重新同步。

## 结果（Consequences）

- 优点：字节流渲染零适配（任何 agent 自动能显示）；跨平台；xterm.js 成熟。
- 代价：Electron 体积大（Chromium 基底）；内存占用高于原生。
- GUI 代码位置：独立目录（如 `gui/` 或 `desktop/`），不进 `internal/`（internal 是 daemon 的）。

## 参考（References）

- xterm.js：https://github.com/xtermjs/xterm.js
- 对标项目：orca、lody、cumora、agent-orchestrator 的 GUI 形态
