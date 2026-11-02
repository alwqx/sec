package upgrade

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/alwqx/sec/version"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestShouldUpgrade(t *testing.T) {
	for _, tc := range []struct {
		current, latest string
		force, want     bool
		reason          string
	}{
		{"v0.4.2", "v0.4.2", false, false, ""},
		{"v0.4.2", "v0.4.3", false, true, ""},
		{"v0.4.9", "v0.5.0", false, true, ""},
		{"v0.9.0", "v0.10.0", false, true, ""},
		{"v0.5.0-rc1", "v0.5.0", false, true, ""},
		{"v0.5.0", "v0.5.0-rc1", false, false, "更新"},
		{"v0.4.3", "v0.4.2", false, false, "更新"},
		{"v0.4.3-2-gabc123", "v0.4.3", false, false, "开发版本"},
		{"v0.4.3-2-gabc123", "v0.4.4", false, false, "开发版本"},
		{"(dev)", "v0.4.3", false, false, "无法比较"},
		{"abc1234", "v0.4.3", false, false, "无法比较"},
		{"v0.4.3", "nightly", false, false, "无法比较"},
		{"(dev)", "v0.4.3", true, true, ""},
		{"v0.4.3", "v0.4.2", true, true, ""},
		{"v0.4.3-2-gabc123", "v0.4.4", true, true, ""},
		{"v0.4.3", "v0.4.3", true, false, ""},
	} {
		got, err := shouldUpgrade(tc.current, tc.latest, tc.force)
		require.Equal(t, tc.want, got, "%s -> %s", tc.current, tc.latest)
		if tc.reason == "" {
			require.NoError(t, err, "%s -> %s", tc.current, tc.latest)
		} else {
			require.ErrorIs(t, err, ErrNeedForce)
			require.ErrorContains(t, err, tc.reason)
		}
	}
}

func TestUpgradeHandlerRefusalIsAnError(t *testing.T) {
	defer gock.Off()
	previous := version.Version
	defer func() { version.Version = previous }()
	mock := func() {
		gock.New("https://api.github.com").Get("/repos/alwqx/sec/releases/latest").Reply(200).BodyString(`{"tag_name":"v0.4.2"}`)
	}
	run := func() (string, error) {
		cmd := NewUpgradeCLI()
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetArgs(nil)
		cmd.SilenceErrors = true
		cmd.SilenceUsage = true
		err := cmd.Execute()
		return out.String(), err
	}

	version.Version = "v9.9.9"
	mock()
	_, err := run()
	require.ErrorIs(t, err, ErrNeedForce, "脚本要能看出没有升级")

	version.Version = ""
	mock()
	_, err = run()
	require.ErrorIs(t, err, ErrNeedForce)

	version.Version = "v0.4.2"
	mock()
	output, err := run()
	require.NoError(t, err, "已是最新不是失败")
	require.Contains(t, output, "Already up to date.")
}

// 目录不可写时报错，当前程序不能被改动。
func TestReplaceBinaryFailureLeavesBinaryUntouched(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("需要普通用户的目录权限")
	}
	dir := t.TempDir()
	execPath := filepath.Join(dir, "sec")
	require.NoError(t, os.WriteFile(execPath, []byte("old binary"), 0o755))
	require.NoError(t, os.Chmod(dir, 0o555))
	defer os.Chmod(dir, 0o755)
	err := replaceBinary(execPath, []byte("new binary"))
	require.ErrorContains(t, err, "有权限的用户")
	data, readErr := os.ReadFile(execPath)
	require.NoError(t, readErr)
	require.Equal(t, "old binary", string(data))
}

// 目标是目录时 rename 失败，不能回退到覆盖写入，也不留临时文件。
func TestReplaceBinaryRenameFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 通过脚本延迟替换")
	}
	dir := t.TempDir()
	execPath := filepath.Join(dir, "sec")
	require.NoError(t, os.Mkdir(execPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(execPath, "keep"), []byte("x"), 0o644))
	err := replaceBinary(execPath, []byte("new binary"))
	require.ErrorContains(t, err, "当前程序没有改动")
	leftovers, globErr := filepath.Glob(filepath.Join(dir, ".sec-new-*"))
	require.NoError(t, globErr)
	require.Empty(t, leftovers)
}

func TestVerifyDigest(t *testing.T) {
	sum := sha256.Sum256([]byte("data"))
	good := hex.EncodeToString(sum[:])
	require.NoError(t, verifyDigest("sha256:"+good, sum[:]))
	require.ErrorContains(t, verifyDigest("sha256:00", sum[:]), "校验失败")
	for _, missing := range []string{"", "sha256:", "md5:abc", good} {
		require.ErrorContains(t, verifyDigest(missing, sum[:]), "拒绝安装", missing)
	}
}

func tarGz(t *testing.T, name string, data []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gz := gzip.NewWriter(&buffer)
	tw := tar.NewWriter(gz)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: "README.md", Mode: 0o644, Size: 2, Typeflag: tar.TypeReg}))
	_, err := tw.Write([]byte("hi"))
	require.NoError(t, err)
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(data)), Typeflag: tar.TypeReg}))
	_, err = tw.Write(data)
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return buffer.Bytes()
}

func digestOf(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func TestDownloadAndReplace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 通过脚本延迟替换")
	}
	defer gock.Off()
	archive := tarGz(t, "sec", []byte("new binary"))
	const url = "https://example.test/sec-v1.0.0-linux-amd64.tar.gz"
	execPath := filepath.Join(t.TempDir(), "sec")
	require.NoError(t, os.WriteFile(execPath, []byte("old binary"), 0o755))
	read := func() string {
		data, err := os.ReadFile(execPath)
		require.NoError(t, err)
		return string(data)
	}
	mock := func() { gock.New("https://example.test").Get("/sec-v1.0.0").Reply(200).BodyString(string(archive)) }

	// 校验不通过或没有校验值时，当前程序不能被改动。
	mock()
	err := downloadAndReplace(context.Background(), &asset{Name: "sec-v1.0.0-linux-amd64.tar.gz", BrowserDownloadURL: url, Digest: digestOf([]byte("other"))}, execPath)
	require.ErrorContains(t, err, "校验失败")
	require.Equal(t, "old binary", read())

	mock()
	err = downloadAndReplace(context.Background(), &asset{Name: "sec-v1.0.0-linux-amd64.tar.gz", BrowserDownloadURL: url}, execPath)
	require.ErrorContains(t, err, "拒绝安装")
	require.Equal(t, "old binary", read())

	gock.New("https://example.test").Get("/sec-v1.0.0").Reply(404)
	err = downloadAndReplace(context.Background(), &asset{Name: "sec-v1.0.0-linux-amd64.tar.gz", BrowserDownloadURL: url, Digest: digestOf(archive)}, execPath)
	require.ErrorContains(t, err, "404")
	require.Equal(t, "old binary", read())

	mock()
	require.NoError(t, downloadAndReplace(context.Background(), &asset{Name: "sec-v1.0.0-linux-amd64.tar.gz", BrowserDownloadURL: url, Digest: digestOf(archive)}, execPath))
	require.Equal(t, "new binary", read())
	info, err := os.Stat(execPath)
	require.NoError(t, err)
	require.NotZero(t, info.Mode()&0o100, "新程序可执行")
	leftovers, err := filepath.Glob(filepath.Join(filepath.Dir(execPath), ".sec-new-*"))
	require.NoError(t, err)
	require.Empty(t, leftovers, "不留临时文件")
}

func TestExtractArchives(t *testing.T) {
	dir := t.TempDir()
	tarPath := filepath.Join(dir, "a.tar.gz")
	require.NoError(t, os.WriteFile(tarPath, tarGz(t, "dist/sec", []byte("bin")), 0o644))
	data, err := extractTarGz(tarPath)
	require.NoError(t, err)
	require.Equal(t, "bin", string(data))

	require.NoError(t, os.WriteFile(tarPath, tarGz(t, "other", []byte("bin")), 0o644))
	_, err = extractTarGz(tarPath)
	require.ErrorContains(t, err, "not found")

	require.NoError(t, os.WriteFile(tarPath, tarGz(t, "sec", nil), 0o644))
	_, err = extractTarGz(tarPath)
	require.ErrorContains(t, err, "empty")

	var buffer bytes.Buffer
	zw := zip.NewWriter(&buffer)
	file, err := zw.Create("sec.exe")
	require.NoError(t, err)
	_, err = file.Write([]byte("exe"))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	zipPath := filepath.Join(dir, "a.zip")
	require.NoError(t, os.WriteFile(zipPath, buffer.Bytes(), 0o644))
	data, err = extractZip(zipPath)
	require.NoError(t, err)
	require.Equal(t, "exe", string(data))
}

func TestFindAsset(t *testing.T) {
	suffix := "-" + runtime.GOOS + "-" + runtime.GOARCH + ".tar.gz"
	if runtime.GOOS == "windows" {
		suffix = "-" + runtime.GOOS + "-" + runtime.GOARCH + ".zip"
	}
	info := &releaseInfo{Assets: []asset{{Name: "sec-v1-other-os.tar.gz"}, {Name: "sec-v1" + suffix + ".md5"}, {Name: "sec-v1" + suffix}}}
	found, err := findAsset(info)
	require.NoError(t, err)
	require.Equal(t, "sec-v1"+suffix, found.Name, "不能选中 .md5 文件")
	_, err = findAsset(&releaseInfo{})
	require.Error(t, err)
}
