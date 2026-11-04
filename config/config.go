// Package config 是 CLI 和 Desktop 共用的配置，保存在 ~/.sec/config.json。读写都加文件锁，两边同时修改也不会写坏文件。
package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alwqx/sec/utils"
	"github.com/alwqx/sec/watchlist"
)

// DefaultSECEmail 是没有配置时使用的 SEC EDGAR 联系邮箱。
const DefaultSECEmail = "kenan3015@gmail.com"

// ErrInvalid 表示配置文件内容无效。读取时返回默认配置和这个错误，调用方可以提示后继续用默认值；
// 修改配置时直接覆盖无效内容，不需要手动编辑文件。
var ErrInvalid = errors.New("配置文件无效")

// Config 是全部配置项。
type Config struct {
	// SECEmail 是访问 SEC EDGAR 时写在 User-Agent 里的联系邮箱，SEC 要求自动程序提供，否则返回 403。
	SECEmail string `json:"sec_edgar_email"`
}

// Default 返回默认配置。
func Default() Config { return Config{SECEmail: DefaultSECEmail} }

// Path 返回配置文件路径。
func Path() (string, error) {
	dir, err := utils.SecDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// ValidateEmail 检查联系邮箱：必须是单个普通邮箱地址，不能带名字或多余字符。
func ValidateEmail(email string) error {
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || address.Name != "" || len(email) > 254 || !strings.Contains(email[strings.LastIndex(email, "@")+1:], ".") {
		return fmt.Errorf("SEC 联系邮箱无效：%q，须为 name@example.com 这样的地址", email)
	}
	return nil
}

func (c Config) validate() error { return ValidateEmail(c.SECEmail) }

// Load 读取配置，文件不存在或某项为空时使用默认值。
func Load(ctx context.Context) (Config, error) {
	path, err := Path()
	if err != nil {
		return Default(), err
	}
	return LoadFile(ctx, path)
}

// Save 保存配置，保存前校验。
func Save(ctx context.Context, cfg Config) error {
	path, err := Path()
	if err != nil {
		return err
	}
	return SaveFile(ctx, path, cfg)
}

// LoadFile 从指定文件读取配置。内容无效时返回默认配置和 ErrInvalid。
func LoadFile(ctx context.Context, path string) (Config, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	unlock, err := watchlist.FileLock(ctx, path)
	if err != nil {
		return Default(), err
	}
	defer unlock()
	return read(path)
}

// read 在持有文件锁时读取配置。
func read(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Default(), fmt.Errorf("%w：%s 不是有效的 JSON，已使用默认配置：%v", ErrInvalid, path, err)
	}
	if cfg.SECEmail == "" {
		cfg.SECEmail = DefaultSECEmail
	}
	if err := cfg.validate(); err != nil {
		return Default(), fmt.Errorf("%w：%s，已使用默认配置：%v", ErrInvalid, path, err)
	}
	return cfg, nil
}

// SaveFile 把配置写入指定文件。
func SaveFile(ctx context.Context, path string, cfg Config) error {
	return UpdateFile(ctx, path, func(c *Config) error {
		*c = cfg
		return nil
	})
}

// UpdateFile 在同一把文件锁里读取、修改并保存配置，CLI 和 Desktop 同时修改时不会互相覆盖。
// 原文件内容无效时从默认配置开始修改。写入时先写临时文件再改名，中途失败不会留下写了一半的文件。
func UpdateFile(ctx context.Context, path string, change func(*Config) error) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	unlock, err := watchlist.FileLock(ctx, path)
	if err != nil {
		return err
	}
	defer unlock()
	cfg, err := read(path)
	if err != nil && !errors.Is(err, ErrInvalid) {
		return err
	}
	if err := change(&cfg); err != nil {
		return err
	}
	if err := cfg.validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return watchlist.AtomicWrite(path, append(data, '\n'))
}

// Update 修改默认位置的配置，见 UpdateFile。
func Update(ctx context.Context, change func(*Config) error) error {
	path, err := Path()
	if err != nil {
		return err
	}
	return UpdateFile(ctx, path, change)
}
