//go:build !windows && !darwin

package main

import (
	"fmt"
	"os"
)

// 窗口依赖 webview，只能在 Windows 和 macOS 上运行。其他平台保留这个入口，让 HTTP 接口和测试也能在 Linux CI 上编译运行。
func main() {
	fmt.Fprintln(os.Stderr, "sec Desktop 只支持 macOS 和 Windows")
	os.Exit(1)
}
