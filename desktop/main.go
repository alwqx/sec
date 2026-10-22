//go:build windows || darwin

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/webview/webview_go"
)

func main() {
	dev := flag.Bool("dev", false, "开发模式：加载 Vite dev server（5173）并打开 CORS")
	debug := flag.Bool("debug", false, "开启 debug 日志与 WebView 调试工具")
	flag.Parse()

	level := slog.LevelInfo
	if *debug {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	// 监听随机端口；dev 模式固定 3001，便于 Vite 代理。
	addr := "127.0.0.1:0"
	if *dev {
		addr = "127.0.0.1:3001"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		slog.Error("监听端口失败", "error", err)
		os.Exit(1)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	srv := &http.Server{Handler: (&desktopServer{token: newToken(), dev: *dev}).Handler()}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("http server", "error", err)
		}
	}()

	url := fmt.Sprintf("http://127.0.0.1:%d/", port)
	if *dev {
		url = "http://localhost:5173/"
	}

	// webview 必须在主线程创建与运行（webview_go 的 init 会 LockOSThread）。
	w := webview.New(*debug)
	defer w.Destroy()
	w.SetTitle("sec · 证券行情")
	w.SetSize(960, 680, webview.HintNone)
	w.Navigate(url)

	// 信号处理：优雅关闭 HTTP server 并退出 WebView 主循环。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		w.Terminate()
	}()

	w.Run()
}

// newToken 生成随机鉴权 token。
func newToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
