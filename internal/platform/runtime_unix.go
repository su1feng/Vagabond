//go:build !windows

package platform

import (
	"os"
	"path/filepath"
)

// maxSocketPathLen 是 unix 域 socket 路径上限：Linux 107、macOS 103，取小值兼容。
const maxSocketPathLen = 103

// runtimeDirEnv 是覆盖运行时目录的环境变量（测试/多实例/自定义部署）。
const runtimeDirEnv = "VAGABOND_RUNTIME_DIR"

// RuntimeDir 返回 Vagabond 的运行时目录（存放 socket 等），不存在则创建（0700）。
// 优先级：
//  1. $VAGABOND_RUNTIME_DIR（显式覆盖）
//  2. $XDG_RUNTIME_DIR/vagabond（systemd tmpfs，0700，注销自动清）
//  3. ~/.local/state/vagabond（任何场景都有）
//
// 不用 /run（需 root）和 /tmp（可能被他人读、被清理工具删）。详见 ADR 0002。
func RuntimeDir() (string, error) {
	if dir := os.Getenv(runtimeDirEnv); dir != "" {
		return dir, ensureDir(dir)
	}
	if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
		dir := filepath.Join(xdg, "vagabond")
		return dir, ensureDir(dir)
	}
	// 手动解析而非 os.UserStateDir：该符号在目标 toolchain 缺失。
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		state = filepath.Join(home, ".local", "state")
	}
	dir := filepath.Join(state, "vagabond")
	return dir, ensureDir(dir)
}
