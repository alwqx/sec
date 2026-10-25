// Package watchlist 为 CLI 与 Desktop 提供共享自选存储和增删规则。
package watchlist

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/alwqx/sec/provider/sina"
	"github.com/alwqx/sec/types"
	"github.com/alwqx/sec/utils"
	"github.com/gofrs/flock"
)

const Limit = 500

var ErrInvalid = errors.New("自选证券无效")
var ErrLimit = errors.New("已达到自选数量上限")

type Item struct {
	Code         string             `json:"code"`
	ExCode       string             `json:"excode"`
	Name         string             `json:"name"`
	AddedAt      string             `json:"added_at"`
	Exchange     string             `json:"exchange"`
	SecurityType types.SecurityType `json:"security_type"`
}

type Store struct{ Path, WorkspacePath string }
type document struct {
	Version int    `json:"version"`
	Items   []Item `json:"items"`
}

func Default() (Store, error) {
	dir, err := utils.SecDir()
	if err != nil {
		return Store{}, err
	}
	return Store{Path: filepath.Join(dir, "watchlist.json"), WorkspacePath: filepath.Join(dir, "desktop", "workspace.json")}, nil
}

// FileLock 使用独立锁文件；进程退出自动释放操作系统锁，不删除锁文件以免锁住不同 inode。
func FileLock(ctx context.Context, path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	lock := flock.New(path + ".lock")
	ok, err := lock.TryLockContext(ctx, 25*time.Millisecond)
	if err != nil {
		_ = lock.Close()
		return nil, err
	}
	if !ok {
		_ = lock.Close()
		return nil, ctx.Err()
	}
	return func() { _ = lock.Unlock(); _ = lock.Close() }, nil
}

func AtomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".sec-write-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if _, err = file.Write(data); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

var identifier = regexp.MustCompile(`^[A-Za-z0-9$._-]{1,40}$`)

func normalize(item Item) (Item, error) {
	item.ExCode = strings.ToUpper(strings.TrimSpace(item.ExCode))
	item.Code = strings.TrimSpace(item.Code)
	item.Name = strings.TrimSpace(item.Name)
	if !identifier.MatchString(item.ExCode) || !identifier.MatchString(item.Code) || item.Name == "" || len(item.Name) > 200 {
		return item, fmt.Errorf("%w：代码或名称无效", ErrInvalid)
	}
	switch {
	case strings.HasPrefix(item.ExCode, "SH"):
		item.Exchange = types.ExChangeSse
	case strings.HasPrefix(item.ExCode, "SZ"):
		item.Exchange = types.ExChangeSzse
	case strings.HasPrefix(item.ExCode, "BJ"):
		item.Exchange = types.ExChangeBse
	case strings.HasPrefix(item.ExCode, "HK"):
		item.Exchange = types.ExChangeHKex
	case strings.HasPrefix(item.ExCode, "OF"):
		item.Exchange = "of"
	case strings.HasPrefix(item.ExCode, "$"):
		if item.Exchange != types.ExChangeNyse {
			item.Exchange = types.ExChangeNasdaq
		}
	default:
		return item, fmt.Errorf("%w：不支持的证券标识", ErrInvalid)
	}
	prefix := 2
	if strings.HasPrefix(item.ExCode, "$") {
		prefix = 1
	}
	if !strings.EqualFold(item.Code, item.ExCode[prefix:]) {
		return item, fmt.Errorf("%w：代码与交易所标识不一致", ErrInvalid)
	}
	item.Code = item.ExCode[prefix:]
	if item.SecurityType == "" {
		item.SecurityType = types.SecurityTypeStock
		if strings.HasPrefix(item.ExCode, "OF") || strings.HasPrefix(item.ExCode, "SH5") || strings.HasPrefix(item.ExCode, "SZ15") || strings.HasPrefix(item.ExCode, "SZ16") {
			item.SecurityType = types.SecurityTypeFund
		}
	}
	if item.SecurityType != types.SecurityTypeStock && item.SecurityType != types.SecurityTypeFund {
		return item, fmt.Errorf("%w：证券类型无效", ErrInvalid)
	}
	return item, nil
}

func FromSecurity(sec sina.BasicSecurity) Item {
	return Item{Code: sec.Code, ExCode: sec.ExCode, Name: sec.Name, Exchange: sec.ExChange, SecurityType: sec.SecurityType}
}
func (i Item) Security() sina.BasicSecurity {
	return sina.BasicSecurity{Code: i.Code, ExCode: i.ExCode, Name: i.Name, ExChange: i.Exchange, SecurityType: i.SecurityType}
}

func backup(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".pre-shared-*.bak")
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err = file.Write(data); err != nil {
		return err
	}
	return file.Sync()
}

// readLocked 在共享锁下合并旧 CLI 数组和 Desktop 列表。版本化文档充当一次性迁移标记。
// 先提交共享文档，再清除旧列表；中断后按标记仅清理，删除的证券不会被重新导入。
func (s Store) readLocked(ctx context.Context) ([]Item, error) {
	data, err := os.ReadFile(s.Path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	exists := err == nil
	doc := document{Items: []Item{}}
	migrated := false
	if exists {
		if json.Unmarshal(data, &doc) == nil && doc.Version == 1 {
			migrated = true
		} else {
			if err = json.Unmarshal(data, &doc.Items); err != nil {
				return nil, fmt.Errorf("读取自选文件失败: %w", err)
			}
		}
	}
	for index, item := range doc.Items {
		normalized, err := normalize(item)
		if err != nil {
			return nil, err
		}
		doc.Items[index] = normalized
	}
	var unlock func()
	var old []byte
	var workspace map[string]json.RawMessage
	if s.WorkspacePath != "" {
		unlock, err = FileLock(ctx, s.WorkspacePath)
		if err != nil {
			return nil, err
		}
		defer unlock()
		old, err = os.ReadFile(s.WorkspacePath)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if err == nil {
			if err = json.Unmarshal(old, &workspace); err != nil {
				return nil, fmt.Errorf("读取旧 Desktop 自选失败: %w", err)
			}
		}
	}
	if !migrated {
		var legacy []sina.BasicSecurity
		if raw := workspace["watchlist"]; raw != nil {
			if err = json.Unmarshal(raw, &legacy); err != nil {
				return nil, err
			}
		}
		indices := map[string]int{}
		merged := []Item{}
		for _, item := range doc.Items {
			if _, found := indices[item.ExCode]; !found {
				indices[item.ExCode] = len(merged)
				merged = append(merged, item)
			}
		}
		for _, sec := range legacy {
			item, err := normalize(FromSecurity(sec))
			if err != nil {
				return nil, err
			}
			if index, found := indices[item.ExCode]; found {
				item.AddedAt = merged[index].AddedAt
				merged[index] = item
			} else {
				indices[item.ExCode] = len(merged)
				merged = append(merged, item)
			}
		}
		// 迁移不截断历史记录；超过上限时仍允许删除，禁止继续新增。
		if exists {
			if err = backup(s.Path, data); err != nil {
				return nil, err
			}
		}
		if len(old) > 0 && workspace["watchlist"] != nil {
			if err = backup(s.WorkspacePath, old); err != nil {
				return nil, err
			}
		}
		if err = s.write(merged); err != nil {
			return nil, err
		}
		doc.Items = merged
	}
	if workspace["watchlist"] != nil {
		delete(workspace, "watchlist")
		clean, err := json.MarshalIndent(workspace, "", "  ")
		if err != nil {
			return nil, err
		}
		if err = AtomicWrite(s.WorkspacePath, clean); err != nil {
			return nil, err
		}
	}
	if doc.Items == nil {
		doc.Items = []Item{}
	}
	return doc.Items, nil
}
func (s Store) write(items []Item) error {
	if items == nil {
		items = []Item{}
	}
	data, err := json.MarshalIndent(document{Version: 1, Items: items}, "", "  ")
	if err != nil {
		return err
	}
	return AtomicWrite(s.Path, data)
}
func (s Store) transaction(ctx context.Context, update func([]Item) ([]Item, error)) ([]Item, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	unlock, err := FileLock(ctx, s.Path)
	if err != nil {
		return nil, err
	}
	defer unlock()
	items, err := s.readLocked(ctx)
	if err != nil {
		return nil, err
	}
	if update == nil {
		return items, nil
	}
	items, err = update(items)
	if err != nil {
		return nil, err
	}
	if err = s.write(items); err != nil {
		return nil, err
	}
	return items, nil
}
func (s Store) List(ctx context.Context) ([]Item, error) { return s.transaction(ctx, nil) }
func (s Store) Add(ctx context.Context, security sina.BasicSecurity) ([]Item, bool, error) {
	item, err := normalize(FromSecurity(security))
	if err != nil {
		return nil, false, err
	}
	added := false
	items, err := s.transaction(ctx, func(items []Item) ([]Item, error) {
		for _, existing := range items {
			if existing.ExCode == item.ExCode {
				return items, nil
			}
		}
		if len(items) >= Limit {
			return nil, fmt.Errorf("%w，最多保存 %d 只", ErrLimit, Limit)
		}
		item.AddedAt = time.Now().Format("2006-01-02")
		added = true
		return append(items, item), nil
	})
	return items, added, err
}
func (s Store) Remove(ctx context.Context, keys ...string) ([]Item, []Item, error) {
	removed := []Item{}
	targets := map[string]bool{}
	for _, key := range keys {
		targets[strings.ToUpper(strings.TrimSpace(key))] = true
	}
	items, err := s.transaction(ctx, func(items []Item) ([]Item, error) {
		kept := []Item{}
		for _, item := range items {
			if targets[item.ExCode] || targets[item.Code] {
				removed = append(removed, item)
			} else {
				kept = append(kept, item)
			}
		}
		return kept, nil
	})
	return items, removed, err
}
