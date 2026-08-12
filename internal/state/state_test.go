package state

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"
)

// applySeq 顺序应用 actions 到新 AppState，reducer 单测辅助。
func applySeq(t *testing.T, acts ...Action) *AppState {
	t.Helper()
	s := &AppState{}
	for _, a := range acts {
		if err := reduce(s, a); err != nil {
			t.Fatalf("reduce %T: %v", a, err)
		}
	}
	return s
}

// unknownAction 仅供测试：包外无法构造（sealed action() 未导出），测 reduce default 分支。
type unknownAction struct{}

func (unknownAction) action() {}

func TestReduceBuildTree(t *testing.T) {
	// happy path：建 workspace → tab → 两个 pane → 设 agent → 切焦点。
	s := applySeq(t,
		AddWorkspace{ID: "w1"},
		NewTab{WorkspaceID: "w1", ID: "t1"},
		NewPane{WorkspaceID: "w1", TabID: "t1", ID: "p1", Cwd: "/home"},
		NewPane{WorkspaceID: "w1", TabID: "t1", ID: "p2", Cwd: "/tmp"},
		SetPaneAgent{WorkspaceID: "w1", TabID: "t1", PaneID: "p1", AgentRef: "codex-1"},
		SetFocus{Focus: Focus{WorkspaceID: "w1", TabID: "t1", PaneID: "p1"}},
	)
	if len(s.Workspaces) != 1 || s.Workspaces[0].ID != "w1" {
		t.Fatalf("workspace: %+v", s)
	}
	w := s.Workspaces[0]
	if len(w.Tabs) != 1 || w.Tabs[0].ID != "t1" {
		t.Fatalf("tab: %+v", w)
	}
	tab := w.Tabs[0]
	if len(tab.Panes) != 2 {
		t.Fatalf("panes: %d", len(tab.Panes))
	}
	if tab.Panes[0].Cwd != "/home" || tab.Panes[0].AgentRef != "codex-1" {
		t.Fatalf("pane0: %+v", tab.Panes[0])
	}
	if s.Focus.PaneID != "p1" {
		t.Fatalf("focus: %+v", s.Focus)
	}
}

func TestReduceCloseCascadeAndFocus(t *testing.T) {
	s := applySeq(t,
		AddWorkspace{ID: "w1"},
		NewTab{WorkspaceID: "w1", ID: "t1"},
		NewPane{WorkspaceID: "w1", TabID: "t1", ID: "p1"},
		SetFocus{Focus{WorkspaceID: "w1", TabID: "t1", PaneID: "p1"}},
	)
	// 关 pane：焦点回退到 tab 级。
	if err := reduce(s, ClosePane{WorkspaceID: "w1", TabID: "t1", ID: "p1"}); err != nil {
		t.Fatalf("close pane: %v", err)
	}
	if s.Focus.PaneID != "" || s.Focus.TabID != "t1" {
		t.Fatalf("focus after close pane: %+v", s.Focus)
	}
	// 关 tab：焦点回退到 workspace 级。
	if err := reduce(s, CloseTab{WorkspaceID: "w1", ID: "t1"}); err != nil {
		t.Fatalf("close tab: %v", err)
	}
	if s.Focus.TabID != "" || s.Focus.WorkspaceID != "w1" {
		t.Fatalf("focus after close tab: %+v", s.Focus)
	}
	// 关 workspace：焦点清空。
	if err := reduce(s, RemoveWorkspace{ID: "w1"}); err != nil {
		t.Fatalf("remove workspace: %v", err)
	}
	if s.Focus != (Focus{}) {
		t.Fatalf("focus after remove workspace: %+v", s.Focus)
	}
}

func TestReduceErrors(t *testing.T) {
	// 每个 case 在独立 clone 上跑，避免串扰。
	base := applySeq(t,
		AddWorkspace{ID: "w1"},
		NewTab{WorkspaceID: "w1", ID: "t1"},
		NewPane{WorkspaceID: "w1", TabID: "t1", ID: "p1"},
	)
	cases := []struct {
		name string
		act  Action
		want error
	}{
		{"dup workspace", AddWorkspace{ID: "w1"}, ErrDuplicate},
		{"dup tab", NewTab{WorkspaceID: "w1", ID: "t1"}, ErrDuplicate},
		{"dup pane", NewPane{WorkspaceID: "w1", TabID: "t1", ID: "p1"}, ErrDuplicate},
		{"tab missing ws", NewTab{WorkspaceID: "nope", ID: "t2"}, ErrNotFound},
		{"pane missing tab", NewPane{WorkspaceID: "w1", TabID: "nope", ID: "p2"}, ErrNotFound},
		{"close pane missing", ClosePane{WorkspaceID: "w1", TabID: "t1", ID: "nope"}, ErrNotFound},
		{"close tab missing ws", CloseTab{WorkspaceID: "nope", ID: "t1"}, ErrNotFound},
		{"setcwd missing pane", SetPaneCwd{WorkspaceID: "w1", TabID: "t1", PaneID: "nope"}, ErrNotFound},
		{"setagent missing ws", SetPaneAgent{WorkspaceID: "nope", TabID: "t1", PaneID: "p1"}, ErrNotFound},
		{"focus missing ws", SetFocus{Focus: Focus{WorkspaceID: "nope"}}, ErrNotFound},
		{"focus pane missing", SetFocus{Focus: Focus{WorkspaceID: "w1", TabID: "t1", PaneID: "nope"}}, ErrNotFound},
		{"focus pane without ws", SetFocus{Focus: Focus{PaneID: "p1"}}, ErrNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := reduce(clone(base), tc.act)
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestSetFocusEmptyResets(t *testing.T) {
	s := applySeq(t,
		AddWorkspace{ID: "w1"},
		NewTab{WorkspaceID: "w1", ID: "t1"},
		SetFocus{Focus{WorkspaceID: "w1", TabID: "t1"}},
	)
	// 全空 Focus = 清空焦点（不报 not-found）。
	if err := reduce(s, SetFocus{Focus{}}); err != nil {
		t.Fatalf("reset focus: %v", err)
	}
	if s.Focus != (Focus{}) {
		t.Fatalf("focus not cleared: %+v", s.Focus)
	}
}

func TestCoreSendSnapshot(t *testing.T) {
	c := New()
	defer c.Stop()

	c.Send(AddWorkspace{ID: "w1"})
	c.Send(NewTab{WorkspaceID: "w1", ID: "t1"})
	c.Send(NewPane{WorkspaceID: "w1", TabID: "t1", ID: "p1", Cwd: "/x"})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	s, err := c.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(s.Workspaces) != 1 || s.Workspaces[0].Tabs[0].Panes[0].ID != "p1" {
		t.Fatalf("state: %+v", s)
	}
}

func TestSnapshotDeepCopyIsolation(t *testing.T) {
	c := New()
	defer c.Stop()
	c.Send(AddWorkspace{ID: "w1"})
	c.Send(NewTab{WorkspaceID: "w1", ID: "t1"})
	c.Send(NewPane{WorkspaceID: "w1", TabID: "t1", ID: "p1", Cwd: "/orig"})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	s1, err := c.Snapshot(ctx)
	if err != nil {
		t.Fatalf("s1: %v", err)
	}
	// 篡改返回的快照（调用方违约改它），核心 state 不应受影响。
	s1.Workspaces[0].ID = "mutated"
	s1.Workspaces[0].Tabs[0].Panes[0].Cwd = "/mutated"

	s2, err := c.Snapshot(ctx)
	if err != nil {
		t.Fatalf("s2: %v", err)
	}
	if s2.Workspaces[0].ID != "w1" {
		t.Fatalf("core workspace id mutated by snapshot edit: %+v", s2)
	}
	if s2.Workspaces[0].Tabs[0].Panes[0].Cwd != "/orig" {
		t.Fatalf("core pane cwd mutated: %+v", s2.Workspaces[0].Tabs[0].Panes[0])
	}
}

func TestConcurrentSendSnapshot(t *testing.T) {
	c := New()
	defer c.Stop()
	c.Send(AddWorkspace{ID: "w"})
	c.Send(NewTab{WorkspaceID: "w", ID: "t"})

	const N = 50
	var wg sync.WaitGroup
	// N 个 goroutine 各发一个 NewPane。
	for i := 0; i < N; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Send(NewPane{WorkspaceID: "w", TabID: "t", ID: fmt.Sprintf("p%d", i)})
		}()
	}
	// 同时并发 Snapshot，验证读路径不与写路径竞态。
	snapDone := make(chan struct{})
	go func() {
		defer close(snapDone)
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _ = c.Snapshot(ctx)
	}()

	wg.Wait()
	<-snapDone

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	s, err := c.Snapshot(ctx)
	if err != nil {
		t.Fatalf("final snapshot: %v", err)
	}
	if got := len(s.Workspaces[0].Tabs[0].Panes); got != N {
		t.Fatalf("pane count: got %d, want %d", got, N)
	}
}

func TestSnapshotContextCancel(t *testing.T) {
	c := New()
	defer c.Stop()
	// 已取消 ctx：Snapshot 应立即返回 context.Canceled，不阻塞。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Snapshot(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
}

func TestStopNoLeak(t *testing.T) {
	before := runtime.NumGoroutine()
	c := New()
	c.Stop()
	// 核心 goroutine 应在 Stop 后退出。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= before {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("goroutine leak: before=%d now=%d", before, runtime.NumGoroutine())
}

func TestStopIdempotent(t *testing.T) {
	c := New()
	c.Stop()
	c.Stop() // 不 panic、不阻塞
}

func TestEmptyStateSnapshot(t *testing.T) {
	c := New()
	defer c.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	s, err := c.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if s == nil {
		t.Fatal("nil snapshot")
	}
	if len(s.Workspaces) != 0 {
		t.Fatalf("expected 0 workspaces, got %d", len(s.Workspaces))
	}
}

func TestReduceUnknownAction(t *testing.T) {
	// reduce 对未知 action 类型返回 ErrUnknownAction（sealed 防外部构造，此处包内模拟）。
	if err := reduce(&AppState{}, unknownAction{}); !errors.Is(err, ErrUnknownAction) {
		t.Fatalf("got %v, want ErrUnknownAction", err)
	}
}

func TestCloneNil(t *testing.T) {
	// clone 对 nil 输入安全返回 nil；对空 state 返回非 nil 空拷贝。
	if clone(nil) != nil {
		t.Fatal("clone(nil) should return nil")
	}
	c := clone(&AppState{})
	if c == nil || len(c.Workspaces) != 0 {
		t.Fatalf("clone empty: %+v", c)
	}
}
