// Package state 实现 Vagabond daemon 的核心状态树及其单持有者状态机。
//
// 这是 Rule 1（单持有者状态）的落地：AppState 三级树（workspace → tab → pane）
// 只被一个核心 goroutine（Core）持有；外部（daemon 连接 worker / api / render）
// 只能通过 Core.Send 发 Action 变更状态、Core.Snapshot 查询快照，绝不直接改 state。
//
// 字段二分（Rule 5 轻量快照）：Cwd / AgentRef 是持久化字段；未来的 PTY 引用、
// agent 运行态、字节流属运行时字段，绝不落盘（persist 批次提取时按此边界）。
//
// 演进（本批不实现）：layout 布局坐标（布局引擎批次）、agent 运行态
// idle/working/blocked（discovery 批次）、coordination state（ADR 0010 v2）。
package state

// AppState 是状态树根，仅被 Core 的核心 goroutine 持有（Rule 1）。
type AppState struct {
	Workspaces []*Workspace
	Focus      Focus
}

// Focus 标记当前聚焦的 workspace/tab/pane，用于输入路由与 render 高亮。
type Focus struct {
	WorkspaceID string
	TabID       string
	PaneID      string
}

// Workspace 是顶层容器，对应一个项目/worktree（discovery 的 workspace 字段）。
type Workspace struct {
	ID   string
	Tabs []*Tab
	// coordination state 扩展点（ADR 0010 v1 设计预留，本批不实现）：
	// 未来挂 decisions/owners/locks/checkpoints（versioned CAS）。挂 workspace 级
	// 为默认（跨 pane 共享事实），但归属 ADR 0010 标记为待细化。
}

// Tab 是 workspace 内的标签页，聚合若干 pane。
type Tab struct {
	ID    string
	Panes []*Pane
	// 布局坐标（Rule 5 持久化字段）—— 留给布局引擎批次定义 split/比例/rect。
}

// Pane 是单个终端面，绑定一个 agent 会话。
type Pane struct {
	ID       string
	Cwd      string // 工作目录，持久化（Rule 5）
	AgentRef string // agent 会话引用，持久化（Rule 5）
	// 运行时字段（不落盘）后续批次：PTY 引用（pty 批次）、agent 运行态
	// idle/working/blocked（归属待定，可能 discovery 目录而非此处）。
}

// clone 返回 AppState 的深拷贝，用于 Snapshot 只读快照。调用方约定不改返回值。
func clone(s *AppState) *AppState {
	if s == nil {
		return nil
	}
	out := &AppState{Focus: s.Focus, Workspaces: make([]*Workspace, len(s.Workspaces))}
	for i, w := range s.Workspaces {
		out.Workspaces[i] = cloneWorkspace(w)
	}
	return out
}

func cloneWorkspace(w *Workspace) *Workspace {
	if w == nil {
		return nil
	}
	out := &Workspace{ID: w.ID, Tabs: make([]*Tab, len(w.Tabs))}
	for i, t := range w.Tabs {
		out.Tabs[i] = cloneTab(t)
	}
	return out
}

func cloneTab(t *Tab) *Tab {
	if t == nil {
		return nil
	}
	out := &Tab{ID: t.ID, Panes: make([]*Pane, len(t.Panes))}
	for i, p := range t.Panes {
		out.Panes[i] = clonePane(p)
	}
	return out
}

func clonePane(p *Pane) *Pane {
	if p == nil {
		return nil
	}
	return &Pane{ID: p.ID, Cwd: p.Cwd, AgentRef: p.AgentRef}
}
