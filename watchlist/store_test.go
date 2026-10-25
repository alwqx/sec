package watchlist

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/sina"
	"github.com/alwqx/sec/types"
	"github.com/stretchr/testify/require"
)

func testStore(t *testing.T) Store {
	root := t.TempDir()
	return Store{Path: filepath.Join(root, "watchlist.json"), WorkspacePath: filepath.Join(root, "desktop", "workspace.json")}
}
func security(code string) sina.BasicSecurity {
	return sina.BasicSecurity{Code: code, ExCode: "SH" + code, Name: "证券" + code, SecurityType: types.SecurityTypeStock}
}

func TestMergeAndBackupLegacyWatchlists(t *testing.T) {
	s := testStore(t)
	cli := []byte(`[{"code":"600036","excode":"sh600036","name":"招商银行","added_at":"2024-01-01"}]`)
	desktop := []byte(`{"watchlist":[{"code":"600036","ex_code":"SH600036","name":"招商银行","exchange":"sse","security_type":"stock"},{"code":"510300","ex_code":"SH510300","name":"沪深300ETF","exchange":"sse","security_type":"fund"}],"settings":{"theme":"light","refresh_seconds":30,"download_directory":"/tmp/reports"}}`)
	require.NoError(t, os.WriteFile(s.Path, cli, 0600))
	require.NoError(t, os.MkdirAll(filepath.Dir(s.WorkspacePath), 0700))
	require.NoError(t, os.WriteFile(s.WorkspacePath, desktop, 0600))
	items, err := s.List(context.Background())
	require.NoError(t, err)
	require.Len(t, items, 2)
	require.Equal(t, "SH600036", items[0].ExCode)
	require.Equal(t, "2024-01-01", items[0].AddedAt)
	require.Equal(t, types.SecurityTypeFund, items[1].SecurityType)
	var state map[string]json.RawMessage
	data, err := os.ReadFile(s.WorkspacePath)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &state))
	require.NotContains(t, state, "watchlist")
	require.JSONEq(t, `{"theme":"light","refresh_seconds":30,"download_directory":"/tmp/reports"}`, string(state["settings"]))
	for path, want := range map[string][]byte{s.Path: cli, s.WorkspacePath: desktop} {
		backups, err := filepath.Glob(path + ".pre-shared-*.bak")
		require.NoError(t, err)
		require.Len(t, backups, 1)
		data, err := os.ReadFile(backups[0])
		require.NoError(t, err)
		require.Equal(t, want, data)
	}
	_, _, err = s.Remove(context.Background(), "sh600036")
	require.NoError(t, err)
	// 即使中断留下旧列表，迁移标记也阻止已删除证券再次被导入。
	require.NoError(t, os.WriteFile(s.WorkspacePath, desktop, 0600))
	items, err = s.List(context.Background())
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "SH510300", items[0].ExCode)
}

func TestCorruptMigrationPreservesBothSources(t *testing.T) {
	for _, corruptCLI := range []bool{true, false} {
		s := testStore(t)
		cli, desktop := []byte(`[]`), []byte(`{"watchlist":[]}`)
		if corruptCLI {
			cli = []byte(`broken`)
		} else {
			desktop = []byte(`broken`)
		}
		require.NoError(t, os.WriteFile(s.Path, cli, 0600))
		require.NoError(t, os.MkdirAll(filepath.Dir(s.WorkspacePath), 0700))
		require.NoError(t, os.WriteFile(s.WorkspacePath, desktop, 0600))
		_, err := s.List(context.Background())
		require.Error(t, err)
		for path, want := range map[string][]byte{s.Path: cli, s.WorkspacePath: desktop} {
			got, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, want, got)
		}
	}
}

func TestSharedMutationsAndIdentity(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	_, added, err := s.Add(ctx, security("600036"))
	require.NoError(t, err)
	require.True(t, added)
	other := Store{Path: s.Path, WorkspacePath: s.WorkspacePath}
	sec := security("600036")
	sec.ExCode = "sh600036"
	items, added, err := other.Add(ctx, sec)
	require.NoError(t, err)
	require.False(t, added)
	require.Len(t, items, 1)
	sec.ExCode = "SZ600036"
	items, added, err = other.Add(ctx, sec)
	require.NoError(t, err)
	require.True(t, added)
	require.Len(t, items, 2)
	items, removed, err := s.Remove(ctx, "SH600036")
	require.NoError(t, err)
	require.Len(t, removed, 1)
	require.Len(t, items, 1)
	require.Equal(t, "SZ600036", items[0].ExCode)
}

func TestLockCancellation(t *testing.T) {
	s := testStore(t)
	unlock, err := FileLock(context.Background(), s.Path)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err = s.List(ctx)
	require.Error(t, err)
	unlock()
	_, err = s.List(context.Background())
	require.NoError(t, err)
}

func TestMigrationPreservesOverLimitAndAllowsRemoval(t *testing.T) {
	s := testStore(t)
	legacy := make([]Item, 0, Limit+1)
	for index := 0; index <= Limit; index++ {
		legacy = append(legacy, FromSecurity(security(fmt.Sprintf("%06d", 600000+index))))
	}
	data, err := json.Marshal(legacy)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(s.Path, data, 0600))
	items, err := s.List(context.Background())
	require.NoError(t, err)
	require.Len(t, items, Limit+1)
	_, _, err = s.Add(context.Background(), security("688001"))
	require.ErrorIs(t, err, ErrLimit)
	_, _, err = s.Remove(context.Background(), "SH600000", "SH600001")
	require.NoError(t, err)
	items, added, err := s.Add(context.Background(), security("688001"))
	require.NoError(t, err)
	require.True(t, added)
	require.Len(t, items, Limit)
}

func TestWatchProcess(t *testing.T) {
	if os.Getenv("SEC_WATCH_CHILD") != "1" {
		return
	}
	_, _, err := (Store{Path: os.Getenv("SEC_WATCH_PATH")}).Add(context.Background(), security(os.Getenv("SEC_WATCH_CODE")))
	require.NoError(t, err)
}

func TestConcurrentProcessesDoNotLoseUpdates(t *testing.T) {
	s := testStore(t)
	binary, err := os.Executable()
	require.NoError(t, err)
	var group sync.WaitGroup
	for index := 0; index < 6; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			command := exec.Command(binary, "-test.run=^TestWatchProcess$")
			command.Env = append(os.Environ(), "SEC_WATCH_CHILD=1", "SEC_WATCH_PATH="+s.Path, "SEC_WATCH_CODE="+fmt.Sprintf("60000%d", index))
			output, err := command.CombinedOutput()
			if err != nil {
				t.Errorf("child: %v %s", err, output)
			}
		}(index)
	}
	group.Wait()
	items, err := s.List(context.Background())
	require.NoError(t, err)
	require.Len(t, items, 6)
}
