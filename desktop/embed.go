//go:build windows || darwin

package main

import (
	"embed"
	"io/fs"
)

// uiDist 内嵌 React 前端构建产物。正式构建前需在 desktop/ui 下执行 npm run build，
// 否则只会内嵌占位的 index.html。
//
//go:embed ui/dist
var uiDist embed.FS

// uiFS 返回剥离 ui/dist 前缀后的前端文件系统。
func uiFS() (fs.FS, error) {
	return fs.Sub(uiDist, "ui/dist")
}
