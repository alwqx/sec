package config

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newSecret(t *testing.T) *AISecret {
	t.Helper()
	return NewAISecret(filepath.Join(t.TempDir(), ".sec", "secret.key"))
}

func TestAISecretRoundTrip(t *testing.T) {
	secret := newSecret(t)
	_, err := secret.Decrypt("ds", "enc:v1:AAAA")
	require.ErrorIs(t, err, ErrSecretMissing, "还没有密钥")

	first, err := secret.Encrypt("ds", "sk-test-1234567890")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(first, aiKeyPrefix))
	require.NotContains(t, first, "sk-test")
	info, err := os.Stat(secret.Path())
	require.NoError(t, err, "第一次加密时生成密钥")
	require.EqualValues(t, secretKeySize, info.Size())
	if runtime.GOOS != "windows" {
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}

	second, err := secret.Encrypt("ds", "sk-test-1234567890")
	require.NoError(t, err)
	require.NotEqual(t, first, second, "每次用新的随机 nonce")
	for _, value := range []string{first, second} {
		plain, err := secret.Decrypt("ds", value)
		require.NoError(t, err)
		require.Equal(t, "sk-test-1234567890", plain)
	}

	// 复制到别的配置下、改动密文、格式不对都无法解密。
	_, err = secret.Decrypt("other", first)
	require.ErrorIs(t, err, ErrKeyUndecryptable, "配置名是附加认证数据")
	sealed, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(first, aiKeyPrefix))
	require.NoError(t, err)
	sealed[len(sealed)-1] ^= 1
	_, err = secret.Decrypt("ds", aiKeyPrefix+base64.StdEncoding.EncodeToString(sealed))
	require.ErrorIs(t, err, ErrKeyUndecryptable)
	for _, bad := range []string{"sk-plain", "enc:v1:!!!", "enc:v1:AAAA", "enc:v2:" + strings.TrimPrefix(first, aiKeyPrefix)} {
		_, err = secret.Decrypt("ds", bad)
		require.ErrorIs(t, err, ErrKeyUndecryptable, bad)
	}

	// 换了密钥（比如换机器后重新生成）：旧密文无法解密，重新设置后可以。
	require.NoError(t, os.Remove(secret.Path()))
	_, err = secret.Decrypt("ds", first)
	require.ErrorIs(t, err, ErrSecretMissing)
	third, err := secret.Encrypt("ds", "sk-new-1234567890")
	require.NoError(t, err)
	_, err = secret.Decrypt("ds", first)
	require.ErrorIs(t, err, ErrKeyUndecryptable)
	plain, err := secret.Decrypt("ds", third)
	require.NoError(t, err)
	require.Equal(t, "sk-new-1234567890", plain)
}

func TestAISecretFileChecks(t *testing.T) {
	secret := newSecret(t)
	_, err := secret.Encrypt("ds", "k")
	require.NoError(t, err)

	if runtime.GOOS != "windows" {
		require.NoError(t, os.Chmod(secret.Path(), 0o644))
		_, err = secret.Encrypt("ds", "k")
		require.ErrorContains(t, err, "权限过宽")
		require.ErrorContains(t, err, "chmod 600")
		require.NoError(t, os.Chmod(secret.Path(), 0o600))
	}

	if runtime.GOOS != "windows" {
		require.NoError(t, os.Chmod(secret.Path(), 0o644))
		_, err = secret.Decrypt("ds", "enc:v1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
		require.ErrorIs(t, err, ErrSecretPermissions)
		require.NoError(t, os.Chmod(secret.Path(), 0o600))
	}

	// 已损坏：解密报错；重新设置 Key 时生成新密钥替换，之后照常加解密。
	require.NoError(t, os.WriteFile(secret.Path(), []byte("short"), 0o600))
	_, err = secret.Decrypt("ds", "enc:v1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA")
	require.ErrorIs(t, err, ErrSecretCorrupt)
	value, err := secret.Encrypt("ds", "sk-replaced-123456")
	require.NoError(t, err)
	plain, err := secret.Decrypt("ds", value)
	require.NoError(t, err)
	require.Equal(t, "sk-replaced-123456", plain)
	info, err := os.Stat(secret.Path())
	require.NoError(t, err)
	require.EqualValues(t, secretKeySize, info.Size())
	if runtime.GOOS != "windows" {
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	}
}

func TestAISecretWithoutHardLinks(t *testing.T) {
	// exFAT、部分网络盘不支持硬链接：改用 O_EXCL 直接创建，仍不覆盖已有文件。
	original := linkFile
	t.Cleanup(func() { linkFile = original })
	linkFile = func(string, string) error { return &os.LinkError{Op: "link", Err: syscall.ENOTSUP} }

	secret := newSecret(t)
	value, err := secret.Encrypt("ds", "sk-nolink-123456")
	require.NoError(t, err)
	plain, err := secret.Decrypt("ds", value)
	require.NoError(t, err)
	require.Equal(t, "sk-nolink-123456", plain)
	before, err := os.ReadFile(secret.Path())
	require.NoError(t, err)
	require.NoError(t, NewAISecret(secret.Path()).create(false))
	after, err := os.ReadFile(secret.Path())
	require.NoError(t, err)
	require.Equal(t, before, after, "已有密钥时不覆盖")
}

func TestAISecretStaleTemps(t *testing.T) {
	// 中途退出留下的临时密钥文件：超过 1 分钟的删掉，更新的可能是另一个进程正在写，保留。
	secret := newSecret(t)
	dir := filepath.Dir(secret.Path())
	require.NoError(t, os.MkdirAll(dir, 0o700))
	stale, fresh := filepath.Join(dir, ".secret-stale"), filepath.Join(dir, ".secret-fresh")
	for _, path := range []string{stale, fresh} {
		require.NoError(t, os.WriteFile(path, make([]byte, secretKeySize), 0o600))
	}
	old := time.Now().Add(-2 * staleTemp)
	require.NoError(t, os.Chtimes(stale, old, old))
	_, err := secret.Encrypt("ds", "k")
	require.NoError(t, err)
	_, err = os.Stat(stale)
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(fresh)
	require.NoError(t, err)
}

func TestAISecretConcurrentCreate(t *testing.T) {
	// CLI 和 Desktop 同时第一次保存 Key：只生成一个密钥，双方的密文都能解密。
	secret := newSecret(t)
	values, errs := make([]string, 8), make([]error, 8)
	var wg sync.WaitGroup
	for i := range values {
		wg.Add(1)
		go func() {
			defer wg.Done()
			values[i], errs[i] = NewAISecret(secret.Path()).Encrypt("ds", "sk-concurrent-123456")
		}()
	}
	wg.Wait()
	// testing 不允许在其他 goroutine 里 FailNow，等结束后再检查。
	for _, err := range errs {
		require.NoError(t, err)
	}
	for _, value := range values {
		plain, err := secret.Decrypt("ds", value)
		require.NoError(t, err)
		require.Equal(t, "sk-concurrent-123456", plain)
	}
	entries, err := os.ReadDir(filepath.Dir(secret.Path()))
	require.NoError(t, err)
	require.Len(t, entries, 1, "不留临时文件")
}

func TestAIKeys(t *testing.T) {
	secret := newSecret(t)
	env := map[string]string{}
	getenv := func(name string) string { return env[name] }
	var ai AIConfig
	setKey := func(name, key string) error {
		_, err := ai.SetKey(secret, name, key)
		return err
	}
	require.NoError(t, ai.Add("ds", AIProfile{Provider: "deepseek", Model: "deepseek-chat"}))
	require.NoError(t, ai.Add("local", AIProfile{Provider: "ollama", Model: "qwen3"}))

	key, err := ai.ResolveKey(secret, "ds", getenv)
	require.NoError(t, err)
	require.Equal(t, AIKey{Source: AIKeyNone}, key)
	require.Equal(t, "未设置", ai.KeyStatus(secret, "ds", getenv))
	require.Equal(t, "未设置（本机模型可以不填）", ai.KeyStatus(secret, "local", getenv))

	// 3. 厂商的默认环境变量，OPENAI_API_KEY 不算。
	env["OPENAI_API_KEY"] = "sk-openai-0000000000"
	env["DEEPSEEK_API_KEY"] = "sk-provider-11111111"
	key, err = ai.ResolveKey(secret, "ds", getenv)
	require.NoError(t, err)
	require.Equal(t, AIKey{Value: "sk-provider-11111111", Source: AIKeyFromProviderEnv, Env: "DEEPSEEK_API_KEY"}, key)
	require.Equal(t, "厂商环境变量 DEEPSEEK_API_KEY（末 4 位 1111）", ai.KeyStatus(secret, "ds", getenv))

	// 2. 保存的 Key 优先于厂商环境变量。
	require.ErrorContains(t, setKey("ds", "  "), "不能为空")
	require.ErrorContains(t, setKey("ds", "sk-a sk-b"), "空白字符")
	require.ErrorIs(t, setKey("nosuch", "k"), ErrAIProfileNotFound)
	require.NoError(t, setKey("ds", " sk-stored-22222222\n"))
	require.NoError(t, ai.Validate(), "保存的是密文")
	require.NotContains(t, ai.Profiles["ds"].APIKey, "sk-stored")
	key, err = ai.ResolveKey(secret, "ds", getenv)
	require.NoError(t, err)
	require.Equal(t, AIKey{Value: "sk-stored-22222222", Source: AIKeyStored}, key, "去掉首尾空白")
	require.Equal(t, "加密保存（末 4 位 2222）", ai.KeyStatus(secret, "ds", getenv))

	// 1. api_key_env 优先；指定了却没设置时报错，不往下找。
	require.NoError(t, ai.Edit("ds", func(p *AIProfile) { p.APIKeyEnv = "MY_DS_KEY" }))
	_, err = ai.ResolveKey(secret, "ds", getenv)
	require.ErrorContains(t, err, "从环境变量 MY_DS_KEY 读取 Key，但这个环境变量没有设置")
	require.Contains(t, ai.KeyStatus(secret, "ds", getenv), "没有设置")
	env["MY_DS_KEY"] = "sk-env-333"
	key, err = ai.ResolveKey(secret, "ds", getenv)
	require.NoError(t, err)
	require.Equal(t, AIKey{Value: "sk-env-333", Source: AIKeyFromEnv, Env: "MY_DS_KEY"}, key)
	require.Equal(t, "环境变量 MY_DS_KEY", ai.KeyStatus(secret, "ds", getenv), "太短的 Key 不显示末 4 位")
	env["MY_DS_KEY"] = "sk-env-1234567890中文"
	require.Equal(t, "环境变量 MY_DS_KEY", ai.KeyStatus(secret, "ds", getenv), "含非 ASCII 字符时不显示末 4 位")

	// 设置 Key 时清空 api_key_env，否则保存的 Key 用不上。
	cleared, err := ai.SetKey(secret, "ds", "sk-stored-44444444")
	require.NoError(t, err)
	require.Equal(t, "MY_DS_KEY", cleared)
	require.Empty(t, ai.Profiles["ds"].APIKeyEnv)
	require.Equal(t, "加密保存（末 4 位 4444）", ai.KeyStatus(secret, "ds", getenv))
	cleared, err = ai.SetKey(secret, "ds", "sk-stored-22222222")
	require.NoError(t, err)
	require.Empty(t, cleared)

	// secret.key 有问题时只给简短状态，不带路径。
	if runtime.GOOS != "windows" {
		require.NoError(t, os.Chmod(secret.Path(), 0o644))
		require.Equal(t, "加密保存，secret.key 权限过宽，请改为 600", ai.KeyStatus(secret, "ds", getenv))
		require.NoError(t, os.Chmod(secret.Path(), 0o600))
	}
	data, err := os.ReadFile(secret.Path())
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(secret.Path(), data[:10], 0o600))
	require.Equal(t, "加密保存，secret.key 已损坏，请重新设置", ai.KeyStatus(secret, "ds", getenv))
	require.NotContains(t, ai.KeyStatus(secret, "ds", getenv), secret.Path())

	// 密钥丢失：保存的 Key 无法解密，提示重新设置，不删除密文。
	require.NoError(t, os.Remove(secret.Path()))
	_, err = ai.ResolveKey(secret, "ds", getenv)
	require.ErrorIs(t, err, ErrSecretMissing)
	require.Equal(t, "加密保存，无法解密，请重新设置", ai.KeyStatus(secret, "ds", getenv))
	require.NotEmpty(t, ai.Profiles["ds"].APIKey)

	had, err := ai.UnsetKey("ds")
	require.NoError(t, err)
	require.True(t, had)
	had, err = ai.UnsetKey("ds")
	require.NoError(t, err)
	require.False(t, had)
	require.Empty(t, ai.Profiles["ds"].APIKey)
	_, err = ai.UnsetKey("nosuch")
	require.ErrorIs(t, err, ErrAIProfileNotFound)
}

func TestDescribeKey(t *testing.T) {
	ai := AIConfig{Default: "ds", Profiles: map[string]AIProfile{"ds": {Provider: "deepseek", Model: "m"}, "local": {Provider: "ollama", Model: "m"}}}
	require.Equal(t, "加密保存（末 4 位 2222）", ai.DescribeKey("ds", AIKey{Value: "sk-stored-22222222", Source: AIKeyStored}, nil))
	require.Equal(t, "厂商环境变量 DEEPSEEK_API_KEY（末 4 位 1111）", ai.DescribeKey("ds", AIKey{Value: "sk-provider-11111111", Source: AIKeyFromProviderEnv, Env: "DEEPSEEK_API_KEY"}, nil))
	require.Equal(t, "未设置（本机模型可以不填）", ai.DescribeKey("local", AIKey{Source: AIKeyNone}, nil))
	require.Equal(t, "加密保存，secret.key 已损坏，请重新设置", ai.DescribeKey("ds", AIKey{}, ErrSecretCorrupt))
}
