// Package release 提供 CLI 与 Desktop 共用的 GitHub Release 查询。
package release

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/alwqx/sec/utils"
)

const LatestURL = "https://github.com/alwqx/sec/releases/latest"

type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}
type Info struct {
	TagName string  `json:"tag_name"`
	Name    string  `json:"name"`
	Assets  []Asset `json:"assets"`
}

func FetchLatest(ctx context.Context) (*Info, error) {
	headers := http.Header{}
	headers.Set("Accept", "application/vnd.github+json")
	response, err := utils.MakeRequest(ctx, http.MethodGet, "https://api.github.com/repos/alwqx/sec/releases/latest", headers, nil, 25*time.Second)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub API 返回状态 %d", response.StatusCode)
	}
	const max = 2 * 1024 * 1024
	body, err := io.ReadAll(io.LimitReader(response.Body, max+1))
	if err != nil {
		return nil, err
	}
	if len(body) > max {
		return nil, fmt.Errorf("Release 响应过大")
	}
	var result Info
	if err = json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	if result.TagName == "" || strings.ContainsAny(result.TagName, "\r\n") {
		return nil, fmt.Errorf("Release 版本信息无效")
	}
	return &result, nil
}

func PageURL(tag string) string {
	return "https://github.com/alwqx/sec/releases/tag/" + url.PathEscape(tag)
}

func ValidateURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "github.com" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("仅支持 SEC 官方 GitHub Release 链接")
	}
	if path.Clean(parsed.Path) != parsed.Path {
		return fmt.Errorf("无效的 Release 路径")
	}
	if parsed.Path == "/alwqx/sec/releases/latest" || strings.HasPrefix(parsed.Path, "/alwqx/sec/releases/tag/") || strings.HasPrefix(parsed.Path, "/alwqx/sec/releases/download/") {
		return nil
	}
	return fmt.Errorf("仅支持 SEC 官方 GitHub Release 链接")
}

// DesktopAsset 只匹配实际发布的 macOS DMG，避免把 CLI 压缩包当作 Desktop 安装包。
func DesktopAsset(info *Info, goos, arch string) *Asset {
	if goos != "darwin" || arch != "arm64" && arch != "amd64" {
		return nil
	}
	want := "sec-" + info.TagName + "-desktop-darwin-" + arch + ".dmg"
	expected := "https://github.com/alwqx/sec/releases/download/" + url.PathEscape(info.TagName) + "/" + url.PathEscape(want)
	for _, asset := range info.Assets {
		if asset.Name == want && asset.BrowserDownloadURL == expected {
			copy := asset
			return &copy
		}
	}
	return nil
}
