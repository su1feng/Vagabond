package protocol

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
)

func TestFrameRoundTrip(t *testing.T) {
	cases := [][]byte{
		[]byte{},
		[]byte("hello"),
		bytes.Repeat([]byte("x"), 1024),
		bytes.Repeat([]byte("y"), MaxFrameSize),   // 正好等于上限
		bytes.Repeat([]byte("z"), MaxFrameSize-1), // 刚好不超长
	}
	for i, payload := range cases {
		var buf bytes.Buffer
		if err := WriteFrame(&buf, payload); err != nil {
			t.Fatalf("case %d: WriteFrame: %v", i, err)
		}
		got, err := ReadFrame(&buf)
		if err != nil {
			t.Fatalf("case %d: ReadFrame: %v", i, err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("case %d: payload mismatch: got %d bytes want %d", i, len(got), len(payload))
		}
	}
}

func TestWriteFrameTooLarge(t *testing.T) {
	var buf bytes.Buffer
	payload := make([]byte, MaxFrameSize+1)
	err := WriteFrame(&buf, payload)
	if !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("WriteFrame: got %v, want ErrFrameTooLarge", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("WriteFrame wrote %d bytes on error, want 0", buf.Len())
	}
}

func TestReadFrameTooLarge(t *testing.T) {
	var buf bytes.Buffer
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(MaxFrameSize+1))
	buf.Write(hdr[:])
	_, err := ReadFrame(&buf)
	if !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("ReadFrame: got %v, want ErrFrameTooLarge", err)
	}
}

func TestReadFrameMaliciousLength(t *testing.T) {
	// 声明 uint32 最大值：不应导致溢出或超大分配，应直接拒绝。
	var buf bytes.Buffer
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], 0xFFFFFFFF)
	buf.Write(hdr[:])
	_, err := ReadFrame(&buf)
	if !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("ReadFrame: got %v, want ErrFrameTooLarge", err)
	}
}

func TestReadFrameTruncatedPayload(t *testing.T) {
	var buf bytes.Buffer
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], 10)
	buf.Write(hdr[:])
	buf.Write([]byte("abc")) // 声明 10 字节，只给 3
	_, err := ReadFrame(&buf)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("ReadFrame: got %v, want io.ErrUnexpectedEOF", err)
	}
}

func TestReadFrameEmptyStream(t *testing.T) {
	_, err := ReadFrame(bytes.NewReader(nil))
	if !errors.Is(err, io.EOF) {
		t.Fatalf("ReadFrame: got %v, want io.EOF", err)
	}
}

func TestReadFramePeerDisconnect(t *testing.T) {
	// 对端写了长度头但未写 payload 就断开，读 payload 时应报错。
	// client.Write 放 goroutine：net.Pipe 无缓冲，write 阻塞直到对端 read。
	client, server := net.Pipe()
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], 100)
	go func() {
		_, _ = client.Write(hdr[:])
		_ = client.Close()
	}()
	defer server.Close()

	_, err := ReadFrame(server)
	if err == nil {
		t.Fatal("ReadFrame: want error on peer disconnect, got nil")
	}
}

func TestFrameConcurrent(t *testing.T) {
	// 多对独立 pipe 并发 framing，验证 ReadFrame/WriteFrame 无共享状态、不串。
	const N = 8
	var wg sync.WaitGroup
	errc := make(chan error, N*2)
	for i := 0; i < N; i++ {
		client, server := net.Pipe()
		defer client.Close()
		defer server.Close()
		payload := []byte(fmt.Sprintf("concurrent-msg-%d", i))
		wg.Add(2)
		go func() {
			defer wg.Done()
			if err := WriteFrame(client, payload); err != nil {
				errc <- fmt.Errorf("WriteFrame: %w", err)
			}
		}()
		go func() {
			defer wg.Done()
			got, err := ReadFrame(server)
			if err != nil {
				errc <- fmt.Errorf("ReadFrame: %w", err)
				return
			}
			if !bytes.Equal(got, payload) {
				errc <- fmt.Errorf("payload mismatch")
			}
		}()
	}
	wg.Wait()
	close(errc)
	for err := range errc {
		t.Fatal(err)
	}
}

func TestWriteFrameToBrokenWriter(t *testing.T) {
	// 写到已关闭的连接应返回错误。
	client, server := net.Pipe()
	if err := client.Close(); err != nil {
		t.Fatalf("client close: %v", err)
	}
	defer server.Close()
	if err := WriteFrame(server, []byte("anything")); err == nil {
		t.Fatal("WriteFrame: want error on closed writer, got nil")
	}
}
