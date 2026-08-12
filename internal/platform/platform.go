// Package platform 收敛平台能力抽象（Project Map：平台差异只收敛于此）。
// 当前提供运行时目录与 socket 路径，只实现 unix；Windows 未来按 build tag 添加。
package platform

import (
	"errors"
	"os"
	"path/filepath"
)

var (
	// ErrPathTooLong 在 socket 路径超过平台 Unix 域地址上限时返回。
	ErrPathTooLong = errors.New("platform: socket path too long for platform")
	// ErrInvalidName 在 name 不是合法扁平文件名（为空、含路径分隔符、. 或 ..）时返回，
	// 防止路径穿越（如 "../evil"）逃出 RuntimeDir。
	ErrInvalidName = errors.New("platform: socket name must be a plain file name")
)

// SocketPath 返回 name 的 Unix 域 socket 路径（位于 RuntimeDir 下）。
// name 必须是扁平文件名（如 "api.sock"）；为空、含分隔符或为 . / .. 返回 ErrInvalidName，
// 防止路径穿越逃出 RuntimeDir。路径超长返回 ErrPathTooLong。
func SocketPath(name string) (string, error) {
	if name == "" || filepath.Base(name) != name || name == "." || name == ".." {
		return "", ErrInvalidName
	}
	dir, err := RuntimeDir()
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, name)
	if len(p) > maxSocketPathLen {
		return "", ErrPathTooLong
	}
	return p, nil
}

// ensureDir 确保 dir 存在且仅属当前用户（0700）。
// 显式 Chmod 是因为 MkdirAll 不收紧已有目录的权限。
//
//nolint:gosec // dir 来自受控的 RuntimeDir（非外部输入）；0700 对目录正确（需执行位才能进入）。
func ensureDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.Chmod(dir, 0o700)
}
