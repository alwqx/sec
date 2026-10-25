//go:build windows || darwin

package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/alwqx/sec/provider/sina"
	"github.com/alwqx/sec/utils"
	"github.com/alwqx/sec/watchlist"
)

// workspaceStore 仅保存桌面偏好；旧 watchlist 字段由共享自选存储迁移。
type workspaceStore struct {
	mu   sync.Mutex
	path string
}

type workspaceSettings struct {
	DownloadDirectory string `json:"download_directory"`
	RefreshSeconds    int    `json:"refresh_seconds"`
	Theme             string `json:"theme"`
}

type workspaceState struct {
	Watchlist []sina.BasicSecurity `json:"watchlist"`
	Settings  workspaceSettings    `json:"settings"`
}

func defaultWorkspace() workspaceState {
	return workspaceState{Watchlist: []sina.BasicSecurity{}, Settings: workspaceSettings{RefreshSeconds: 5, Theme: "dark"}}
}

func (s *workspaceStore) filePath() (string, error) {
	if s.path != "" {
		return s.path, nil
	}
	dir, err := utils.SecDir("desktop")
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "workspace.json"), nil
}

var workspaceCode = regexp.MustCompile(`^[A-Za-z0-9$._-]{1,40}$`)

func validateWorkspace(state workspaceState) bool {
	if len(state.Settings.DownloadDirectory) > 4096 {
		return false
	}
	if _, err := downloadDirectory(state.Settings.DownloadDirectory); err != nil {
		return false
	}
	if len(state.Watchlist) > 500 {
		return false
	}
	switch state.Settings.RefreshSeconds {
	case 5, 15, 30, 60:
	default:
		return false
	}
	switch state.Settings.Theme {
	case "dark", "light", "system":
	default:
		return false
	}
	seen := make(map[string]bool)
	for _, item := range state.Watchlist {
		if !workspaceCode.MatchString(item.ExCode) || item.Code == "" || len(item.Code) > 40 || item.Name == "" || len(item.Name) > 200 || len(item.ExChange) > 20 || (item.SecurityType != "stock" && item.SecurityType != "fund") || seen[item.ExCode] {
			return false
		}
		seen[item.ExCode] = true
	}
	return true
}

func (s *workspaceStore) load() (workspaceState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := defaultWorkspace()
	path, err := s.filePath()
	if err != nil {
		return state, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	unlock, err := watchlist.FileLock(ctx, path)
	if err != nil {
		return state, err
	}
	defer unlock()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if err = json.Unmarshal(data, &state); err != nil {
		return state, err
	}
	if !validateWorkspace(state) {
		return state, errors.New("本地工作台数据无效")
	}
	if state.Watchlist == nil {
		state.Watchlist = []sina.BasicSecurity{}
	}
	return state, nil
}

func (s *workspaceStore) save(state workspaceState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := s.filePath()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	unlock, err := watchlist.FileLock(ctx, path)
	if err != nil {
		return err
	}
	defer unlock()
	data, err := json.MarshalIndent(struct {
		Settings workspaceSettings `json:"settings"`
	}{state.Settings}, "", "  ")
	if err != nil {
		return err
	}
	return watchlist.AtomicWrite(path, data)
}

func (s *desktopServer) handleWorkspace(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		state, err := s.loadSharedWorkspace(r.Context())
		if err != nil {
			slog.ErrorContext(r.Context(), "读取工作台失败", "error", err)
			writeError(w, http.StatusInternalServerError, "读取本地自选与设置失败，请检查数据文件")
			return
		}
		writeJSON(w, state)
		return
	}
	// JSON 内容类型与同源校验阻止跨站表单或网页修改本地数据。
	if r.Header.Get("Content-Type") != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "请使用 JSON 格式")
		return
	}
	if !s.dev && r.Header.Get("Origin") != "" && r.Header.Get("Origin") != "http://"+r.Host {
		writeError(w, http.StatusForbidden, "不允许跨站修改工作台")
		return
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256*1024))
	decoder.DisallowUnknownFields()
	var state workspaceState
	if err := decoder.Decode(&state); err != nil || !validateWorkspace(state) {
		writeError(w, http.StatusBadRequest, "自选或设置格式无效")
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeError(w, http.StatusBadRequest, "请求必须包含单个 JSON 对象")
		return
	}
	if state.Watchlist == nil {
		state.Watchlist = []sina.BasicSecurity{}
	}
	store, err := s.sharedWatchlist()
	if err != nil {
		writeError(w, 500, "自选存储初始化失败")
		return
	}
	if _, err = store.List(r.Context()); err != nil {
		writeError(w, 500, "自选迁移失败，请检查数据文件")
		return
	}
	if err := s.workspace.save(state); err != nil {
		slog.ErrorContext(r.Context(), "保存工作台失败", "error", err)
		writeError(w, http.StatusInternalServerError, "保存自选与设置失败，请稍后重试")
		return
	}
	state, err = s.loadSharedWorkspace(r.Context())
	if err != nil {
		writeError(w, 500, "重新读取工作台失败")
		return
	}
	writeJSON(w, state)
}
