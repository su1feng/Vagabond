package protocol

import (
	"encoding/json"
	"fmt"
	"io"
)

// NegotiateServer 在服务端执行握手：读客户端 Hello、校验版本、回 Welcome。
// 版本不匹配时回 Welcome{OK:false} 并返回 ErrVersionMismatch，调用方应关闭连接。
// 无 magic preamble，靠首帧版本校验兜底。
func NegotiateServer(rw io.ReadWriter, serverVersion uint32) error {
	payload, err := ReadFrame(rw)
	if err != nil {
		return err
	}
	var h Hello
	if err := json.Unmarshal(payload, &h); err != nil {
		return fmt.Errorf("protocol: decode hello: %w", err)
	}

	var w Welcome
	switch h.Version {
	case serverVersion:
		w = Welcome{Version: serverVersion, OK: true}
	default:
		w = Welcome{
			Version: serverVersion,
			OK:      false,
			Reason:  fmt.Sprintf("client protocol v%d != server v%d", h.Version, serverVersion),
		}
	}

	out, err := json.Marshal(w)
	if err != nil {
		return err
	}
	if err := WriteFrame(rw, out); err != nil {
		return err
	}
	if !w.OK {
		return ErrVersionMismatch
	}
	return nil
}

// NegotiateClient 在客户端执行握手：发 Hello、读 Welcome、校验 OK。
// 与 NegotiateServer 的读写序列在 net.Pipe 等全双工连接上对齐，无死锁。
func NegotiateClient(rw io.ReadWriter, clientVersion uint32) error {
	out, err := json.Marshal(Hello{Version: clientVersion})
	if err != nil {
		return err
	}
	if err := WriteFrame(rw, out); err != nil {
		return err
	}
	payload, err := ReadFrame(rw)
	if err != nil {
		return err
	}
	var w Welcome
	if err := json.Unmarshal(payload, &w); err != nil {
		return fmt.Errorf("protocol: decode welcome: %w", err)
	}
	if !w.OK {
		return fmt.Errorf("%w: %s", ErrVersionMismatch, w.Reason)
	}
	return nil
}
