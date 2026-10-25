//go:build windows || darwin

package main

import (
	"context"
	"net/http"
	"runtime"
	"time"

	"github.com/alwqx/sec/release"
	"github.com/alwqx/sec/version"
)

func (s *desktopServer) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"version": version.Version, "build_time": version.BuildTime, "git_commit": version.GitCommit, "os": runtime.GOOS, "arch": runtime.GOARCH, "release_url": release.LatestURL})
}

func (s *desktopServer) handleUpdates(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	info, err := release.FetchLatest(ctx)
	if err != nil {
		writeError(w, 502, "检查更新失败，可能是网络异常或 GitHub 请求限额，请稍后重试或直接查看 Release")
		return
	}
	status := "unknown"
	if order, known := release.Compare(version.Version, info.TagName); known {
		if order < 0 {
			status = "available"
		} else if order == 0 {
			status = "current"
		} else {
			status = "ahead"
		}
	}
	asset := release.DesktopAsset(info, runtime.GOOS, runtime.GOARCH)
	writeJSON(w, map[string]any{"latest_version": info.TagName, "status": status, "release_url": release.PageURL(info.TagName), "asset": asset})
}
