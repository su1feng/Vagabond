// Package main 是 vagabond 命令入口。
//
// vagabond 的入口只做分发（AGENTS.md Project Map：cmd 只分发，不放业务）。当前为
// 可运行骨架：serve 起 daemon（前台），snapshot 连本地 daemon 打印 state。
// 完整的「检测 daemon → 拉起 → attach」分发依赖 TUI（internal/client），留后续批次。
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
)

// apiSocketFile 是 daemon JSON API socket 的文件名（与 internal/daemon 的 apiSocketName 对应）。
const apiSocketFile = "api.sock"

// version 是 vagabond 的版本字符串，骨架阶段为占位值。
// 正式构建时可用 `go build -ldflags "-X main.version=<ver>"` 覆盖。
var version = "0.0.0-dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "vagabond:", err)
		os.Exit(1)
	}
}

// run 分发子命令。返回 error 由 main 打印到 stderr 并 exit 1。
func run(args []string) error {
	if len(args) == 0 {
		usage(os.Stdout)
		return nil
	}
	switch args[0] {
	case "serve":
		// 前台运行，SIGINT/SIGTERM 触发优雅关停（Rule 7：context 取消）。
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return runServe(ctx)
	case "snapshot":
		return runSnapshot(os.Stdout)
	case "version":
		fmt.Println("vagabond", version)
		return nil
	case "help", "-h", "--help":
		usage(os.Stdout)
		return nil
	default:
		usage(os.Stderr)
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

func usage(w io.Writer) {
	_, _ = fmt.Fprint(w, `usage: vagabond <command>

commands:
  serve     run the daemon in the foreground (SIGINT/SIGTERM to stop)
  snapshot  print current state from the running daemon
  version   print version
`)
}
