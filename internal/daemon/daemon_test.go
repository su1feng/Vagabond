package daemon

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/su1feng/Vagabond/internal/api"
	"github.com/su1feng/Vagabond/internal/platform"
	"github.com/su1feng/Vagabond/internal/protocol"
	"github.com/su1feng/Vagabond/internal/state"
)

// startDaemon 在临时运行时目录监听并后台 Serve；cancel 或测试结束自动关停。
// 返回 Serve 退出信号 serveDone 与取消函数。
func startDaemon(t *testing.T) (d *Daemon, serveDone <-chan struct{}, cancel context.CancelFunc) {
	t.Helper()
	t.Setenv("VAGABOND_RUNTIME_DIR", t.TempDir())
	d, err := Listen()
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	ctx, cancelFn := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = d.Serve(ctx); close(done) }()
	t.Cleanup(func() {
		cancelFn()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("Serve did not exit within 3s after cancel")
		}
		_ = d.Close()
	})
	return d, done, cancelFn
}

// socketPath 返回临时运行时目录下 name 的 socket 路径。
func socketPath(t *testing.T, name string) string {
	t.Helper()
	p, err := platform.SocketPath(name)
	if err != nil {
		t.Fatalf("SocketPath(%q): %v", name, err)
	}
	return p
}

// dialHandshake 连上指定 socket 并完成客户端握手，返回已握手的连接。
// 握手失败即 t.Fatalf；版本不匹配等"预期失败"场景请直接用 net.Dial + NegotiateClient。
func dialHandshake(t *testing.T, name string, version uint32) net.Conn {
	t.Helper()
	conn, err := net.Dial("unix", socketPath(t, name))
	if err != nil {
		t.Fatalf("Dial %q: %v", name, err)
	}
	if err := protocol.NegotiateClient(conn, version); err != nil {
		_ = conn.Close()
		t.Fatalf("NegotiateClient(%q): %v", name, err)
	}
	return conn
}

func TestListenAndServeHandshake(t *testing.T) {
	startDaemon(t)
	// api 与 client 两个 socket 都能连上并完成握手。
	for _, name := range []string{apiSocketName, clientSocketName} {
		conn := dialHandshake(t, name, protocol.ProtocolVersion)
		defer conn.Close()
	}
}

func TestHandshakeVersionMismatch(t *testing.T) {
	startDaemon(t)
	// 双向：客户端版本比服务端旧（v-1）或新（v+1）都应被拒绝。
	for _, clientV := range []uint32{protocol.ProtocolVersion + 1, protocol.ProtocolVersion - 1} {
		conn, err := net.Dial("unix", socketPath(t, apiSocketName))
		if err != nil {
			t.Fatalf("Dial: %v", err)
		}
		err = protocol.NegotiateClient(conn, clientV)
		if !errors.Is(err, protocol.ErrVersionMismatch) {
			_ = conn.Close()
			t.Fatalf("NegotiateClient(v=%d): got %v, want ErrVersionMismatch", clientV, err)
		}
		_ = conn.Close()
	}
}

func TestMalformedHandshake(t *testing.T) {
	startDaemon(t)
	conn, err := net.Dial("unix", socketPath(t, apiSocketName))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()
	// 发畸形首帧（非合法 JSON）：server NegotiateServer 应 decode 失败并关连接。
	if err := protocol.WriteFrame(conn, []byte("not json")); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}
	// server 关连接后，客户端后续读应报错。
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := protocol.ReadFrame(conn); err == nil {
		t.Fatal("ReadFrame: want error after malformed handshake, got nil")
	}
	// daemon 仍存活：新连接能正常握手（错误被隔离）。
	c2 := dialHandshake(t, apiSocketName, protocol.ProtocolVersion)
	defer c2.Close()
}

func TestWorkerErrorDoesNotAffectDaemon(t *testing.T) {
	startDaemon(t)
	// 握手成功后，客户端发一个超长帧声明（对抗性）：server 的 ReadFrame 返回
	// ErrFrameTooLarge，worker 应干净退出并关连接。
	c1 := dialHandshake(t, apiSocketName, protocol.ProtocolVersion)
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(protocol.MaxFrameSize+1))
	if _, err := c1.Write(hdr[:]); err != nil {
		t.Fatalf("write oversized frame header: %v", err)
	}
	_ = c1.Close()
	time.Sleep(50 * time.Millisecond)
	// 新客户端仍能握手——证明 worker 的协议级错误被隔离，不影响 daemon（Rule 7）。
	c2 := dialHandshake(t, apiSocketName, protocol.ProtocolVersion)
	defer c2.Close()
}

func TestClientDisconnectDoesNotAffectDaemon(t *testing.T) {
	startDaemon(t)
	// 第一个客户端握手后立即断开。
	c1 := dialHandshake(t, apiSocketName, protocol.ProtocolVersion)
	_ = c1.Close()
	// 给 server worker 一点时间处理断开（worker 退出不应阻塞 accept）。
	time.Sleep(50 * time.Millisecond)
	// 第二个客户端仍能正常握手——证明断开被隔离（Rule 7）。
	c2 := dialHandshake(t, apiSocketName, protocol.ProtocolVersion)
	defer c2.Close()
}

func TestGracefulShutdown(t *testing.T) {
	t.Setenv("VAGABOND_RUNTIME_DIR", t.TempDir())
	d, err := Listen()
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = d.Serve(ctx); close(done) }()

	// 连一个客户端并握手。
	conn := dialHandshake(t, apiSocketName, protocol.ProtocolVersion)

	cancel()
	select {
	case <-done:
		// Serve 在 cancel 后返回。
	case <-time.After(3 * time.Second):
		t.Fatal("Serve did not return within 3s after cancel")
	}

	// 客户端连接应被关闭：读应返回错误（EOF / 重置 / 超时均算）。
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := protocol.ReadFrame(conn); err == nil {
		t.Fatal("ReadFrame: want error after shutdown, got nil")
	}
	_ = conn.Close()
	_ = d.Close()
}

func TestConcurrentConnections(t *testing.T) {
	startDaemon(t)
	// 预先算好路径，避免在子 goroutine 里调用 t 方法（t.Fatalf 仅限测试 goroutine）。
	apiP := socketPath(t, apiSocketName)
	clientP := socketPath(t, clientSocketName)

	const N = 16
	var wg sync.WaitGroup
	errc := make(chan error, N)
	for i := 0; i < N; i++ {
		p := apiP
		if i%2 == 0 {
			p = clientP
		}
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			conn, err := net.Dial("unix", p)
			if err != nil {
				errc <- err
				return
			}
			defer conn.Close()
			if err := protocol.NegotiateClient(conn, protocol.ProtocolVersion); err != nil {
				errc <- err
				return
			}
		}(p)
	}
	wg.Wait()
	close(errc)
	for err := range errc {
		t.Fatalf("concurrent connection: %v", err)
	}
}

func TestSocketFileCleanup(t *testing.T) {
	t.Setenv("VAGABOND_RUNTIME_DIR", t.TempDir())
	d, err := Listen()
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	api, client := d.SocketPaths()
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	for _, p := range []string{api, client} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("socket %q still exists after Close: %v", p, err)
		}
	}
}

func TestStaleSocketUnlinked(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VAGABOND_RUNTIME_DIR", dir)
	// 预创建残留文件，模拟上次未正常退出。
	for _, name := range []string{apiSocketName, clientSocketName} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("stale"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
	}
	d, err := Listen()
	if err != nil {
		t.Fatalf("Listen with stale sockets: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = d.Serve(ctx); close(done) }()
	t.Cleanup(func() {
		cancel()
		<-done
		_ = d.Close()
	})

	// 残留文件被 unlink 后能正常监听 + 连接。
	c := dialHandshake(t, apiSocketName, protocol.ProtocolVersion)
	defer c.Close()
}

func TestNoGoroutineLeak(t *testing.T) {
	before := runtime.NumGoroutine()
	_, done, cancel := startDaemon(t)

	// 连若干客户端并断开，触发 worker 创建与退出。
	for i := 0; i < 8; i++ {
		c := dialHandshake(t, apiSocketName, protocol.ProtocolVersion)
		_ = c.Close()
	}

	cancel()
	<-done
	// 等 worker 与 watcher goroutine 回落。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= before+2 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("goroutine leak: before=%d now=%d", before, runtime.NumGoroutine())
}

func TestDoubleCloseIdempotent(t *testing.T) {
	t.Setenv("VAGABOND_RUNTIME_DIR", t.TempDir())
	d, err := Listen()
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// startDaemonWithAPI 启动注入了 api.Handler（绑定新 Core）的 daemon，供端到端测试。
func startDaemonWithAPI(t *testing.T) (*Daemon, *state.Core) {
	t.Helper()
	t.Setenv("VAGABOND_RUNTIME_DIR", t.TempDir())
	core := state.New()
	t.Cleanup(core.Stop)
	d, err := Listen()
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	d.WithAPI(api.New(core))
	ctx, cancelFn := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = d.Serve(ctx); close(done) }()
	t.Cleanup(func() {
		cancelFn()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("Serve did not exit within 3s after cancel")
		}
		_ = d.Close()
	})
	return d, core
}

// dialAPI 连上 api socket 并完成握手。
func dialAPI(t *testing.T) net.Conn {
	t.Helper()
	conn, err := net.Dial("unix", socketPath(t, apiSocketName))
	if err != nil {
		t.Fatalf("Dial api.sock: %v", err)
	}
	if err := protocol.NegotiateClient(conn, protocol.ProtocolVersion); err != nil {
		_ = conn.Close()
		t.Fatalf("NegotiateClient: %v", err)
	}
	return conn
}

// apiRequest 在已握手的连接上发 JSON 请求并读回响应（端到端测试辅助）。
func apiRequest(t *testing.T, conn net.Conn, req string) api.Response {
	t.Helper()
	if err := protocol.WriteFrame(conn, []byte(req)); err != nil {
		t.Fatalf("write %q: %v", req, err)
	}
	payload, err := protocol.ReadFrame(conn)
	if err != nil {
		t.Fatalf("read response for %q: %v", req, err)
	}
	var resp api.Response
	if err := json.Unmarshal(payload, &resp); err != nil {
		t.Fatalf("unmarshal response for %q: %v (raw=%s)", req, err, payload)
	}
	return resp
}

func TestAPISocketSnapshotEndToEnd(t *testing.T) {
	// 端到端 Rule 1：客户端 → daemon api socket → api.Handler → Core.Snapshot → 响应。
	_, core := startDaemonWithAPI(t)
	core.Send(state.AddWorkspace{ID: "w1"})
	core.Send(state.NewTab{WorkspaceID: "w1", ID: "t1"})
	core.Send(state.NewPane{WorkspaceID: "w1", TabID: "t1", ID: "p1", Cwd: "/demo"})

	conn := dialAPI(t)
	defer conn.Close()

	resp := apiRequest(t, conn, `{"method":"snapshot"}`)
	if !resp.OK || resp.State == nil {
		t.Fatalf("resp: %+v", resp)
	}
	if len(resp.State.Workspaces) != 1 || resp.State.Workspaces[0].Tabs[0].Panes[0].ID != "p1" {
		t.Fatalf("state mismatch: %+v", resp.State)
	}
}

func TestAPISocketWriteEndToEnd(t *testing.T) {
	startDaemonWithAPI(t)
	conn := dialAPI(t)
	defer conn.Close()

	// 客户端发写请求 → 各 OK=true（fire-and-forget 投递成功）。
	for _, r := range []string{
		`{"method":"add-workspace","id":"w1"}`,
		`{"method":"new-tab","workspace":"w1","id":"t1"}`,
		`{"method":"new-pane","workspace":"w1","tab":"t1","id":"p1","cwd":"/x"}`,
		`{"method":"set-pane-agent","workspace":"w1","tab":"t1","pane":"p1","agent":"codex-1"}`,
	} {
		if resp := apiRequest(t, conn, r); !resp.OK {
			t.Fatalf("write %q: %+v", r, resp)
		}
	}
	// snapshot 验证端到端写生效。
	resp := apiRequest(t, conn, `{"method":"snapshot"}`)
	p := resp.State.Workspaces[0].Tabs[0].Panes[0]
	if p.ID != "p1" || p.Cwd != "/x" || p.AgentRef != "codex-1" {
		t.Fatalf("pane after writes: %+v", p)
	}
}

func TestAPISocketErrorResponses(t *testing.T) {
	startDaemonWithAPI(t)
	conn := dialAPI(t)
	defer conn.Close()

	// 未知 method → OK=false + error。
	if resp := apiRequest(t, conn, `{"method":"frobnicate"}`); resp.OK {
		t.Fatal("expected OK=false for unknown method")
	}
	// 无效 JSON → OK=false + error。
	if resp := apiRequest(t, conn, `not json`); resp.OK {
		t.Fatal("expected OK=false for invalid json")
	}
	// 连接仍可用：后续 snapshot 正常返回。
	if resp := apiRequest(t, conn, `{"method":"snapshot"}`); !resp.OK {
		t.Fatalf("connection unusable after error responses: %+v", resp)
	}
}

func TestClientSocketIsPassthrough(t *testing.T) {
	// 即使注入了 api，client socket 也走 servePassthrough（不响应 JSON）。
	startDaemonWithAPI(t)
	conn, err := net.Dial("unix", socketPath(t, clientSocketName))
	if err != nil {
		t.Fatalf("Dial client.sock: %v", err)
	}
	defer conn.Close()
	if err := protocol.NegotiateClient(conn, protocol.ProtocolVersion); err != nil {
		t.Fatalf("NegotiateClient: %v", err)
	}
	// 发一个看起来像 api 请求的帧，servePassthrough 应丢弃、不回。
	if err := protocol.WriteFrame(conn, []byte(`{"method":"snapshot"}`)); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if _, err := protocol.ReadFrame(conn); err == nil {
		t.Fatal("client socket should not respond (passthrough), got a response")
	}
}
