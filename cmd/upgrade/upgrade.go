package upgrade

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/alwqx/sec/utils"
	"github.com/alwqx/sec/version"
	"github.com/spf13/cobra"
)

const (
	githubLatestAPI = "https://api.github.com/repos/alwqx/sec/releases/latest"
	maxBinarySize   = 500 * 1024 * 1024 // 500 MB
)

type releaseInfo struct {
	TagName string  `json:"tag_name"`
	Name    string  `json:"name"`
	Assets  []asset `json:"assets"`
}

type asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

func NewUpgradeCLI() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade sec to the latest version from GitHub releases",
		RunE:  UpgradeHandler,
	}
	rootCmd.Flags().BoolP("debug", "D", false, "Enable debug mode")

	return rootCmd
}

// UpgradeHandler 自动升级到最新版本
func UpgradeHandler(cmd *cobra.Command, args []string) error {
	currentVersion := version.Version
	if currentVersion == "" {
		currentVersion = "(dev)"
	}

	fmt.Printf("Current version: %s\n", currentVersion)
	fmt.Println("Checking latest release...")

	release, err := fetchLatestRelease(cmd.Context())
	if err != nil {
		return fmt.Errorf("failed to fetch latest release: %w", err)
	}

	latestVersion := release.TagName
	fmt.Printf("Latest  version: %s\n", latestVersion)

	if currentVersion == latestVersion {
		fmt.Println("Already up to date.")
		return nil
	}

	// 找到匹配当前系统架构的 asset
	asset, err := findAsset(release)
	if err != nil {
		return err
	}

	fmt.Printf("Downloading %s (%d bytes)...\n", asset.Name, asset.Size)

	// 获取当前二进制路径
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("failed to resolve executable path: %w", err)
	}

	if err := downloadAndReplace(cmd.Context(), asset, execPath); err != nil {
		return fmt.Errorf("failed to upgrade: %w", err)
	}

	return nil
}

// fetchLatestRelease 获取最新的 release 信息
func fetchLatestRelease(ctx context.Context) (*releaseInfo, error) {
	headers := http.Header{}
	headers.Set("Accept", "application/vnd.github+json")

	resp, err := utils.MakeRequest(ctx, http.MethodGet, githubLatestAPI, headers, nil, 30*time.Second)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github API returned status %d", resp.StatusCode)
	}

	var release releaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}

	if release.TagName == "" {
		return nil, errors.New("no release found")
	}

	return &release, nil
}

// findAsset 查找匹配当前 OS/Arch 的 release asset
func findAsset(release *releaseInfo) (*asset, error) {
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	// 标准化 arch 名称以匹配 release asset 命名
	archName := goarch
	switch goarch {
	case "amd64":
		archName = "amd64"
	case "arm64":
		archName = "arm64"
	default:
		return nil, fmt.Errorf("unsupported architecture: %s", goarch)
	}

	// asset 命名格式: sec-{version}-{goos}-{goarch}.tar.gz 或 .zip
	wantSuffix := fmt.Sprintf("-%s-%s.tar.gz", goos, archName)
	if goos == "windows" {
		wantSuffix = fmt.Sprintf("-%s-%s.zip", goos, archName)
	}

	for i := range release.Assets {
		a := &release.Assets[i]
		if strings.HasSuffix(a.Name, wantSuffix) {
			return a, nil
		}
	}

	return nil, fmt.Errorf("no release asset found for %s/%s", goos, archName)
}

// downloadAndReplace 下载 release asset 并替换当前二进制
func downloadAndReplace(ctx context.Context, a *asset, execPath string) error {
	// 下载到临时文件
	tmpFile, err := os.CreateTemp("", "sec-upgrade-*")
	if err != nil {
		return err
	}
	defer func() {
		closeErr := tmpFile.Close()
		if closeErr != nil {
			slog.ErrorContext(ctx, "downloadAndReplace failed close tmpFile", "error", closeErr)
		}
	}()

	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	resp, err := utils.MakeRequest(ctx, http.MethodGet, a.BrowserDownloadURL, nil, nil, 5*time.Minute)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	_, err = io.Copy(tmpFile, resp.Body)
	if err != nil {
		return err
	}

	// 解压并提取二进制
	var binaryData []byte
	if strings.HasSuffix(a.Name, ".zip") {
		binaryData, err = extractZip(tmpPath)
	} else {
		binaryData, err = extractTarGz(tmpPath)
	}
	if err != nil {
		return err
	}

	// 替换当前二进制
	return replaceBinary(execPath, binaryData)
}

// extractTarGz 从 tar.gz 中提取 sec 二进制
func extractTarGz(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		// 找到 sec 二进制（不在子目录中，或路径名为 "sec"）
		name := filepath.Base(hdr.Name)
		if name == "sec" || name == "sec.exe" {
			if hdr.Typeflag == tar.TypeReg {
				if hdr.Size > maxBinarySize {
					return nil, fmt.Errorf("binary size %d exceeds maximum %d", hdr.Size, maxBinarySize)
				}
				data, err := io.ReadAll(tr)
				if err != nil {
					return nil, err
				}
				if len(data) == 0 {
					return nil, errors.New("binary in archive is empty")
				}
				return data, nil
			}
		}
	}

	return nil, errors.New("binary not found in archive")
}

// extractZip 从 zip 中提取 sec.exe 二进制
func extractZip(path string) ([]byte, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer r.Close()

	for _, f := range r.File {
		name := filepath.Base(f.Name)
		if name == "sec" || name == "sec.exe" {
			if f.UncompressedSize64 > maxBinarySize {
				return nil, fmt.Errorf("binary size %d exceeds maximum %d", f.UncompressedSize64, maxBinarySize)
			}

			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()

			data, err := io.ReadAll(rc)
			if err != nil {
				return nil, err
			}
			if len(data) == 0 {
				return nil, errors.New("binary in archive is empty")
			}
			return data, nil
		}
	}

	return nil, errors.New("binary not found in archive")
}

// replaceBinary 替换当前运行的二进制文件
func replaceBinary(execPath string, data []byte) error {
	if runtime.GOOS == "windows" {
		return replaceBinaryWindows(execPath, data)
	}

	// 将新二进制写入系统临时目录
	tmpFile, err := os.CreateTemp("", ".sec-new-*")
	if err != nil {
		return err
	}
	defer func() {
		closeErr := tmpFile.Close()
		if closeErr != nil {
			slog.Error("replaceBinary failed to close tmpFile", "error", closeErr)
		}
	}()

	tmpPath := tmpFile.Name()
	removeTmp := true // true defer 中删除 tmpPath
	defer func() {
		if !removeTmp {
			return
		}
		removeErr := os.Remove(tmpPath)
		if removeErr != nil {
			slog.Error("replaceBinary failed to remove tmpPath", "tmpPath", tmpPath, "error", removeErr)
		}
	}()

	n, err := tmpFile.Write(data)
	if err != nil {
		return err
	}
	if n != len(data) {
		return fmt.Errorf("short write: wrote %d of %d bytes", n, len(data))
	}

	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return err
	}
	// os.Rename 在同设备上是原子的，跨设备时回退到 copy
	if err := os.Rename(tmpPath, execPath); err != nil {
		return copyFile(tmpPath, execPath)
	}
	// os.Rename 执行成功，tmpPath 已经被 mv 到 execPath
	// tmpPath 不存在，defer 中不再执行删除目录操作
	removeTmp = false

	return nil
}

// replaceBinaryWindows Windows 下通过批处理脚本延迟替换
func replaceBinaryWindows(execPath string, data []byte) error {
	newPath := execPath + ".new"
	if err := os.WriteFile(newPath, data, 0o755); err != nil {
		return err
	}

	// 创建延迟替换脚本
	scriptPath := execPath + ".upgrade.bat"
	script := fmt.Sprintf(
		"@echo off\r\n"+
			":loop\r\n"+
			"timeout /t 1 /nobreak >nul\r\n"+
			"del /F /Q \"%s\" 2>nul\r\n"+
			"if exist \"%s\" goto loop\r\n"+
			"move /Y \"%s\" \"%s\"\r\n"+
			"del \"%%~f0\"\r\n",
		execPath, execPath, newPath, execPath,
	)

	if err := os.WriteFile(scriptPath, []byte(script), 0o644); err != nil {
		os.Remove(newPath)
		return err
	}
	// 在后台执行脚本，当前进程退出后脚本会完成替换
	cmd := exec.Command("cmd", "/C", scriptPath)
	if err := cmd.Start(); err != nil {
		os.Remove(scriptPath)
		os.Remove(newPath)
		return fmt.Errorf("failed to start upgrade script: %w", err)
	}

	return nil
}

// copyFile 复制文件内容
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}

	if _, err = io.Copy(dstFile, srcFile); err != nil {
		dstFile.Close()
		return err
	}

	return dstFile.Close()
}
