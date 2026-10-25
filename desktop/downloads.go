//go:build windows || darwin

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type savedDownload struct {
	Filename string `json:"filename"`
	Path     string `json:"path"`
	Size     int64  `json:"size"`
}

func downloadDirectory(override string) (string, error) {
	override = strings.TrimSpace(override)
	if override == "~" || strings.HasPrefix(override, "~/") || strings.HasPrefix(override, `~\`) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		override = filepath.Join(home, strings.TrimLeft(override[1:], `/\`))
	}
	if override != "" {
		if !filepath.IsAbs(override) || strings.ContainsRune(override, 0) {
			return "", errors.New("下载目录必须是绝对路径或以 ~/ 开头")
		}
		return filepath.Clean(override), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Downloads"), nil
}

func (s *desktopServer) downloadDirectory() (string, error) {
	if s.downloadRoot != "" {
		return downloadDirectory(s.downloadRoot)
	}
	state, err := s.workspace.load()
	if err != nil {
		return "", err
	}
	return downloadDirectory(state.Settings.DownloadDirectory)
}

// saveDownload 先完整写入临时文件，再以独占的文件名发布；重复下载不会覆盖已有文件。
func (s *desktopServer) saveDownload(filename string, write func(io.Writer) error) (*savedDownload, error) {
	directory, err := s.downloadDirectory()
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	file, err := os.CreateTemp(directory, ".sec-download-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err = write(file); err != nil {
		return nil, err
	}
	if err = file.Sync(); err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if err = file.Close(); err != nil {
		return nil, err
	}
	extension := filepath.Ext(filename)
	base := filename[:len(filename)-len(extension)]
	for index := 0; index < 1000; index++ {
		name := filename
		if index > 0 {
			name = fmt.Sprintf("%s_%d%s", base, index+1, extension)
		}
		path := filepath.Join(directory, name)
		// 同目录硬链接实现不覆盖的原子发布，随后删除临时名称。
		if err := os.Link(file.Name(), path); err != nil {
			if errors.Is(err, os.ErrExist) {
				continue
			}
			return nil, err
		}
		return &savedDownload{Filename: name, Path: path, Size: info.Size()}, nil
	}
	return nil, errors.New("同名下载文件过多")
}

func (s *desktopServer) readDownloadRequest(w http.ResponseWriter, r *http.Request, value any) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(w, 415, "请使用 JSON 格式")
		return false
	}
	if !s.dev && r.Header.Get("Origin") != "" && r.Header.Get("Origin") != "http://"+r.Host {
		writeError(w, 403, "不允许跨站保存文件")
		return false
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		writeError(w, 400, "下载参数格式无效")
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeError(w, 400, "请求必须包含单个 JSON 对象")
		return false
	}
	return true
}
