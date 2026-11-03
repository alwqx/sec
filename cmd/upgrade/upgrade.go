package upgrade

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/alwqx/sec/release"
	"github.com/alwqx/sec/utils"
	"github.com/alwqx/sec/version"
	"github.com/spf13/cobra"
)

const (
	maxBinarySize = 500 * 1024 * 1024 // 500 MB
)

type releaseInfo = release.Info
type asset = release.Asset

func NewUpgradeCLI() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade sec to the latest version from GitHub releases",
		RunE:  UpgradeHandler,
	}
	rootCmd.Flags().BoolP("debug", "D", false, "Enable debug mode")
	rootCmd.Flags().Bool("force", false, "Replace this binary even if it is a development build or newer than the latest release")

	return rootCmd
}

// UpgradeHandler 自动升级到最新版本
func UpgradeHandler(cmd *cobra.Command, args []string) error {
	out := cmd.OutOrStdout()
	currentVersion := version.Version
	if currentVersion == "" {
		currentVersion = "(dev)"
	}

	fmt.Fprintf(out, "Current version: %s\n", currentVersion)
	fmt.Fprintln(out, "Checking latest release...")

	release, err := fetchLatestRelease(cmd.Context())
	if err != nil {
		return fmt.Errorf("failed to fetch latest release: %w", err)
	}

	latestVersion := release.TagName
	fmt.Fprintf(out, "Latest  version: %s\n", latestVersion)

	force, _ := cmd.Flags().GetBool("force")
	proceed, err := shouldUpgrade(currentVersion, latestVersion, force)
	if err != nil {
		return err
	}
	if !proceed {
		fmt.Fprintln(out, "Already up to date.")
		return nil
	}

	// 找到匹配当前系统架构的 asset
	asset, err := findAsset(release)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "Downloading %s (%s)...\n", asset.Name, utils.HumanByte(float64(asset.Size)))

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

// gitDescribe 匹配 git describe 的版本，如 v0.4.3-2-gabc。它是标签之后的开发版本，按语义版本却会排在标签之前。
var gitDescribe = regexp.MustCompile(`-[0-9]+-g[0-9a-f]+$`)

// ErrNeedForce 表示需要 --force 才能替换，当前程序没有被改动。
var ErrNeedForce = errors.New("未升级")

// shouldUpgrade 判断是否替换当前程序。只升不降；开发版本无法比较，需要 force。
// 已是最新时返回 false 和 nil，拒绝替换时返回错误。
func shouldUpgrade(current, latest string, force bool) (bool, error) {
	if current == latest {
		return false, nil
	}
	if force {
		return true, nil
	}
	if gitDescribe.MatchString(current) {
		return false, fmt.Errorf("%w：当前是开发版本 %s；要替换为 %s 请使用 --force", ErrNeedForce, current, latest)
	}
	order, known := release.Compare(current, latest)
	switch {
	case !known:
		return false, fmt.Errorf("%w：无法比较当前版本 %s 和最新版本 %s，开发版本也是这种情况；要替换请使用 --force", ErrNeedForce, current, latest)
	case order < 0:
		return true, nil
	case order > 0:
		return false, fmt.Errorf("%w：当前版本 %s 比最新版本 %s 更新；要降级请使用 --force", ErrNeedForce, current, latest)
	}
	return false, nil
}

// verifyDigest 校验下载内容。digest 是 GitHub 对 Release 文件计算的 sha256，没有校验值时拒绝安装。
func verifyDigest(digest string, sum []byte) error {
	want, ok := strings.CutPrefix(digest, "sha256:")
	if !ok || want == "" {
		return fmt.Errorf("发布文件没有 sha256 校验值（%q），拒绝安装", digest)
	}
	if got := hex.EncodeToString(sum); !strings.EqualFold(got, want) {
		return fmt.Errorf("sha256 校验失败，文件可能已损坏或被篡改：期望 %s，实际 %s", want, got)
	}
	return nil
}

// fetchLatestRelease 获取最新的 release 信息
func fetchLatestRelease(ctx context.Context) (*releaseInfo, error) { return release.FetchLatest(ctx) }

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

	hash := sha256.New()
	if _, err = io.Copy(io.MultiWriter(tmpFile, hash), resp.Body); err != nil {
		return err
	}
	// 校验通过后才解压，更不会替换当前程序。
	if err := verifyDigest(a.Digest, hash.Sum(nil)); err != nil {
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

	// 写到目标目录，同一文件系统上 rename 才是原子的。不回退到覆盖写入：写一半失败会留下损坏的程序。
	dir := filepath.Dir(execPath)
	tmpFile, err := os.CreateTemp(dir, ".sec-new-*")
	if err != nil {
		return fmt.Errorf("无法写入 %s，请用有权限的用户运行，或手动下载: %w", dir, err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath) // rename 成功后文件已不在原路径，这里只清理失败的情况

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		return err
	}
	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, execPath); err != nil {
		return fmt.Errorf("替换 %s 失败，当前程序没有改动，请用有权限的用户运行，或手动下载: %w", execPath, err)
	}
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
