//go:build windows || darwin

package main

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"

	"github.com/alwqx/sec/provider/sina"
	"github.com/alwqx/sec/watchlist"
)

func (s *desktopServer) sharedWatchlist() (watchlist.Store, error) {
	if s.workspace.path != "" {
		return watchlist.Store{Path: filepath.Join(filepath.Dir(s.workspace.path), "watchlist.json"), WorkspacePath: s.workspace.path}, nil
	}
	return watchlist.Default()
}
func watchSecurities(items []watchlist.Item) []sina.BasicSecurity {
	results := make([]sina.BasicSecurity, 0, len(items))
	for _, item := range items {
		results = append(results, item.Security())
	}
	return results
}
func (s *desktopServer) loadSharedWorkspace(ctx context.Context) (workspaceState, error) {
	store, err := s.sharedWatchlist()
	if err != nil {
		return workspaceState{}, err
	}
	items, err := store.List(ctx)
	if err != nil {
		return workspaceState{}, err
	}
	state, err := s.workspace.load()
	state.Watchlist = watchSecurities(items)
	return state, err
}
func (s *desktopServer) handleWatchlist(w http.ResponseWriter, r *http.Request) {
	store, err := s.sharedWatchlist()
	if err != nil {
		writeError(w, 500, "读取共享自选失败")
		return
	}
	var items []watchlist.Item
	if r.Method == http.MethodGet {
		items, err = store.List(r.Context())
	} else {
		var request struct {
			Action   string             `json:"action"`
			Security sina.BasicSecurity `json:"security"`
			Code     string             `json:"code"`
		}
		if !s.readDownloadRequest(w, r, &request) {
			return
		}
		switch request.Action {
		case "add":
			items, _, err = store.Add(r.Context(), request.Security)
		case "remove":
			if !workspaceCode.MatchString(request.Code) {
				writeError(w, 400, "证券标识无效")
				return
			}
			items, _, err = store.Remove(r.Context(), request.Code)
		default:
			writeError(w, 400, "自选操作必须为 add 或 remove")
			return
		}
	}
	if err != nil {
		if errors.Is(err, watchlist.ErrInvalid) || errors.Is(err, watchlist.ErrLimit) {
			writeError(w, 400, err.Error())
			return
		}
		writeError(w, 500, "共享自选操作失败："+err.Error())
		return
	}
	writeJSON(w, map[string]any{"results": watchSecurities(items)})
}
