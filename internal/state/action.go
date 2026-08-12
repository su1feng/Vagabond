package state

import "errors"

// Action 是对 AppState 的变更动作（Project Map「变更动作」）。
// sealed 接口：标记方法 action() 未导出，限制只能在包内实现，防止外部扩展。
// Rule 1：Action 经 Core.Send 投递到核心 goroutine，由 reduce 串行应用；
// 外部绝不直接改 state。
type Action interface {
	action()
}

// Sentinel errors 由 reduce 返回：target 不存在 / ID 冲突。
var (
	ErrNotFound      = errors.New("state: target not found")
	ErrDuplicate     = errors.New("state: duplicate id")
	ErrUnknownAction = errors.New("state: unknown action")
)

// reduce 应用 act 到 s（mutate-in-place）。仅在 Core 核心 goroutine 内单线程调用，
// 天然无竞态。返回 sentinel error 表示 target 不存在或冲突；核心循环记录后忽略。
func reduce(s *AppState, act Action) error {
	switch a := act.(type) {
	case AddWorkspace:
		return reduceAddWorkspace(s, a)
	case RemoveWorkspace:
		return reduceRemoveWorkspace(s, a)
	case NewTab:
		return reduceNewTab(s, a)
	case CloseTab:
		return reduceCloseTab(s, a)
	case NewPane:
		return reduceNewPane(s, a)
	case ClosePane:
		return reduceClosePane(s, a)
	case SetPaneCwd:
		return reduceSetPaneCwd(s, a)
	case SetPaneAgent:
		return reduceSetPaneAgent(s, a)
	case SetFocus:
		return reduceSetFocus(s, a)
	default:
		return ErrUnknownAction
	}
}

// --- Actions ---
//
// 每个 Action 携带定位层级 ID（{WorkspaceID, TabID?, PaneID?}）+ 载荷。
// 任何层级定位失败返回 ErrNotFound；同级 ID 冲突返回 ErrDuplicate。

// AddWorkspace 新增一个空 workspace。
type AddWorkspace struct {
	ID string
}

func (AddWorkspace) action() {}

// RemoveWorkspace 删除 workspace（连同其 tab/pane）；若焦点指向它则重置焦点。
type RemoveWorkspace struct {
	ID string
}

func (RemoveWorkspace) action() {}

// NewTab 在指定 workspace 内新增空 tab。
type NewTab struct {
	WorkspaceID string
	ID          string
}

func (NewTab) action() {}

// CloseTab 删除 tab（连同其 pane）；若焦点指向它则回退到 workspace 级焦点。
type CloseTab struct {
	WorkspaceID string
	ID          string
}

func (CloseTab) action() {}

// NewPane 在指定 tab 内新增 pane（带初始 cwd）。
type NewPane struct {
	WorkspaceID string
	TabID       string
	ID          string
	Cwd         string
}

func (NewPane) action() {}

// ClosePane 删除 pane；若焦点指向它则回退到 tab 级焦点。
type ClosePane struct {
	WorkspaceID string
	TabID       string
	ID          string
}

func (ClosePane) action() {}

// SetPaneCwd 设置 pane 的工作目录。
type SetPaneCwd struct {
	WorkspaceID string
	TabID       string
	PaneID      string
	Cwd         string
}

func (SetPaneCwd) action() {}

// SetPaneAgent 设置 pane 绑定的 agent 会话引用。
type SetPaneAgent struct {
	WorkspaceID string
	TabID       string
	PaneID      string
	AgentRef    string
}

func (SetPaneAgent) action() {}

// SetFocus 设置焦点。校验目标存在性（空字段表示重置该级焦点；全空 = 清空焦点）。
type SetFocus struct {
	Focus Focus
}

func (SetFocus) action() {}

// --- reducer 实现 ---

func reduceAddWorkspace(s *AppState, a AddWorkspace) error {
	if findWorkspace(s, a.ID) != nil {
		return ErrDuplicate
	}
	s.Workspaces = append(s.Workspaces, &Workspace{ID: a.ID})
	return nil
}

func reduceRemoveWorkspace(s *AppState, a RemoveWorkspace) error {
	for i, w := range s.Workspaces {
		if w.ID == a.ID {
			s.Workspaces = append(s.Workspaces[:i], s.Workspaces[i+1:]...)
			if s.Focus.WorkspaceID == a.ID {
				s.Focus = Focus{} // 焦点指向被删 workspace：重置（不自动选下一个，避免隐式策略）
			}
			return nil
		}
	}
	return ErrNotFound
}

func reduceNewTab(s *AppState, a NewTab) error {
	w := findWorkspace(s, a.WorkspaceID)
	if w == nil {
		return ErrNotFound
	}
	if findTab(w, a.ID) != nil {
		return ErrDuplicate
	}
	w.Tabs = append(w.Tabs, &Tab{ID: a.ID})
	return nil
}

func reduceCloseTab(s *AppState, a CloseTab) error {
	w := findWorkspace(s, a.WorkspaceID)
	if w == nil {
		return ErrNotFound
	}
	for i, t := range w.Tabs {
		if t.ID == a.ID {
			w.Tabs = append(w.Tabs[:i], w.Tabs[i+1:]...)
			if s.Focus.WorkspaceID == a.WorkspaceID && s.Focus.TabID == a.ID {
				s.Focus = Focus{WorkspaceID: a.WorkspaceID}
			}
			return nil
		}
	}
	return ErrNotFound
}

func reduceNewPane(s *AppState, a NewPane) error {
	w := findWorkspace(s, a.WorkspaceID)
	if w == nil {
		return ErrNotFound
	}
	t := findTab(w, a.TabID)
	if t == nil {
		return ErrNotFound
	}
	if findPane(t, a.ID) != nil {
		return ErrDuplicate
	}
	t.Panes = append(t.Panes, &Pane{ID: a.ID, Cwd: a.Cwd})
	return nil
}

func reduceClosePane(s *AppState, a ClosePane) error {
	t := mustTab(s, a.WorkspaceID, a.TabID)
	if t == nil {
		return ErrNotFound
	}
	for i, p := range t.Panes {
		if p.ID == a.ID {
			t.Panes = append(t.Panes[:i], t.Panes[i+1:]...)
			if s.Focus.WorkspaceID == a.WorkspaceID && s.Focus.TabID == a.TabID && s.Focus.PaneID == a.ID {
				s.Focus = Focus{WorkspaceID: a.WorkspaceID, TabID: a.TabID}
			}
			return nil
		}
	}
	return ErrNotFound
}

func reduceSetPaneCwd(s *AppState, a SetPaneCwd) error {
	p := mustPane(s, a.WorkspaceID, a.TabID, a.PaneID)
	if p == nil {
		return ErrNotFound
	}
	p.Cwd = a.Cwd
	return nil
}

func reduceSetPaneAgent(s *AppState, a SetPaneAgent) error {
	p := mustPane(s, a.WorkspaceID, a.TabID, a.PaneID)
	if p == nil {
		return ErrNotFound
	}
	p.AgentRef = a.AgentRef
	return nil
}

func reduceSetFocus(s *AppState, a SetFocus) error {
	f := a.Focus
	// 由浅入深校验：非空字段必须指向存在的 target。
	if f.WorkspaceID != "" && findWorkspace(s, f.WorkspaceID) == nil {
		return ErrNotFound
	}
	if f.TabID != "" {
		w := findWorkspace(s, f.WorkspaceID)
		if w == nil || findTab(w, f.TabID) == nil {
			return ErrNotFound
		}
	}
	if f.PaneID != "" && mustPane(s, f.WorkspaceID, f.TabID, f.PaneID) == nil {
		return ErrNotFound
	}
	s.Focus = f
	return nil
}

// --- finders ---

func findWorkspace(s *AppState, id string) *Workspace {
	for _, w := range s.Workspaces {
		if w.ID == id {
			return w
		}
	}
	return nil
}

func findTab(w *Workspace, id string) *Tab {
	for _, t := range w.Tabs {
		if t.ID == id {
			return t
		}
	}
	return nil
}

func findPane(t *Tab, id string) *Pane {
	for _, p := range t.Panes {
		if p.ID == id {
			return p
		}
	}
	return nil
}

// mustTab 返回指定 workspace/tab，任一层缺失返回 nil。
func mustTab(s *AppState, wsID, tabID string) *Tab {
	w := findWorkspace(s, wsID)
	if w == nil {
		return nil
	}
	return findTab(w, tabID)
}

// mustPane 返回指定 workspace/tab/pane，任一层缺失返回 nil。
func mustPane(s *AppState, wsID, tabID, paneID string) *Pane {
	t := mustTab(s, wsID, tabID)
	if t == nil {
		return nil
	}
	return findPane(t, paneID)
}
