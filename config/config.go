// Package config 是 CLI 和 Desktop 共用的配置，保存在 ~/.sec/config.json。读写都加文件锁，两边同时修改也不会写坏文件。
package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alwqx/sec/utils"
	"github.com/alwqx/sec/watchlist"
)

// DefaultSECEmail 是没有配置时使用的 SEC EDGAR 联系邮箱。
const DefaultSECEmail = "sec_dev@example.com"

// ErrInvalid 表示配置文件内容无效。读取时返回默认配置和这个错误，调用方可以提示后继续用默认值；
// 修改配置时直接覆盖无效内容，不需要手动编辑文件。
var ErrInvalid = errors.New("配置文件无效")

// ErrInvalidAI 表示 ai 段无效，它也是 ErrInvalid。读取时忽略 ai 段（相当于未启用）；修改其他配置时 ai 段按原样写回，
// 要修改 ai 段时返回这个错误，避免丢掉已保存的模型配置和 Key，需要用户先手动修正。
var ErrInvalidAI = fmt.Errorf("%w：ai 段无效", ErrInvalid)

// ErrInvalidJSON 表示整个文件不是有效的 JSON 对象，它也是 ErrInvalid。读取时全部配置（包括 ai 段）都换成默认值，
// 不能当作没有模型配置。
var ErrInvalidJSON = fmt.Errorf("%w：不是有效的 JSON 对象", ErrInvalid)

// Config 是全部配置项。
type Config struct {
	// SECEmail 是访问 SEC EDGAR 时写在 User-Agent 里的联系邮箱，SEC 要求自动程序提供，否则返回 403。
	SECEmail string `json:"sec_edgar_email"`
	// AI 是模型配置，见 AIConfig；没有配置时不写入文件。
	AI AIConfig `json:"ai,omitzero"`
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

func (c Config) validate() error {
	if err := ValidateEmail(c.SECEmail); err != nil {
		return err
	}
	if err := c.AI.Validate(); err != nil {
		return fmt.Errorf("ai 段：%w", err)
	}
	return nil
}

// Redacted 返回去掉 Key 的副本，用于 sec config 显示和接口返回。
func (c Config) Redacted() Config {
	c.AI = c.AI.Redacted()
	return c
}

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

// LoadFile 从指定文件读取配置。不是有效的 JSON 时返回默认配置和 ErrInvalidJSON；
// 某一项无效时只把这一项换成默认值，并返回 ErrInvalid（ai 段无效时是 ErrInvalidAI）。
func LoadFile(ctx context.Context, path string) (Config, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	unlock, err := watchlist.FileLock(ctx, path)
	if err != nil {
		return Default(), err
	}
	defer unlock()
	cfg, _, err := read(path)
	return cfg, err
}

// read 在持有文件锁时读取配置，同时返回文件里 ai 段的原始内容（没有时为 nil），ai 段无效时 UpdateFile 用它原样写回。
func read(path string) (Config, json.RawMessage, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil, nil
	}
	if err != nil {
		return cfg, nil, err
	}
	// ai 段和其他配置分开解析：其他配置的类型不对时只把它们换成默认值，不会连带丢掉 ai 段；
	// ai 段的类型不对（如 "enabled": "yes"）时按 ai 段无效处理，修改时不覆盖文件。
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return Default(), nil, fmt.Errorf("%w（%s），已使用默认配置：%v", ErrInvalidJSON, path, err)
	}
	aiData := takeAI(fields)
	var invalid []error
	rest, err := json.Marshal(fields)
	if err == nil {
		err = json.Unmarshal(rest, &cfg)
	}
	if err != nil {
		cfg = Default()
		invalid = append(invalid, fmt.Errorf("%w：%s 里 ai 段以外的配置格式不对，已使用默认值：%v", ErrInvalid, path, err))
	}
	// 类型错误只说明字段和类型，不含值，可以写进提示。
	var aiErr error
	if aiData != nil {
		aiErr = json.Unmarshal(aiData, &cfg.AI)
	}
	if aiErr == nil {
		aiErr = cfg.AI.Validate()
	}
	if aiErr != nil {
		cfg.AI = AIConfig{}
		invalid = append(invalid, fmt.Errorf("%w：%s，已忽略 ai 段：%v", ErrInvalidAI, path, aiErr))
	}
	if cfg.SECEmail == "" {
		cfg.SECEmail = DefaultSECEmail
	}
	if err := ValidateEmail(cfg.SECEmail); err != nil {
		cfg.SECEmail = DefaultSECEmail
		invalid = append(invalid, fmt.Errorf("%w：%s，SEC 联系邮箱已使用默认值：%v", ErrInvalid, path, err))
	}
	cfg.AI.normalize()
	return cfg, aiData, errors.Join(invalid...)
}

// takeAI 从 fields 里取出并删掉 ai 字段，没有时返回 nil。和 encoding/json 一样不分大小写，"AI" 也算，名字完全一样的优先。
func takeAI(fields map[string]json.RawMessage) json.RawMessage {
	var found json.RawMessage
	for key, value := range fields {
		if strings.EqualFold(key, "ai") {
			if found == nil || key == "ai" {
				found = value
			}
			delete(fields, key)
		}
	}
	return found
}

// SaveFile 用 cfg 整体替换指定文件里的配置，会覆盖其他进程同时做的修改，只用于测试和初始化；
// 修改某一项请用 UpdateFile。
func SaveFile(ctx context.Context, path string, cfg Config) error {
	return UpdateFile(ctx, path, func(c *Config) error {
		*c = cfg
		return nil
	})
}

// UpdateFile 在同一把文件锁里读取、修改并保存配置，CLI 和 Desktop 同时修改时不会互相覆盖。
// 原文件内容无效时从默认值开始修改。ai 段无效时，没有修改 ai 段就照常保存其他配置，ai 段按文件里的原始内容写回；
// 修改了 ai 段时不保存，返回 ErrInvalidAI，避免丢掉已保存的 Key。写入时先写临时文件再改名，中途失败不会留下写了一半的文件。
func UpdateFile(ctx context.Context, path string, change func(*Config) error) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	unlock, err := watchlist.FileLock(ctx, path)
	if err != nil {
		return err
	}
	defer unlock()
	cfg, rawAI, err := read(path)
	invalidAI := errors.Is(err, ErrInvalidAI)
	if err != nil && !errors.Is(err, ErrInvalid) {
		return err
	}
	if err := change(&cfg); err != nil {
		return err
	}
	if invalidAI {
		// 读取时已忽略无效的 ai 段，cfg.AI 是空的；不是空的说明这次修改了 ai 段。
		if !cfg.AI.isZero() {
			return fmt.Errorf("%w；为避免丢掉已保存的模型配置和 Key，没有修改，请先手动修正或删除 ai 段", err)
		}
		if err := ValidateEmail(cfg.SECEmail); err != nil {
			return err
		}
		data, err := marshalWithAI(cfg, rawAI)
		if err != nil {
			return err
		}
		return watchlist.AtomicWrite(path, append(data, '\n'))
	}
	if err := cfg.validate(); err != nil {
		return err
	}
	cfg.AI.normalize()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return watchlist.AtomicWrite(path, append(data, '\n'))
}

// marshalWithAI 编码 cfg，ai 段用文件里的原始内容。
func marshalWithAI(cfg Config, rawAI json.RawMessage) ([]byte, error) {
	data, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	fields["ai"] = rawAI
	return json.MarshalIndent(fields, "", "  ")
}

// Update 修改默认位置的配置，见 UpdateFile。
func Update(ctx context.Context, change func(*Config) error) error {
	path, err := Path()
	if err != nil {
		return err
	}
	return UpdateFile(ctx, path, change)
}

// LoadOrWarn 读取配置，供命令行使用：文件内容无效时把原因写到 warn，并使用默认值。
func LoadOrWarn(ctx context.Context, warn io.Writer) (Config, error) {
	cfg, err := Load(ctx)
	if errors.Is(err, ErrInvalid) {
		fmt.Fprintln(warn, err)
		return cfg, nil
	}
	return cfg, err
}
