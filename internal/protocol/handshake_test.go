package protocol

import (
	"errors"
	"net"
	"testing"
)

func TestHandshakeSuccess(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	errc := make(chan error, 1)
	go func() { errc <- NegotiateServer(server, ProtocolVersion) }()

	if err := NegotiateClient(client, ProtocolVersion); err != nil {
		t.Fatalf("NegotiateClient: %v", err)
	}
	if err := <-errc; err != nil {
		t.Fatalf("NegotiateServer: %v", err)
	}
}

func TestHandshakeVersionMismatch(t *testing.T) {
	// 双向：客户端版本比服务端旧（v-1）或新（v+1）都应拒绝。
	for _, clientV := range []uint32{ProtocolVersion + 1, ProtocolVersion - 1} {
		client, server := net.Pipe()
		errc := make(chan error, 1)
		go func() { errc <- NegotiateServer(server, ProtocolVersion) }()

		err := NegotiateClient(client, clientV)
		if !errors.Is(err, ErrVersionMismatch) {
			t.Fatalf("NegotiateClient(v=%d): got %v, want ErrVersionMismatch", clientV, err)
		}
		if err := <-errc; !errors.Is(err, ErrVersionMismatch) {
			t.Fatalf("NegotiateServer: got %v, want ErrVersionMismatch", err)
		}
		_ = client.Close()
		_ = server.Close()
	}
}

func TestHandshakeMalformedHello(t *testing.T) {
	// 客户端发的首帧不是合法 JSON，服务端应报 decode 错误（而非版本不匹配）。
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	errc := make(chan error, 1)
	go func() { errc <- NegotiateServer(server, ProtocolVersion) }()

	if err := WriteFrame(client, []byte("not json")); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}
	err := <-errc
	if err == nil {
		t.Fatal("NegotiateServer: want error for malformed hello, got nil")
	}
	if errors.Is(err, ErrVersionMismatch) {
		t.Fatalf("NegotiateServer: got ErrVersionMismatch, want decode error: %v", err)
	}
}

func TestHandshakeBadHelloFieldType(t *testing.T) {
	// v 字段给字符串而非数字，json 解码到 uint32 应失败。
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	errc := make(chan error, 1)
	go func() { errc <- NegotiateServer(server, ProtocolVersion) }()

	if err := WriteFrame(client, []byte(`{"v":"not-a-number"}`)); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}
	err := <-errc
	if err == nil || errors.Is(err, ErrVersionMismatch) {
		t.Fatalf("NegotiateServer: got %v, want decode error", err)
	}
}

func TestNegotiateClientMalformedWelcome(t *testing.T) {
	// server 回的 Welcome 不是合法 JSON，client 应报 decode 错误（非版本不匹配）。
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	errc := make(chan error, 1)
	go func() { errc <- NegotiateClient(client, ProtocolVersion) }()

	// server 先读掉 client 的 Hello，再回畸形 Welcome。
	if _, err := ReadFrame(server); err != nil {
		t.Fatalf("read hello: %v", err)
	}
	if err := WriteFrame(server, []byte("not json")); err != nil {
		t.Fatalf("write malformed welcome: %v", err)
	}

	err := <-errc
	if err == nil || errors.Is(err, ErrVersionMismatch) {
		t.Fatalf("NegotiateClient: got %v, want decode error", err)
	}
}
