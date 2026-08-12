package main

import (
	"context"
	"fmt"
	"net"

	"github.com/su1feng/Vagabond/internal/api"
	"github.com/su1feng/Vagabond/internal/daemon"
	"github.com/su1feng/Vagabond/internal/platform"
	"github.com/su1feng/Vagabond/internal/protocol"
	"github.com/su1feng/Vagabond/internal/state"
)

// runServe 起 daemon（前台）：探测已有 daemon → Listen + 注入 api → Serve。
// ctx 取消（SIGINT/SIGTERM）时 Serve 优雅关停（Rule 7）。
func runServe(ctx context.Context) error {
	running, err := probeDaemon()
	if err != nil {
		return err
	}
	if running {
		return fmt.Errorf("daemon already running")
	}
	core := state.New()
	defer core.Stop()
	d, err := daemon.Listen()
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	d.WithAPI(api.New(core))
	return d.Serve(ctx)
}

// probeDaemon 探测 api socket 上是否已有 daemon 在跑，防止 runServe 抢占已运行
// daemon 的 socket 路径（daemon.listenUnix 会无条件 unlink 残留文件）。
// 返回值：
//   - (false, nil)：连不上 = 无 listener（残留 socket 或未启动），可安全 Listen
//   - (true,  nil)：连上且握手成功 = 活 daemon 已在跑
//   - (true,  err)：连上但握手失败 = 活 daemon 协议版本不符（不应抢占）
func probeDaemon() (bool, error) {
	path, err := platform.SocketPath(apiSocketFile)
	if err != nil {
		return false, fmt.Errorf("resolve socket path: %w", err)
	}
	conn, dialErr := net.Dial("unix", path)
	if dialErr != nil {
		return false, nil // 连不上 = 无 listener
	}
	defer conn.Close()
	if negErr := protocol.NegotiateClient(conn, protocol.ProtocolVersion); negErr != nil {
		return true, fmt.Errorf("daemon running with incompatible protocol: %w", negErr)
	}
	return true, nil
}
