//go:build windows || darwin

package main

import (
	"errors"
	"net/url"
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
