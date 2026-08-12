//go:build !windows

package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeDirEnvOverride(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("VAGABOND_RUNTIME_DIR", tmp)
	t.Setenv("XDG_RUNTIME_DIR", "")

	got, err := RuntimeDir()
	if err != nil {
		t.Fatalf("RuntimeDir: %v", err)
	}
	if got != tmp {
		t.Fatalf("got %q, want %q", got, tmp)
	}
	info, err := os.Stat(got)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("perm %o, want 0700", info.Mode().Perm())
	}
}

func TestRuntimeDirXDG(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("VAGABOND_RUNTIME_DIR", "")
	t.Setenv("XDG_RUNTIME_DIR", xdg)

	got, err := RuntimeDir()
	if err != nil {
		t.Fatalf("RuntimeDir: %v", err)
	}
	if want := filepath.Join(xdg, "vagabond"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRuntimeDirStateFallback(t *testing.T) {
	state := t.TempDir()
	t.Setenv("VAGABOND_RUNTIME_DIR", "")
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("XDG_STATE_HOME", state)

	got, err := RuntimeDir()
	if err != nil {
		t.Fatalf("RuntimeDir: %v", err)
	}
	if want := filepath.Join(state, "vagabond"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRuntimeDirPointsToFile(t *testing.T) {
	// 环境变量指向一个文件（非目录），ensureDir 应失败。
	f, err := os.CreateTemp("", "vagabond-file-*")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	path := f.Name()
	_ = f.Close()
	defer os.Remove(path)

	t.Setenv("VAGABOND_RUNTIME_DIR", path)
	if _, err := RuntimeDir(); err == nil {
		t.Fatal("RuntimeDir: want error when env points to a file, got nil")
	}
}

func TestSocketPathTooLong(t *testing.T) {
	t.Setenv("VAGABOND_RUNTIME_DIR", t.TempDir())
	name := strings.Repeat("a", maxSocketPathLen) // 配合运行时目录前缀必然超长
	_, err := SocketPath(name)
	if err != ErrPathTooLong {
		t.Fatalf("got %v, want ErrPathTooLong", err)
	}
}

func TestSocketPathOK(t *testing.T) {
	t.Setenv("VAGABOND_RUNTIME_DIR", t.TempDir())
	got, err := SocketPath("api.sock")
	if err != nil {
		t.Fatalf("SocketPath: %v", err)
	}
	if !strings.HasSuffix(got, "api.sock") {
		t.Fatalf("got %q, want suffix api.sock", got)
	}
}

func TestSocketPathInvalidNames(t *testing.T) {
	t.Setenv("VAGABOND_RUNTIME_DIR", t.TempDir())
	// 这些 name 应被拒绝（路径穿越 / 空 / 当前目录 / 含分隔符）。
	for _, name := range []string{"", ".", "..", "../evil", "a/b", "/abs/path"} {
		got, err := SocketPath(name)
		if err != ErrInvalidName {
			t.Errorf("SocketPath(%q): got (%q, %v), want ErrInvalidName", name, got, err)
		}
	}
}

func TestSocketPathStaysWithinRuntimeDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VAGABOND_RUNTIME_DIR", dir)
	got, err := SocketPath("api.sock")
	if err != nil {
		t.Fatalf("SocketPath: %v", err)
	}
	// 结果路径必须仍在 RuntimeDir 下（防穿越回归）。
	rel, err := filepath.Rel(dir, got)
	if err != nil {
		t.Fatalf("filepath.Rel: %v", err)
	}
	if strings.HasPrefix(rel, "..") {
		t.Fatalf("SocketPath escaped RuntimeDir: %q (rel %q)", got, rel)
	}
}

func TestSocketPathBoundaryLength(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("VAGABOND_RUNTIME_DIR", dir)
	// 总路径长度 = len(dir) + 1(分隔符) + len(name)。精确控制到上限和超 1。
	overhead := len(dir) + 1
	exactLen := maxSocketPathLen - overhead
	if exactLen < 0 {
		t.Skipf("runtime dir too long (%d) to test boundary", len(dir))
	}

	got, err := SocketPath(strings.Repeat("a", exactLen))
	if err != nil {
		t.Fatalf("exact-boundary SocketPath: %v (got %q)", err, got)
	}
	if len(got) != maxSocketPathLen {
		t.Fatalf("exact-boundary length: got %d, want %d", len(got), maxSocketPathLen)
	}

	if _, err := SocketPath(strings.Repeat("a", exactLen+1)); err != ErrPathTooLong {
		t.Fatalf("over-by-1 SocketPath: got %v, want ErrPathTooLong", err)
	}
}
