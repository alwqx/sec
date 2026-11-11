//go:build windows || darwin

package main

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alwqx/sec/internal/sse"
	"github.com/alwqx/sec/llm"
	"github.com/alwqx/sec/valuation"
)

func wantsAIStream(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept"), ",") {
		kind, params, err := mime.ParseMediaType(strings.TrimSpace(part))
		if err == nil && kind == "text/event-stream" {
			if q, exists := params["q"]; exists {
				weight, err := strconv.ParseFloat(q, 64)
				if err != nil || !(weight > 0 && weight <= 1) {
					continue
				}
			}
			return true
		}
	}
	return false
}

// aiStream 串行写出正文与心跳；客户端断开或写超时后取消模型请求。
type aiStream struct {
	w        http.ResponseWriter
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex
	err      error
	terminal bool
	stopped  chan struct{}
}

func newAIStream(w http.ResponseWriter, parent context.Context) *aiStream {
	ctx, cancel := context.WithCancel(parent)
	s := &aiStream{w: w, ctx: ctx, cancel: cancel, stopped: make(chan struct{})}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	go func() {
		defer close(s.stopped)
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if s.send("", nil) != nil {
					return
				}
			}
		}
	}()
	return s
}

func (s *aiStream) close() { s.cancel(); <-s.stopped }

func (s *aiStream) send(kind string, data any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	if err := s.ctx.Err(); err != nil {
		return err
	}
	if s.terminal {
		return nil
	}
	control := http.NewResponseController(s.w)
	// 限制慢客户端的单次写出时间；Recorder 等不支持 deadline，仍可用于测试。
	_ = control.SetWriteDeadline(time.Now().Add(15 * time.Second))
	if kind == "" {
		_, s.err = io.WriteString(s.w, ": ping\n\n")
	} else {
		s.err = sse.Write(s.w, kind, data)
	}
	if s.err == nil {
		s.err = control.Flush()
	}
	if s.err != nil {
		s.cancel()
		return s.err
	}
	s.terminal = kind == "done" || kind == "error"
	return nil
}

func (s *desktopServer) streamAIValuation(w http.ResponseWriter, r *http.Request, code string, params valuation.DCFParams, input json.RawMessage, client llm.Client, model llm.Model) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute+40*time.Second)
	defer cancel()
	stream := newAIStream(w, ctx)
	defer func() {
		if recover() != nil {
			_ = stream.send("error", map[string]any{"message": "估值解读异常，请稍后重试", "status": 500})
		}
		stream.close()
	}()
	if stream.send("status", map[string]string{"text": "正在获取估值数据…"}) != nil {
		return
	}
	result, err := s.fetchValuation(stream.ctx, code, params)
	if err != nil {
		status, message := valuationFailure(err)
		_ = stream.send("error", map[string]any{"message": message, "status": status})
		return
	}
	explainer := valuationExplainer(stream.ctx, code, result, client, model)
	modelCtx, cancelModel := context.WithTimeout(stream.ctx, 5*time.Minute)
	defer cancelModel()
	if stream.send("status", map[string]string{"text": "正在生成解读…"}) != nil {
		return
	}
	explanation, err := explainer.ExplainStream(modelCtx, input, func(event llm.ExplanationEvent) error {
		if event.Type == "meta" {
			return stream.send("meta", event.Meta)
		}
		return stream.send(event.Type, map[string]string{"text": event.Text})
	})
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		status, message := explanationFailure(modelCtx, err)
		_ = stream.send("error", map[string]any{"message": message, "status": status})
		return
	}
	_ = stream.send("done", explanation)
}
