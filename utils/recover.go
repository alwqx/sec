package utils

import (
	"context"
	"errors"
	"log/slog"
	"runtime/debug"
)

// ErrPanic 表示并发子任务发生 panic；panic 的值已记入错误日志，堆栈只在 Debug 级别记录，不把运行时细节暴露给上层输出。
var ErrPanic = errors.New("内部错误")

// Recover 运行 fn 并接住其中的 panic，记日志后返回 ErrPanic。供工具或业务包另起 goroutine 时使用，
// 避免单个子任务 panic 导致整个进程（sec mcp、sec ask、Desktop）退出。
func Recover(ctx context.Context, task string, fn func() error) (err error) {
	defer func() {
		if p := recover(); p != nil {
			slog.ErrorContext(ctx, "goroutine panicked", "task", task, "panic", p)
			if slog.Default().Enabled(ctx, slog.LevelDebug) {
				slog.DebugContext(ctx, "goroutine panic stack", "task", task, "stack", string(debug.Stack()))
			}
			err = ErrPanic
		}
	}()
	return fn()
}
