//go:build !windows

package agent

import (
	"bytes"
	"errors"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/su1feng/Vagabond/internal/pty"
)

// readWithTimeout 读 PTY 直到 EOF 或超时（防常驻命令卡死测试）。
func readWithTimeout(t *testing.T, p pty.PTY, timeout time.Duration) []byte {
	t.Helper()
	ch := make(chan []byte, 1)
	go func() {
		data, _ := io.ReadAll(p)
		ch <- data
	}()
	select {
	case data := <-ch:
		return data
	case <-time.After(timeout):
		return nil
	}
}

func closeAgent(t *testing.T, a Agent) {
	t.Helper()
	_ = a.Stop()
}

func TestStartStop(t *testing.T) {
	a := New(pty.StartConfig{Command: "sh"})
	if status := a.Status(); status != StatusIdle {
		t.Fatalf("before Start: %v, want Idle", status)
	}
	if err := a.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if status := a.Status(); status != StatusRunning {
		t.Fatalf("after Start: %v, want Running", status)
	}
	if a.PTY() == nil {
		t.Fatal("PTY nil after Start")
	}
	if err := a.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if status := a.Status(); status != StatusExited {
		t.Fatalf("after Stop: %v, want Exited", status)
	}
}

func TestPTYNilBeforeStart(t *testing.T) {
	a := New(pty.StartConfig{Command: "sh"})
	if a.PTY() != nil {
		t.Fatal("PTY should be nil before Start")
	}
}

func TestSendInputBeforeStart(t *testing.T) {
	a := New(pty.StartConfig{Command: "sh"})
	err := a.SendInput([]byte("x"))
	if !errors.Is(err, ErrNotStarted) {
		t.Fatalf("SendInput before Start: got %v, want ErrNotStarted", err)
	}
}

func TestSendInput(t *testing.T) {
	a := New(pty.StartConfig{Command: "sh", Args: []string{"-c", "read x; echo MARKER:$x"}})
	defer closeAgent(t, a)
	if err := a.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := a.SendInput([]byte("ping\n")); err != nil {
		t.Fatalf("SendInput: %v", err)
	}
	out := readWithTimeout(t, a.PTY(), 2*time.Second)
	if !bytes.Contains(out, []byte("MARKER:ping")) {
		t.Fatalf("output %q does not contain MARKER:ping", out)
	}
}

func TestStartMissingCommand(t *testing.T) {
	a := New(pty.StartConfig{Command: "/nonexistent/vagabond-agent-test-xyz"})
	if err := a.Start(); err == nil {
		t.Fatal("expected error for missing command")
	}
	if status := a.Status(); status != StatusIdle {
		t.Fatalf("status after failed Start: %v, want Idle", status)
	}
}

func TestStartIdempotent(t *testing.T) {
	a := New(pty.StartConfig{Command: "cat"})
	defer closeAgent(t, a)
	if err := a.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := a.Start(); err != nil {
		t.Fatalf("second Start: %v", err)
	}
}

func TestStopIdempotent(t *testing.T) {
	a := New(pty.StartConfig{Command: "cat"})
	if err := a.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := a.Stop(); err != nil {
		t.Fatalf("first Stop: %v", err)
	}
	if err := a.Stop(); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
}

func TestStartWithCwd(t *testing.T) {
	dir := t.TempDir()
	a := New(pty.StartConfig{Command: "pwd", Cwd: dir})
	defer closeAgent(t, a)
	if err := a.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	out := readWithTimeout(t, a.PTY(), 2*time.Second)
	if !bytes.Contains(out, []byte(dir)) {
		t.Fatalf("output %q does not contain cwd %q", out, dir)
	}
}

func TestStartWithEnv(t *testing.T) {
	a := New(pty.StartConfig{
		Command: "sh",
		Args:    []string{"-c", "echo $VAGABOND_AGENT_TEST_VAR"},
		Env:     []string{"PATH=" + os.Getenv("PATH"), "VAGABOND_AGENT_TEST_VAR=hello-agent"},
	})
	defer closeAgent(t, a)
	if err := a.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	out := readWithTimeout(t, a.PTY(), 2*time.Second)
	if !bytes.Contains(out, []byte("hello-agent")) {
		t.Fatalf("output %q does not contain env value", out)
	}
}

func TestConcurrentSendInputStatus(t *testing.T) {
	// 并发读（消费 PTY）+ 多 goroutine 写输入/查状态，-race 验证无竞态。
	a := New(pty.StartConfig{Command: "cat"})
	if err := a.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		buf := make([]byte, 256)
		for {
			if _, err := a.PTY().Read(buf); err != nil {
				return // Stop 后 master 关，Read 退出
			}
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = a.SendInput([]byte("hello\n"))
			_ = a.Status()
		}()
	}
	wg.Wait()

	if err := a.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	<-readDone
}
