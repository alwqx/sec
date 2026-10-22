//go:build windows || darwin

package main

import (
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"

	"github.com/alwqx/sec/provider/sina"
)

// desktopServer 提供本地 HTTP 服务：/api/* JSON 接口 + 前端静态资源。
type desktopServer struct {
	token string
	dev   bool
}

// Handler 返回装配好的路由。
func (s *desktopServer) Handler() http.Handler {
	mux := http.NewServeMux()

	// API 路由：显式方法 + 路径，避免与 SPA 的 GET / 冲突。
	// dev 模式跳过鉴权并打开 CORS。
	mux.Handle("GET /api/v1/search", s.withAuth(s.withCORS(http.HandlerFunc(s.handleSearch))))
	mux.Handle("GET /api/v1/quote", s.withAuth(s.withCORS(http.HandlerFunc(s.handleQuote))))

	// 前端 SPA：先尝试真实文件，否则回退 index.html（支持前端路由）。
	mux.Handle("GET /", s.spa())

	// 兜底捕获 panic，避免单个 provider 异常拖垮整个桌面进程。
	return s.withRecover(mux)
}

// withRecover 捕获 handler 中未预料的 panic，返回 500 JSON 而非空连接。
func (s *desktopServer) withRecover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if p := recover(); p != nil {
				slog.Error("panic serving request", "path", r.URL.Path, "panic", p)
				writeError(w, http.StatusInternalServerError, "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// withAuth 校验 cookie token，防止本机其他程序跨站调用。
func (s *desktopServer) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.dev {
			next.ServeHTTP(w, r)
			return
		}
		c, err := r.Cookie("token")
		if err != nil || c.Value != s.token {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// withCORS 仅在 dev 模式打开跨域，供 Vite dev server 直连。
func (s *desktopServer) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.dev {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// spa 返回前端静态资源 handler，并在响应中下发鉴权 cookie。
func (s *desktopServer) spa() http.Handler {
	fsys, err := uiFS()
	if err != nil {
		// 不应发生：占位 dist 保证 ui/dist 始终存在。
		panic(err)
	}
	fileServer := http.FileServer(http.FS(fsys))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "token", Value: s.token, Path: "/", HttpOnly: true})

		p := strings.TrimPrefix(r.URL.Path, "/")
		if p != "" {
			if f, err := fs.Stat(fsys, p); err == nil && !f.IsDir() {
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		data, err := fs.ReadFile(fsys, "index.html")
		if err != nil {
			http.Error(w, "前端未构建：请先在 desktop/ui 执行 npm run build", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(data)
	})
}

// handleSearch 搜索证券。
func (s *desktopServer) handleSearch(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.URL.Query().Get("key"))
	if key == "" {
		writeError(w, http.StatusBadRequest, "缺少 key 参数")
		return
	}
	results := sina.Search(r.Context(), key)
	if results == nil {
		results = []*sina.BasicSecurity{}
	}
	writeJSON(w, map[string]any{"results": results})
}

// handleQuote 查询单只证券行情。
func (s *desktopServer) handleQuote(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		writeError(w, http.StatusBadRequest, "缺少 code 参数")
		return
	}
	q, err := sina.QuerySecQuote(r.Context(), code)
	if err != nil {
		slog.ErrorContext(r.Context(), "query quote", "code", code, "error", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, q)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("write json", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
