//go:build !windows

package pty

import (
	"os"
	"os/exec"
	"sync"

	creackpty "github.com/creack/pty"
)

// start 用 creack/pty 启动命令并附 PTY（unix 实现）。
func start(cfg StartConfig) (PTY, error) {
	cmd := exec.Command(cfg.Command, cfg.Args...) //nolint:gosec // PTY 设计就是启动调用方提供的命令（agent 子进程），命令来源受控
	if cfg.Cwd != "" {
		cmd.Dir = cfg.Cwd
	}
	if cfg.Env != nil {
		cmd.Env = cfg.Env
	}
	master, err := creackpty.Start(cmd)
	if err != nil {
		return nil, err
	}
	p := &unixPTY{master: master, cmd: cmd}
	if cfg.Rows != 0 && cfg.Cols != 0 {
		// 初始尺寸失败不致命（用 PTY 默认尺寸），忽略错误。
		_ = p.Resize(cfg.Rows, cfg.Cols)
	}
	return p, nil
}

// unixPTY 是 unix 平台的 PTY 实现，封装 creack/pty 的 master (*os.File) 与子进程。
type unixPTY struct {
	master    *os.File
	cmd       *exec.Cmd
	closeOnce sync.Once
	closeErr  error
}

func (p *unixPTY) Read(b []byte) (int, error)  { return p.master.Read(b) }
func (p *unixPTY) Write(b []byte) (int, error) { return p.master.Write(b) }

func (p *unixPTY) Resize(rows, cols uint16) error {
	return creackpty.Setsize(p.master, &creackpty.Winsize{Rows: rows, Cols: cols})
}

// Close 关 master → 杀子进程 → wait 回收，幂等（sync.Once）。
// 顺序很重要：先关 master 让阻塞的 Read 解除，再杀进程，最后 wait 防僵尸。
func (p *unixPTY) Close() error {
	p.closeOnce.Do(func() {
		p.closeErr = p.master.Close()
		if p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
		}
		_ = p.cmd.Wait() // 回收僵尸；进程被 kill 后返回非 nil，忽略
	})
	return p.closeErr
}
