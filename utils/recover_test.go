package utils

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecover(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, Recover(ctx, "ok", func() error { return nil }))

	sentinel := errors.New("upstream failed")
	require.ErrorIs(t, Recover(ctx, "fail", func() error { return sentinel }), sentinel)

	var buf bytes.Buffer
	orig := slog.Default()
	t.Cleanup(func() { slog.SetDefault(orig) })

	// Info 级别只记录 panic 的值，不输出堆栈。
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	err := Recover(ctx, "boom", func() error {
		var m map[string]int
		m["x"] = 1
		return nil
	})
	require.ErrorIs(t, err, ErrPanic)
	require.Equal(t, "内部错误", err.Error(), "不把 panic 的值放进错误文本")
	require.Contains(t, buf.String(), "goroutine panicked")
	require.Contains(t, buf.String(), "task=boom")
	require.NotContains(t, buf.String(), "goroutine panic stack")

	// Debug 级别额外记录堆栈。
	buf.Reset()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	err = Recover(ctx, "boom_debug", func() error {
		panic("unexpected state")
	})
	require.ErrorIs(t, err, ErrPanic)
	require.Contains(t, buf.String(), "goroutine panicked")
	require.Contains(t, buf.String(), "goroutine panic stack")
	require.Contains(t, buf.String(), "TestRecover")
}
