//go:build windows || darwin

package main

import (
	"errors"
	"github.com/alwqx/sec/release"
	"net/url"
	"os"
	"os/exec"
	"runtime"
)

func validateDisclosureURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || (u.Host != "www.cninfo.com.cn" && u.Host != "static.cninfo.com.cn") {
		return errors.New("仅支持打开巨潮资讯原文链接")
	}
	return nil
}

// openDisclosure 由用户点击原文后调用，交给系统浏览器，避免替换研究工作台。
func openDisclosure(raw string) error {
	if err := validateDisclosureURL(raw); err != nil {
		return err
	}
	var command *exec.Cmd
	if runtime.GOOS == "darwin" {
		command = exec.Command("open", raw)
	} else {
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", raw)
	}
	if err := command.Start(); err != nil {
		return errors.New("打开原文失败，请复制链接到浏览器")
	}
	go func() { _ = command.Wait() }()
	return nil
}

// openDownloadFolder 打开工作台设置中的下载目录，不接收前端传入的路径。
func (s *desktopServer) openDownloadFolder() error {
	directory, err := s.downloadDirectory()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	var command *exec.Cmd
	if runtime.GOOS == "darwin" {
		command = exec.Command("open", directory)
	} else {
		command = exec.Command("explorer", directory)
	}
	if err := command.Start(); err != nil {
		return err
	}
	go func() { _ = command.Wait() }()
	return nil
}

// openRelease 在系统浏览器打开官方发布页面或 Desktop 安装包。
func openRelease(raw string) error {
	if err := release.ValidateURL(raw); err != nil {
		return err
	}
	var command *exec.Cmd
	if runtime.GOOS == "darwin" {
		command = exec.Command("open", raw)
	} else {
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", raw)
	}
	if err := command.Start(); err != nil {
		return errors.New("打开 Release 失败，请在浏览器访问 GitHub SEC 发布页面")
	}
	go func() { _ = command.Wait() }()
	return nil
}
