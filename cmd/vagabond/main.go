// Package main 是 vagabond 命令入口。
//
// vagabond 的入口只做分发:检测 daemon 是否在运行 → 不存在则拉起 daemon →
// attach 客户端。业务逻辑全部在 internal/ 下,本文件保持薄。
//
// 当前为骨架占位,仅打印版本;检测/拉起/attach 的落点后续补。
package main

import "fmt"

// version 是 vagabond 的版本字符串,骨架阶段为占位值。
// 正式构建时可用 `go build -ldflags "-X main.version=<ver>"` 覆盖。
var version = "0.0.0-dev"

func main() {
	fmt.Println("vagabond", version)
	// TODO: 检测 daemon → 不存在则拉起 → attach 客户端(业务逻辑在 internal/)。
}
