package daemon

import (
	"context"
	"encoding/binary"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/su1feng/Vagabond/internal/platform"
	"github.com/su1feng/Vagabond/internal/protocol"
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
