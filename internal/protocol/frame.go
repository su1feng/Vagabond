package protocol

import (
	"encoding/binary"
	"io"
)

// ReadFrame 读取一帧并返回 payload（不含长度前缀）。
// 帧格式：[length: uint32 大端][payload]，length 不含自身。
// 超长返回 ErrFrameTooLarge；连接断开返回 io.EOF 或 io.ErrUnexpectedEOF。
func ReadFrame(r io.Reader) ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if int64(n) > int64(MaxFrameSize) { // 防 32 位平台溢出
		return nil, ErrFrameTooLarge
	}
	if n == 0 {
		return []byte{}, nil
	}
	payload := make([]byte, int(n))
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// WriteFrame 把 payload 作为一帧写入 w。超长返回 ErrFrameTooLarge（不写入任何字节）。
// 非原子：先写长度头再写 payload，调用方需保证同一 w 不被并发写入。
func WriteFrame(w io.Writer, payload []byte) error {
	if len(payload) > MaxFrameSize {
		return ErrFrameTooLarge
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(payload))) //nolint:gosec // len 已在上面校验 <= MaxFrameSize，不会溢出 uint32
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	if len(payload) == 0 {
		return nil
	}
	if _, err := w.Write(payload); err != nil {
		return err
	}
	return nil
}
