// Package agent 实现 Agent 适配器：封装 pty 启动/停止/输入/状态，是异构 agent
// （codex/claude-code/kimi-code/reasonix 等）的统一入口。
//
// 设计取向（AGENTS.md Project Map：agent 有 Start/Stop，叶子）：agent 适配器封装
// PTY（agent.Start 内部调 pty.Start，agent 持有 pty.PTY）；wrapper 经 agent 管理终端面
// （读字节流/注入/VT 状态感知）。依赖分层：wrapper → agent → pty。
//
// 异构 agent 通过 pty.StartConfig.Command 区分（codex/claude...），不为每种写独立类。
//
// 本批为骨架：Status 只有进程级 Idle/Running/Exited；Rule 3 的真实
// idle/working/blocked 依赖 VT 翻译器（wrapper/discovery 批次）；Rule 6 的阻塞 Wait
// 原语留后续（需 pty 退出信号扩展）。
package agent

import (
	"errors"
	"sync"

	"github.com/su1feng/Vagabond/internal/pty"
)

// ErrNotStarted 在 agent 未启动时操作（如 SendInput）返回。
var ErrNotStarted = errors.New("agent: not started")

// Status 是 agent 状态。
//
// 骨架期只有进程级三态。Rule 3 的真实 idle/working/blocked 由 VT 翻译器感知
// （wrapper/discovery 批次），本批不实现。
type Status int

const (
	StatusIdle    Status = iota // 未启动
	StatusRunning               // 已启动（骨架；真实 working/idle/blocked 留 VT）
	StatusExited                // 已停止
)

// Agent 是单个 agent 进程的适配器，封装 PTY 生命周期。
//
// 并发约定：方法可被多 goroutine 调用（内部加锁）；Start/Stop 幂等。
type Agent interface {
	// Start 启动 agent 子进程（内部 pty.Start）。幂等：已启动/已退出时 no-op。
	Start() error
	// Stop 停止 agent（pty.Close）。幂等。
	Stop() error
	// Status 返回当前状态（骨架占位）。
	Status() Status
	// SendInput 写输入到 agent（pty.Write）。未启动返回 ErrNotStarted。
	SendInput(p []byte) error
	// PTY 返回底层 PTY（wrapper 读字节流/resize）。Start 前为 nil。
	PTY() pty.PTY
}

// agent 是 Agent 的通用实现，跑任意 pty.StartConfig.Command。
type agent struct {
	mu     sync.Mutex
	cfg    pty.StartConfig
	pty    pty.PTY
	status Status
}

// New 创建 agent（存配置不启动）。Start 才真正启动进程。
func New(cfg pty.StartConfig) Agent {
	return &agent{cfg: cfg, status: StatusIdle}
}

func (a *agent) Start() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.status != StatusIdle {
		return nil // 幂等：已启动或已退出
	}
	p, err := pty.Start(a.cfg)
	if err != nil {
		return err
	}
	a.pty = p
	a.status = StatusRunning
	return nil
}

func (a *agent) Stop() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.status == StatusExited {
		return nil // 幂等
	}
	if a.pty != nil {
		_ = a.pty.Close()
	}
	a.status = StatusExited
	return nil
}

func (a *agent) Status() Status {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.status
}

// SendInput 锁内取 PTY、锁外 Write，避免持锁做可能阻塞的 I/O。
func (a *agent) SendInput(p []byte) error {
	a.mu.Lock()
	pt := a.pty
	a.mu.Unlock()
	if pt == nil {
		return ErrNotStarted
	}
	_, err := pt.Write(p)
	return err
}

// PTY 返回底层 PTY 的句柄（调用方在锁外使用，Read/Write/Resize 由 os.File 保证并发安全）。
func (a *agent) PTY() pty.PTY {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.pty
}
