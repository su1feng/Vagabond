// Package daemon 实现 Vagabond 的传输宿主：双 unix socket 监听 + 客户端连接管理。
//
// daemon 拥有两个 socket（Rule 4）：api.sock（JSON API，agent/CLI 用）与
// client.sock（二进制字节流，UI 用）。握手后按 socket 类型分发：api socket 的
// payload 交给注入的 APIHandler（internal/api，应用层）；client socket 的字节流
// 暂走骨架（pty 批次接入）。daemon 自身不解析 payload（Project Map）。
//
// Rule 1：daemon 不持有业务状态（AppState 属于 internal/state），api 请求经
// APIHandler → state.Core.Send/Snapshot，连接 worker 绝不直接改 state。
// Rule 8：daemon 不做路由或协调决策。Rule 7：每连接一个 goroutine，客户端断开
// 只清自己的资源，不影响 daemon 或其他连接。
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

// APIHandler 处理 api socket 的 payload（JSON 请求）并返回响应 payload。
// daemon 不解析 payload（Project Map：payload 语义交给 internal/api）。
// 注入后（WithAPI）api socket 走请求-响应循环；未注入则与 client socket 一样读帧丢弃。
type APIHandler interface {
	Handle(ctx context.Context, payload []byte) (response []byte, err error)
}

// connKind 标识连接来自哪个 socket，决定握手后的分发路径。
type connKind int

const (
	kindAPI    connKind = iota // api.sock：JSON 请求-响应
	kindClient                 // client.sock：二进制字节流（pty 批次接入）
)

// Daemon 是 Vagabond 的传输宿主，持有双 socket listener。
// 零值不可用，必须通过 Listen 创建。
type Daemon struct {
	apiListener    net.Listener
	clientListener net.Listener
	apiPath        string
	clientPath     string
	logger         *slog.Logger
	api            APIHandler // 注入后 api socket 走请求-响应；nil 则读帧丢弃

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

// WithAPI 注入 api socket 的处理器；应在 Serve 前调用。
func (d *Daemon) WithAPI(h APIHandler) *Daemon {
	d.api = h
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

	accept := func(lis net.Listener, kind connKind) {
		defer wg.Done()
		for {
			conn, err := lis.Accept()
			if err != nil {
				return // listener 关闭（Close 或 ctx 取消触发）
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				d.handleConn(ctx, conn, kind)
			}()
		}
	}

	wg.Add(2)
	go accept(d.apiListener, kindAPI)
	go accept(d.clientListener, kindClient)

	<-ctx.Done()
	_ = d.Close() // 关闭 listener → accept 循环退出；错误无意义（关停路径）
	wg.Wait()
	return nil
}

// handleConn 在单个连接上执行握手 + 按 socket 类型分发。
// 内置 ctx watcher：ctx 取消时关连接，让阻塞的读写报错退出；
// 连接正常结束时 watcher 经 done 信号无泄漏退出。
// 所有错误在内部消化，绝不向上传播（Rule 7：客户端断开只清自己）。
func (d *Daemon) handleConn(ctx context.Context, conn net.Conn, kind connKind) {
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
	if kind == kindAPI && d.api != nil {
		d.serveAPI(ctx, conn)
	} else {
		d.servePassthrough(conn)
	}
}

// serveAPI 跑 api socket 的请求-响应循环：读帧 → APIHandler.Handle → 回响应帧。
// handler 返回 err（严重错误，如响应序列化失败）或读写失败时关连接。
func (d *Daemon) serveAPI(ctx context.Context, conn net.Conn) {
	for {
		payload, err := protocol.ReadFrame(conn)
		if err != nil {
			return // 对端关闭 / 出错 / 超长帧
		}
		resp, err := d.api.Handle(ctx, payload)
		if err != nil {
			return // 严重错误，关连接
		}
		if err := protocol.WriteFrame(conn, resp); err != nil {
			return
		}
	}
}

// servePassthrough 读帧丢弃：client socket 字节流骨架（pty 批次接入），
// 以及 api socket 未注入 handler 时的兜底。阻塞读受 handleConn 的 ctx watcher 保护。
func (d *Daemon) servePassthrough(conn net.Conn) {
	for {
		if _, err := protocol.ReadFrame(conn); err != nil {
			return
		}
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
