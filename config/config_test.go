package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadSave(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "config.json")
	cfg, err := LoadFile(ctx, path)
	require.NoError(t, err)
	require.Equal(t, DefaultSECEmail, cfg.SECEmail, "文件不存在时用默认值")

	require.NoError(t, SaveFile(ctx, path, Config{SECEmail: "me@example.org"}))
	cfg, err = LoadFile(ctx, path)
	require.NoError(t, err)
	require.Equal(t, "me@example.org", cfg.SECEmail)

	require.NoError(t, os.WriteFile(path, []byte(`{"sec_edgar_email":""}`), 0600))
	cfg, err = LoadFile(ctx, path)
	require.NoError(t, err)
	require.Equal(t, DefaultSECEmail, cfg.SECEmail, "为空时用默认值")

	// 内容无效时返回默认配置和 ErrInvalid，修改时直接覆盖。
	for _, bad := range []string{`{`, `{"sec_edgar_email":"me"}`} {
		require.NoError(t, os.WriteFile(path, []byte(bad), 0600))
		cfg, err = LoadFile(ctx, path)
		require.ErrorIs(t, err, ErrInvalid, bad)
		require.Equal(t, DefaultSECEmail, cfg.SECEmail)
		require.NoError(t, UpdateFile(ctx, path, func(c *Config) error { c.SECEmail = "fixed@example.org"; return nil }), bad)
		cfg, err = LoadFile(ctx, path)
		require.NoError(t, err)
		require.Equal(t, "fixed@example.org", cfg.SECEmail)
	}
}

func TestValidateEmail(t *testing.T) {
	for _, email := range []string{"a@b.co", "kenan3015@gmail.com", "first.last+sec@example.org"} {
		require.NoError(t, ValidateEmail(email), email)
	}
	for _, email := range []string{"", "abc", "a@b", "Name <a@b.co>", "a@b.co x", " a@b.co"} {
		require.Error(t, ValidateEmail(email), email)
	}
	require.Error(t, SaveFile(context.Background(), filepath.Join(t.TempDir(), "c.json"), Config{SECEmail: "bad"}))
}
