package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/su1feng/Vagabond/internal/state"
)

// waitForDaemon 轮询 dialAPI 直到连上 daemon 或超时（容忍 daemon 启动延迟）。
func waitForDaemon(t *testing.T, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if conn, err := dialAPI(); err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("daemon did not come up within %s", timeout)
}

// startServe 后台启动 runServe，返回取消函数与 Serve 退出信号。
func startServe(t *testing.T) (cancel context.CancelFunc, serveErr <-chan error) {
	t.Helper()
	t.Setenv("VAGABOND_RUNTIME_DIR", t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- runServe(ctx) }()
	return cancel, errc
}

func TestRunServeAndSnapshot(t *testing.T) {
	cancel, serveErr := startServe(t)
	waitForDaemon(t, 3*time.Second)

	// snapshot 应输出合法 state JSON（空 state：{}  或 {"Workspaces":null,...}）。
	var out bytes.Buffer
	if err := runSnapshot(&out); err != nil {
		t.Fatalf("runSnapshot: %v", err)
	}
	var s state.AppState
	if err := json.Unmarshal(out.Bytes(), &s); err != nil {
		t.Fatalf("snapshot output not valid state JSON: %v (out=%s)", err, out.String())
	}

	cancel()
	if err := <-serveErr; err != nil {
		t.Fatalf("runServe returned error after cancel: %v", err)
	}
}

func TestRunServeRejectsExisting(t *testing.T) {
	cancel, serveErr := startServe(t)
	defer cancel()
	waitForDaemon(t, 3*time.Second)

	// 第二次 runServe 应报错（探测到已有 daemon）。
	if err := runServe(context.Background()); err == nil {
		t.Fatal("expected error: daemon already running")
	}

	cancel()
	<-serveErr
}

func TestProbeDaemon(t *testing.T) {
	t.Setenv("VAGABOND_RUNTIME_DIR", t.TempDir())
	// 无 daemon：连不上 → (false, nil)。
	if running, err := probeDaemon(); running || err != nil {
		t.Fatalf("probeDaemon with no daemon: running=%v err=%v", running, err)
	}

	cancel, serveErr := startServe(t)
	waitForDaemon(t, 3*time.Second)

	// 有 daemon：(true, nil)。
	if running, err := probeDaemon(); !running || err != nil {
		t.Fatalf("probeDaemon with daemon running: running=%v err=%v", running, err)
	}

	cancel()
	<-serveErr
}

func TestSnapshotWithoutDaemon(t *testing.T) {
	t.Setenv("VAGABOND_RUNTIME_DIR", t.TempDir())
	// 无 daemon 时 snapshot 应报错（daemon not running）。
	if err := runSnapshot(io.Discard); err == nil {
		t.Fatal("expected error when daemon not running")
	}
}

func TestRunDispatch(t *testing.T) {
	// 无参 → usage，无错误。
	if err := run(nil); err != nil {
		t.Fatalf("run with no args: %v", err)
	}
	// 未知命令 → 错误（对抗性：非预期输入）。
	if err := run([]string{"bogus-command"}); err == nil {
		t.Fatal("expected error for unknown command")
	}
	// version → 无错误。
	if err := run([]string{"version"}); err != nil {
		t.Fatalf("run version: %v", err)
	}
}
