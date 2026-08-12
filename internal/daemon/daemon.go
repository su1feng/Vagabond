// Package daemon 实现 Vagabond 的传输宿主：双 unix socket 监听 + 客户端连接管理。
//
// daemon 拥有两个 socket（Rule 4）：api.sock（JSON API，agent/CLI 用）与
// client.sock（二进制字节流，UI 用）。骨架阶段两者一视同仁，都只做 protocol
// 的版本握手 + framing，不解析 payload（payload 语义交给未来上层 internal/api
// 与 render）。
//
// daemon 不持有业务状态（AppState 属于 internal/state，后续批次注入），也不做
// 路由或协调决策（Rule 8）。连接生命周期用 context 管理（Rule 7）：每个连接
// 一个 goroutine，客户端断开只清理自己的资源，不影响 daemon 或其他连接。
//
// 演进路径（不在本骨架实现）：未来 internal/state 接入时，AppState 只被一个核心
// goroutine 持有（Rule 1），届时连接 worker 将从"自处理读帧"改为"通过 channel
// 向核心 goroutine 发送帧/连接事件"，AppState 绝不被 worker 直接修改。本骨架的
// accept + per-conn worker 框架保留，是增量演进而非重写。
package daemon

import (
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"sync"

	"github.com/su1feng/Vagabond/internal/platform"
	"github.com/su1feng/Vagabond/internal/protocol"
)

// socket 文件名（位于 platform.RuntimeDir 下）。
const (
	apiSocketName    = "api.sock"
	clientSocketName = "client.sock"
)

// Daemon 是 Vagabond 的传输宿主，持有双 socket listener。
// 零值不可用，必须通过 Listen 创建。
type Daemon struct {
	apiListener    net.Listener
	clientListener net.Listener
	apiPath        string
	clientPath     string
	logger         *slog.Logger

	closeOnce sync.Once
	closeErr  error
}

// Listen 解析运行时目录并监听 api.sock + client.sock。
// 若 socket 文件残留（上次未正常退出），先 unlink 再 Listen。
func Listen() (*Daemon, error) {
	apiPath, err := platform.SocketPath(apiSocketName)
	if err != nil {
		return nil, err
	}
	clientPath, err := platform.SocketPath(clientSocketName)
	if err != nil {
		return nil, err
	}
	apiLis, err := listenUnix(apiPath)
	if err != nil {
		return nil, err
	}
	clientLis, err := listenUnix(clientPath)
	if err != nil {
		_ = apiLis.Close()
		_ = os.Remove(apiPath)
		return nil, err
	}
	return &Daemon{
		apiListener:    apiLis,
		clientListener: clientLis,
		apiPath:        apiPath,
		clientPath:     clientPath,
		logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	}, nil
}

// WithLogger 注入 logger；传入 nil 保持默认（静默）。应在 Serve 前调用。
func (d *Daemon) WithLogger(l *slog.Logger) *Daemon {
	if l != nil {
		d.logger = l
	}
	return d
}

// SocketPaths 返回 api 与 client socket 的磁盘路径，主要供测试与诊断用。
func (d *Daemon) SocketPaths() (api, client string) {
	return d.apiPath, d.clientPath
}

// Serve 阻塞运行 daemon：accept 两个 listener，每连接 spawn 一个 worker 执行
// 握手 + 读帧循环。ctx 取消时关闭 listener 与所有活跃连接，等所有 worker 退出
// 后返回 nil（预期关停视为正常，非错误）。Serve 返回前会调用 Close。
func (d *Daemon) Serve(ctx context.Context) error {
	var wg sync.WaitGroup

	accept := func(lis net.Listener) {
		defer wg.Done()
		for {
			conn, err := lis.Accept()
			if err != nil {
				return // listener 关闭（Close 或 ctx 取消触发）
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				d.handleConn(ctx, conn)
			}()
		}
	}

	wg.Add(2)
	go accept(d.apiListener)
	go accept(d.clientListener)

	<-ctx.Done()
	_ = d.Close() // 关闭 listener → accept 循环退出；错误无意义（关停路径）
	wg.Wait()
	return nil
}

// handleConn 在单个连接上执行握手 + 读帧循环（骨架丢弃 payload）。
// 内置 ctx watcher：ctx 取消时关连接，让阻塞的 ReadFrame 报错退出；
// 连接正常结束时 watcher 经 done 信号无泄漏退出。
// 所有错误在内部消化，绝不向上传播（Rule 7：客户端断开只清自己）。
func (d *Daemon) handleConn(ctx context.Context, conn net.Conn) {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	defer func() {
		_ = conn.Close()
		close(done)
	}()

	if err := protocol.NegotiateServer(conn, protocol.ProtocolVersion); err != nil {
		d.logger.Debug("daemon: handshake failed", "addr", conn.RemoteAddr(), "err", err)
		return
	}
	for {
		if _, err := protocol.ReadFrame(conn); err != nil {
			return // 对端关闭 / 出错 / 超长帧
		}
		// 骨架阶段丢弃 payload；未来上层按 socket 类型分发（api JSON / client 字节流）。
	}
}

// Close 幂等关闭两个 listener 并清理 socket 文件。
// 可被 Serve（ctx 取消时）或调用方主动调用。
func (d *Daemon) Close() error {
	d.closeOnce.Do(func() {
		if d.apiListener != nil {
			d.closeErr = d.apiListener.Close()
		}
		if d.clientListener != nil {
			if err := d.clientListener.Close(); err != nil && d.closeErr == nil {
				d.closeErr = err
			}
		}
		_ = os.Remove(d.apiPath)
		_ = os.Remove(d.clientPath)
	})
	return d.closeErr
}

// listenUnix 在 path 监听 unix socket；若 path 有残留文件先 unlink（忽略 not-exist）。
func listenUnix(path string) (net.Listener, error) {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return net.Listen("unix", path)
}
