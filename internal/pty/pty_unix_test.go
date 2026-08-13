//go:build !windows

package pty

import (
	"bytes"
	"io"
	"os"
	"testing"
	"time"
)

// readWithTimeout 读 PTY 直到 EOF 或超时，返回累计字节。
// 用于防止 sh/cat 类不立即退出的命令卡死测试；超时返回已读到的部分。
// 超时后 goroutine 仍可能阻塞在 Read，由 defer Close 解除。
func readWithTimeout(t *testing.T, p PTY, timeout time.Duration) []byte {
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

// closePTY 关闭 PTY 并忽略错误（测试清理用，绕过 errcheck 对 defer p.Close() 的检查）。
func closePTY(t *testing.T, p PTY) {
	t.Helper()
	_ = p.Close()
}

func TestStartAndRead(t *testing.T) {
	// echo 一次性命令：进程退出后 master EOF，读到 "hello"。
	p, err := Start(StartConfig{Command: "echo", Args: []string{"hello"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer closePTY(t, p)
	out := readWithTimeout(t, p, 2*time.Second)
	if !bytes.Contains(out, []byte("hello")) {
		t.Fatalf("output %q does not contain 'hello'", out)
	}
}

func TestWriteReadback(t *testing.T) {
	// sh 读一行后回显 MARKER:行内容，验证 Write 注入生效。
	p, err := Start(StartConfig{Command: "sh", Args: []string{"-c", "read x; echo MARKER:$x"}})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer closePTY(t, p)
	if _, err := p.Write([]byte("ping\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out := readWithTimeout(t, p, 2*time.Second)
	if !bytes.Contains(out, []byte("MARKER:ping")) {
		t.Fatalf("output %q does not contain MARKER:ping", out)
	}
}

func TestStartWithCwd(t *testing.T) {
	dir := t.TempDir()
	p, err := Start(StartConfig{Command: "pwd", Cwd: dir})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer closePTY(t, p)
	out := readWithTimeout(t, p, 2*time.Second)
	if !bytes.Contains(out, []byte(dir)) {
		t.Fatalf("output %q does not contain cwd %q", out, dir)
	}
}

func TestStartWithEnv(t *testing.T) {
	// 设 Env 后子进程只继承 Env 列出的变量，需带上 PATH 以便找到 sh（Command 解析用父 PATH，
	// 但 sh 内部若 fork 其他命令需子 PATH）。
	p, err := Start(StartConfig{
		Command: "sh",
		Args:    []string{"-c", "echo $VAGABOND_TEST_VAR"},
		Env:     []string{"PATH=" + os.Getenv("PATH"), "VAGABOND_TEST_VAR=hello-env"},
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer closePTY(t, p)
	out := readWithTimeout(t, p, 2*time.Second)
	if !bytes.Contains(out, []byte("hello-env")) {
		t.Fatalf("output %q does not contain env value", out)
	}
}

func TestStartWithInitialSize(t *testing.T) {
	// 初始尺寸 Rows/Cols 非零时应在 Start 内 Resize；用 stty 查询验证。
	p, err := Start(StartConfig{Command: "sh", Args: []string{"-c", "stty size"}, Rows: 30, Cols: 100})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer closePTY(t, p)
	out := readWithTimeout(t, p, 2*time.Second)
	if !bytes.Contains(out, []byte("30 100")) {
		t.Fatalf("output %q does not show size 30 100", out)
	}
}

func TestResize(t *testing.T) {
	p, err := Start(StartConfig{Command: "sh"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer closePTY(t, p)
	if err := p.Resize(50, 132); err != nil {
		t.Fatalf("Resize: %v", err)
	}
}

func TestClose(t *testing.T) {
	p, err := Start(StartConfig{Command: "cat"}) // cat 不退出，靠 Close 杀
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Close 后 Read 应失败（master 已关）。
	if _, err := p.Read(make([]byte, 1)); err == nil {
		t.Fatal("Read after Close should fail")
	}
}

func TestCloseIdempotent(t *testing.T) {
	p, err := Start(StartConfig{Command: "cat"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestStartMissingCommand(t *testing.T) {
	// 命令不存在：Start 应返回 error（对抗性/异常输入 + 错误传播）。
	if _, err := Start(StartConfig{Command: "/nonexistent/vagabond-test-binary-xyz"}); err == nil {
		t.Fatal("expected error for missing command")
	}
}

func TestConcurrentReadWrite(t *testing.T) {
	// 接口约定：Read 与 Write 可由不同 goroutine 并发（-race 验证无竞态）。
	p, err := Start(StartConfig{Command: "cat"})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		buf := make([]byte, 256)
		for {
			if _, err := p.Read(buf); err != nil {
				return // Close 后 master 关，Read 退出
			}
		}
	}()

	// 并发写若干次；cat 在 PTY 里回显，Read goroutine 消费。
	for i := 0; i < 5; i++ {
		if _, err := p.Write([]byte("hello\n")); err != nil {
			t.Errorf("Write %d: %v", i, err)
		}
	}

	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	<-readDone
}
