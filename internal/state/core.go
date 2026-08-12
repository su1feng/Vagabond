package state

import (
	"context"
	"io"
	"log/slog"
	"sync"
)

// snapRequest 是 Snapshot 查询的 actor 请求：核心 goroutine 把深拷贝发到 reply。
// reply buffered(1)：核心 goroutine 发送不阻塞（即使调用方 ctx 取消后不读 reply）。
type snapRequest struct {
	reply chan *AppState
}

// Core 是 AppState 的单持有者句柄（Rule 1「核心 goroutine」）。
// 它封装核心 goroutine + 两个 channel：acts（变更）、snaps（查询）。
// 零值不可用，必须通过 New 创建。
type Core struct {
	acts   chan Action
	snaps  chan snapRequest
	state  *AppState
	logger *slog.Logger

	cancel   context.CancelFunc
	done     chan struct{}
	stopOnce sync.Once
}

// New 创建并启动核心 goroutine，返回 Core 句柄。
// 初始 AppState 为空（无 workspace）；调用方通过 Send 填充。
func New() *Core {
	ctx, cancel := context.WithCancel(context.Background())
	c := &Core{
		acts:   make(chan Action),
		snaps:  make(chan snapRequest),
		state:  &AppState{},
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		cancel: cancel,
		done:   make(chan struct{}),
	}
	go c.loop(ctx)
	return c
}

// WithLogger 注入 logger（默认静默 io.Discard）；应在首次 Send/Snapshot 前调用。
func (c *Core) WithLogger(l *slog.Logger) *Core {
	if l != nil {
		c.logger = l
	}
	return c
}

// Send 投递一个变更 Action 到核心 goroutine（fire-and-forget）。
// 阻塞直到核心 goroutine 接收。约定 Stop 后不再调用（核心 goroutine 已退出）。
func (c *Core) Send(act Action) {
	c.acts <- act
}

// Snapshot 请求当前 AppState 的只读深拷贝。经 channel 由核心 goroutine 产生，
// 保证读到的是核心串行处理后的一致快照。ctx 取消时立即返回 ctx.Err()。
// 返回值是深拷贝，调用方约定只读（修改不影响核心 state）。
func (c *Core) Snapshot(ctx context.Context) (*AppState, error) {
	req := snapRequest{reply: make(chan *AppState, 1)}
	select {
	case c.snaps <- req:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case s := <-req.reply:
		return s, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Stop 取消核心 goroutine 并阻塞至其退出。幂等。
func (c *Core) Stop() {
	c.stopOnce.Do(func() {
		c.cancel()
		<-c.done
	})
}

// loop 是核心 goroutine：单线程串行应用 Action、应答 Snapshot，直到 ctx 取消。
// AppState 只在此 goroutine 内被读写（Rule 1）。
func (c *Core) loop(ctx context.Context) {
	defer close(c.done)
	for {
		select {
		case act := <-c.acts:
			if err := reduce(c.state, act); err != nil {
				c.logger.Debug("state: reduce error", "action", act, "err", err)
			}
		case req := <-c.snaps:
			req.reply <- clone(c.state)
		case <-ctx.Done():
			return
		}
	}
}
