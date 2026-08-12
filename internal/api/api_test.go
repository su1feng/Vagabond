package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/su1feng/Vagabond/internal/state"
)

// newTestHandler 创建绑定到新 Core 的 Handler，测试结束自动 Stop。
func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	c := state.New()
	t.Cleanup(c.Stop)
	return New(c)
}

// do 发送一个 JSON 请求并解码响应（测试辅助）。
func do(t *testing.T, h *Handler, req string) Response {
	t.Helper()
	out, err := h.Handle(context.Background(), []byte(req))
	if err != nil {
		t.Fatalf("Handle(%q): %v", req, err)
	}
	var resp Response
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal response for %q: %v (raw=%s)", req, err, out)
	}
	return resp
}

func TestHandleSnapshot(t *testing.T) {
	h := newTestHandler(t)
	// 空状态 snapshot。
	resp := do(t, h, `{"method":"snapshot"}`)
	if !resp.OK || resp.State == nil {
		t.Fatalf("resp: %+v", resp)
	}
	if len(resp.State.Workspaces) != 0 {
		t.Fatalf("expected empty, got %+v", resp.State)
	}
}

func TestHandleWriteMethodsAndSnapshot(t *testing.T) {
	h := newTestHandler(t)
	// 发一串写（fire-and-forget），各应 OK=true。
	reqs := []string{
		`{"method":"add-workspace","id":"w1"}`,
		`{"method":"new-tab","workspace":"w1","id":"t1"}`,
		`{"method":"new-pane","workspace":"w1","tab":"t1","id":"p1","cwd":"/x"}`,
		`{"method":"new-pane","workspace":"w1","tab":"t1","id":"p2","cwd":"/y"}`,
		`{"method":"set-pane-agent","workspace":"w1","tab":"t1","pane":"p1","agent":"codex-1"}`,
		`{"method":"set-pane-cwd","workspace":"w1","tab":"t1","pane":"p2","cwd":"/z"}`,
		`{"method":"set-focus","workspace":"w1","tab":"t1","pane":"p1"}`,
	}
	for _, r := range reqs {
		if resp := do(t, h, r); !resp.OK {
			t.Fatalf("write %q failed: %+v", r, resp)
		}
	}
	// snapshot 验证写生效（fire-and-forget 靠 snapshot 确认）。
	resp := do(t, h, `{"method":"snapshot"}`)
	if !resp.OK || resp.State == nil {
		t.Fatalf("snapshot: %+v", resp)
	}
	w := resp.State.Workspaces[0]
	if w.ID != "w1" || len(w.Tabs) != 1 || len(w.Tabs[0].Panes) != 2 {
		t.Fatalf("tree shape: %+v", w)
	}
	p1 := w.Tabs[0].Panes[0]
	if p1.ID != "p1" || p1.AgentRef != "codex-1" || p1.Cwd != "/x" {
		t.Fatalf("p1: %+v", p1)
	}
	p2 := w.Tabs[0].Panes[1]
	if p2.Cwd != "/z" { // 被 set-pane-cwd 改过
		t.Fatalf("p2 cwd: %+v", p2)
	}
	if resp.State.Focus.PaneID != "p1" {
		t.Fatalf("focus: %+v", resp.State.Focus)
	}
}

func TestHandleCloseMethods(t *testing.T) {
	h := newTestHandler(t)
	do(t, h, `{"method":"add-workspace","id":"w1"}`)
	do(t, h, `{"method":"new-tab","workspace":"w1","id":"t1"}`)
	do(t, h, `{"method":"new-pane","workspace":"w1","tab":"t1","id":"p1"}`)
	// close-pane / close-tab / remove-workspace，靠 snapshot 验证。
	if resp := do(t, h, `{"method":"close-pane","workspace":"w1","tab":"t1","pane":"p1"}`); !resp.OK {
		t.Fatalf("close-pane: %+v", resp)
	}
	resp := do(t, h, `{"method":"snapshot"}`)
	if len(resp.State.Workspaces[0].Tabs[0].Panes) != 0 {
		t.Fatalf("pane not closed: %+v", resp.State)
	}
	do(t, h, `{"method":"close-tab","workspace":"w1","tab":"t1"}`)
	do(t, h, `{"method":"remove-workspace","workspace":"w1"}`)
	resp = do(t, h, `{"method":"snapshot"}`)
	if len(resp.State.Workspaces) != 0 {
		t.Fatalf("workspace not removed: %+v", resp.State)
	}
}

func TestHandleUnknownMethod(t *testing.T) {
	h := newTestHandler(t)
	resp := do(t, h, `{"method":"frobnicate"}`)
	if resp.OK {
		t.Fatal("expected OK=false for unknown method")
	}
	if !strings.Contains(resp.Error, "unknown method") {
		t.Fatalf("error: %q", resp.Error)
	}
}

func TestHandleInvalidJSON(t *testing.T) {
	h := newTestHandler(t)
	resp := do(t, h, `not json`)
	if resp.OK {
		t.Fatal("expected OK=false for invalid json")
	}
	if !strings.Contains(resp.Error, "invalid request") {
		t.Fatalf("error: %q", resp.Error)
	}
}

func TestHandleSnapshotContextCancel(t *testing.T) {
	h := newTestHandler(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out, err := h.Handle(ctx, []byte(`{"method":"snapshot"}`))
	if err != nil {
		t.Fatalf("Handle err (should encode error response, not return err): %v", err)
	}
	var resp Response
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.OK {
		t.Fatal("expected OK=false on canceled ctx")
	}
}

func TestHandleBadMethodType(t *testing.T) {
	h := newTestHandler(t)
	// method 字段是数字，json 解码到 string 失败 → invalid request（对抗性：非预期类型）。
	resp := do(t, h, `{"method":123}`)
	if resp.OK {
		t.Fatal("expected OK=false for non-string method")
	}
	if !strings.Contains(resp.Error, "invalid request") {
		t.Fatalf("error: %q", resp.Error)
	}
}

func TestHandleNilEmptyPayload(t *testing.T) {
	h := newTestHandler(t)
	for _, payload := range [][]byte{nil, {}} {
		out, err := h.Handle(context.Background(), payload)
		if err != nil {
			t.Fatalf("Handle(%v): %v", payload, err)
		}
		var resp Response
		if err := json.Unmarshal(out, &resp); err != nil {
			t.Fatalf("unmarshal for %v: %v", payload, err)
		}
		if resp.OK {
			t.Fatalf("expected OK=false for payload %v", payload)
		}
	}
}

func TestHandleConcurrent(t *testing.T) {
	h := newTestHandler(t)
	do(t, h, `{"method":"add-workspace","id":"w"}`)
	do(t, h, `{"method":"new-tab","workspace":"w","id":"t"}`)

	// N goroutine 并发 new-pane + snapshot，验证 Handle/Core 分发路径无竞态、无丢失。
	const N = 30
	var wg sync.WaitGroup
	errc := make(chan error, N)
	for i := 0; i < N; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := fmt.Sprintf(`{"method":"new-pane","workspace":"w","tab":"t","id":"p%d"}`, i)
			if _, err := h.Handle(context.Background(), []byte(req)); err != nil {
				errc <- err
			}
		}()
	}
	wg.Wait()
	close(errc)
	for err := range errc {
		t.Fatalf("concurrent Handle: %v", err)
	}
	resp := do(t, h, `{"method":"snapshot"}`)
	if got := len(resp.State.Workspaces[0].Tabs[0].Panes); got != N {
		t.Fatalf("pane count: got %d, want %d", got, N)
	}
}
