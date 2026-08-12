// Package protocol 实现 daemon 与客户端之间的传输层：长度前缀帧 + 版本握手。
// 两个客户端 socket（Rule 4）共用 framing 原语；payload 由上层按 socket 类型决定，
// 本包只切字节边界、不解析 payload。
package protocol

import "errors"

// ProtocolVersion 是传输协议版本，整数递增（Rule 4）；握手时双方必须精确匹配。
// 任何不兼容的改动必须递增本版本。
const ProtocolVersion uint32 = 1

// MaxFrameSize 是单帧 payload 的最大字节数，超长则 ReadFrame/WriteFrame 返回 ErrFrameTooLarge。
const MaxFrameSize int = 2 << 20 // 2 MiB

var (
	// ErrFrameTooLarge 在帧声明的长度超过 MaxFrameSize 时返回。
	ErrFrameTooLarge = errors.New("protocol: frame exceeds MaxFrameSize")
	// ErrVersionMismatch 在握手版本不匹配时返回。
	ErrVersionMismatch = errors.New("protocol: version mismatch")
)

// Hello 是客户端握手阶段发送的首帧 payload（JSON）。
// 客户端必须先发 Hello，服务端校验版本后回 Welcome。
type Hello struct {
	// Version 是客户端期望的协议版本，必须与服务端 ProtocolVersion 精确匹配。
	Version uint32 `json:"v"`
	// Capabilities 是客户端声明的能力（feature-level 协商），可为空，现阶段留空。
	Capabilities []string `json:"cap,omitempty"`
}

// Welcome 是服务端对 Hello 的响应（JSON）。
type Welcome struct {
	// Version 是服务端的协议版本。
	Version uint32 `json:"v"`
	// OK 为 true 表示握手通过；false 表示不兼容，连接将被关闭。
	OK bool `json:"ok"`
	// Capabilities 是服务端声明的能力，可为空。
	Capabilities []string `json:"cap,omitempty"`
	// Reason 在 OK=false 时给出不兼容原因（如 upgrade 提示）。
	Reason string `json:"reason,omitempty"`
}
