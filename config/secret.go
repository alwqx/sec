package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/alwqx/sec/utils"
)

// secretKeySize 是 AES-256 的密钥长度。
const secretKeySize = 32

var (
	// ErrSecretMissing 表示 secret.key 不存在：被删了或换了机器，已保存的 Key 无法解密，需要重新设置。
	ErrSecretMissing = errors.New("加密密钥 secret.key 不存在，已保存的 API Key 无法解密，请重新设置 Key")
	// ErrKeyUndecryptable 表示保存的 Key 无法解密：secret.key 换过，或密文被改动、复制到了别的配置下。
	ErrKeyUndecryptable = errors.New("API Key 无法解密，请重新设置")
	// ErrSecretPermissions 表示 secret.key 的权限过宽，组或其他用户可以读取。
	ErrSecretPermissions = errors.New("加密密钥 secret.key 的权限过宽")
	// ErrSecretCorrupt 表示 secret.key 已损坏，大小不对。
	ErrSecretCorrupt = errors.New("加密密钥 secret.key 已损坏")
)

// AISecret 是加密 API Key 用的密钥，保存在 ~/.sec/secret.key，和 config.json 分开存放，所有配置共用。
//
// 它防的是 config.json 单独泄露（贴到 issue、同步 dotfiles、备份到网盘），不防能读取用户主目录的本机程序，
// 那样的程序能同时读到 secret.key。见 docs/ai.md 的 API Key 加密。
type AISecret struct{ path string }

// NewAISecret 返回使用指定文件的密钥，测试时用临时目录。
func NewAISecret(path string) *AISecret { return &AISecret{path: path} }

// DefaultAISecret 返回 ~/.sec/secret.key。
func DefaultAISecret() (*AISecret, error) {
	dir, err := utils.SecDir()
	if err != nil {
		return nil, err
	}
	return NewAISecret(filepath.Join(dir, "secret.key")), nil
}

// Path 返回密钥文件路径。
func (s *AISecret) Path() string { return s.path }

// load 读取密钥。文件不存在时返回 ErrSecretMissing。create 为 true（保存 Key）时，文件不存在就生成，
// 已损坏就生成新密钥替换：损坏的密钥解不开任何 Key，替换不会丢东西。权限过宽时不替换，要用户自己修正。
func (s *AISecret) load(create bool) ([]byte, error) {
	key, err := s.read()
	if create && (errors.Is(err, fs.ErrNotExist) || errors.Is(err, ErrSecretCorrupt)) {
		if err := s.create(errors.Is(err, ErrSecretCorrupt)); err != nil {
			return nil, err
		}
		key, err = s.read()
	}
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrSecretMissing
	}
	return key, err
}

func (s *AISecret) read() ([]byte, error) {
	info, err := os.Stat(s.path)
	if err != nil {
		return nil, err
	}
	// Windows 的文件权限位没有意义，靠用户目录本身只对当前用户开放。
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%w：%s 的权限是 %04o，其他用户可以读取，请运行 chmod 600 %s",
			ErrSecretPermissions, s.path, info.Mode().Perm(), s.path)
	}
	key, err := os.ReadFile(s.path)
	if err != nil {
		return nil, err
	}
	if len(key) != secretKeySize {
		return nil, fmt.Errorf("%w：%s 应为 %d 字节，重新设置 Key 时会生成新的密钥", ErrSecretCorrupt, s.path, secretKeySize)
	}
	return key, nil
}

// staleTemp 是残留临时文件的最短存在时间：更新的可能是另一个进程正在生成密钥。
const staleTemp = time.Minute

// create 生成新密钥。replace 为 false 时不覆盖已有文件：先写临时文件再硬链接到目标位置，CLI 和 Desktop 同时生成时只留一个；
// 文件系统不支持硬链接（exFAT、部分网络盘）时改用 O_EXCL 直接创建。replace 为 true 时替换损坏的文件。
func (s *AISecret) create(replace bool) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	removeStaleTemps(dir)
	key := make([]byte, secretKeySize)
	if _, err := rand.Read(key); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".secret-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err := file.Chmod(0o600); err != nil && runtime.GOOS != "windows" {
		return err
	}
	if _, err := file.Write(key); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if replace {
		return os.Rename(file.Name(), s.path)
	}
	err = linkFile(file.Name(), s.path)
	if err == nil || errors.Is(err, fs.ErrExist) {
		return nil
	}
	// 不支持硬链接：O_EXCL 同样不覆盖已有文件，只是另一个进程可能读到写了一半的文件，概率很小，读到时按损坏处理。
	direct, err := os.OpenFile(s.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("保存加密密钥 %s 失败：%w", s.path, err)
	}
	defer direct.Close()
	if _, err := direct.Write(key); err != nil {
		return err
	}
	if err := direct.Sync(); err != nil {
		return err
	}
	return direct.Close()
}

// linkFile 是 os.Link，测试时换成不支持硬链接的情况。
var linkFile = os.Link

// removeStaleTemps 删掉进程中途退出留下的临时密钥文件，它们含有完整的密钥。
func removeStaleTemps(dir string) {
	paths, _ := filepath.Glob(filepath.Join(dir, ".secret-*"))
	for _, path := range paths {
		if info, err := os.Stat(path); err == nil && time.Since(info.ModTime()) > staleTemp {
			os.Remove(path)
		}
	}
}

// Encrypt 加密 Key，配置名作为附加认证数据：密文复制到别的配置下无法解密。没有密钥时先生成。
func (s *AISecret) Encrypt(profile, plaintext string) (string, error) {
	key, err := s.load(true)
	if err != nil {
		return "", err
	}
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), []byte(profile))
	return aiKeyPrefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt 解密 Encrypt 的结果。密钥不存在返回 ErrSecretMissing，密文不对返回 ErrKeyUndecryptable。
func (s *AISecret) Decrypt(profile, value string) (string, error) {
	encoded, ok := strings.CutPrefix(value, aiKeyPrefix)
	if !ok {
		return "", ErrKeyUndecryptable
	}
	sealed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", ErrKeyUndecryptable
	}
	key, err := s.load(false)
	if err != nil {
		return "", err
	}
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	if len(sealed) < gcm.NonceSize()+gcm.Overhead() {
		return "", ErrKeyUndecryptable
	}
	plaintext, err := gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], []byte(profile))
	if err != nil {
		return "", ErrKeyUndecryptable
	}
	return string(plaintext), nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
