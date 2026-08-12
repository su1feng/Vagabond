package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"

	"github.com/su1feng/Vagabond/internal/api"
	"github.com/su1feng/Vagabond/internal/platform"
	"github.com/su1feng/Vagabond/internal/protocol"
)

// runSnapshot 连本地 daemon 发 snapshot 请求，把 state JSON pretty-print 到 out。
// 若 daemon 未运行（连不上 api socket）返回错误。
func runSnapshot(out io.Writer) error {
	conn, err := dialAPI()
	if err != nil {
		return fmt.Errorf("daemon not running: %w", err)
	}
	defer conn.Close()

	req, err := json.Marshal(api.Request{Method: "snapshot"})
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}
	if err := protocol.WriteFrame(conn, req); err != nil {
		return fmt.Errorf("write request: %w", err)
	}
	payload, err := protocol.ReadFrame(conn)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	var resp api.Response
	if err := json.Unmarshal(payload, &resp); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if !resp.OK {
		return fmt.Errorf("snapshot failed: %s", resp.Error)
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(resp.State); err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	return nil
}

// dialAPI 连本地 daemon 的 api socket 并完成客户端握手，返回已握手连接。
func dialAPI() (net.Conn, error) {
	path, err := platform.SocketPath(apiSocketFile)
	if err != nil {
		return nil, err
	}
	conn, err := net.Dial("unix", path)
	if err != nil {
		return nil, err
	}
	if err := protocol.NegotiateClient(conn, protocol.ProtocolVersion); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return conn, nil
}
