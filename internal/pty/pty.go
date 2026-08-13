// Package pty 提供跨平台伪终端（PTY）抽象。
//
// 它是 Project Map 里唯一允许依赖平台 PTY 库（unix: creack/pty；windows: ConPTY）
// 的包——其他模块（agent/wrapper/daemon）一律经本包访问 PTY，不得直接依赖。
//
// PTY 只吐/吞原始字节流（Rule 3：daemon 推原始字节流，VT 翻译只用于 agent 状态
// 感知且不在本包）。Read 返回 agent 输出的原始字节（含终端控制序列），Write 注入
// 输入，本包不做 utf8 解码或 VT 翻译。
//
// 当前只实现 unix（creack/pty，build tag）；windows（ConPTY）后续添加。
package pty

// PTY 是伪终端句柄。
//
// 并发约定：Read 与 Write 可由不同 goroutine 并发调用（一个读、一个写）；
// Close 不得与 Read/Write 并发（调用方负责）。
type PTY interface {
	// Read 读 agent 输出的原始字节流（含终端控制序列）。阻塞至有数据或关闭。
	Read(p []byte) (int, error)
	// Write 写输入到 PTY（注入）。返回写入字节数。
	Write(p []byte) (int, error)
	// Resize 调整终端窗口尺寸（行列）。
	Resize(rows, cols uint16) error
	// Close 关闭 PTY、终止子进程并回收。幂等。
	Close() error
}

// StartConfig 封装 PTY 启动参数。
//
// Cwd 与 Env 是 ADR 0005 隔离沙箱的挂载点：未来隔离模式会在此清理
// GIT_DIR/GIT_WORK_TREE 并把 worktree 设为 Cwd。本批不实现沙箱，但接口先留，
// 避免后续改签名。
type StartConfig struct {
	Command string   // 可执行文件路径（如 "/bin/sh" 或 agent 二进制）
	Args    []string // 命令参数
	Cwd     string   // 工作目录（worktree 路径）；空 = 继承
	Env     []string // 环境变量（KEY=VAL）；nil = 继承
	Rows    uint16   // 初始行数；0 = 默认
	Cols    uint16   // 初始列数；0 = 默认
}

// Start 启动命令并附 PTY，返回 PTY 句柄。调用方负责 Close。
//
// 实现按平台分文件（unix: pty_unix.go）。当前未实现 windows（ConPTY）。
func Start(cfg StartConfig) (PTY, error) {
	return start(cfg)
}
