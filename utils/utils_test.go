package utils

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alwqx/sec/version"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestUserAgent(t *testing.T) {
	version.Version = "test"
	require.NotEqual(t, "", UserAgent())
}

func TestMakeRequest(t *testing.T) {
	defer gock.Off()
	ctx := context.TODO()
	gock.New("http://abc.xyz").Get("/foo").Reply(200).JSON(`OK`)
	resp, err := MakeRequest(ctx, http.MethodGet, "http://abc.xyz/foo", nil, nil, 0)
	require.Nil(t, err)
	require.NotNil(t, resp)

	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.Nil(t, err)
	require.Equal(t, "OK", string(body))
}

func TestWriteJson(t *testing.T) {
	tmpDir := os.TempDir()
	t.Cleanup(func() {
		os.RemoveAll(tmpDir)
	})
	// 1. nil
	err := WriteJson(nil, tmpDir)
	require.Nil(t, err)

	// 2 data
	data := struct {
		Name string
	}{
		Name: "TestFile",
	}

	filePath := filepath.Join(tmpDir, "data.json")
	err = WriteJson(data, filePath)
	require.Nil(t, err)
}

func TestParseBeginEnd(t *testing.T) {
	layout20060102 := "20060102"
	layoutYYYYMMDD := "2006-01-02"
	today := time.Now()

	t.Run("defaults", func(t *testing.T) {
		begin, end, err := ParseBeginEnd("", "", 30, layout20060102, layout20060102)
		require.NoError(t, err)
		require.Equal(t, today.Add(-30*24*time.Hour).Format(layout20060102), begin)
		require.Equal(t, today.Format(layout20060102), end)
	})

	t.Run("defaults with different output layout", func(t *testing.T) {
		begin, end, err := ParseBeginEnd("", "", 30, layout20060102, layoutYYYYMMDD)
		require.NoError(t, err)
		require.Equal(t, today.Add(-30*24*time.Hour).Format(layoutYYYYMMDD), begin)
		require.Equal(t, today.Format(layoutYYYYMMDD), end)
	})

	t.Run("both specified", func(t *testing.T) {
		begin, end, err := ParseBeginEnd("20260101", "20260131", 30, layout20060102, layout20060102)
		require.NoError(t, err)
		require.Equal(t, "20260101", begin)
		require.Equal(t, "20260131", end)
	})

	t.Run("both specified with different output layout", func(t *testing.T) {
		begin, end, err := ParseBeginEnd("20260101", "20260131", 30, layout20060102, layoutYYYYMMDD)
		require.NoError(t, err)
		require.Equal(t, "2026-01-01", begin)
		require.Equal(t, "2026-01-31", end)
	})

	t.Run("only begin", func(t *testing.T) {
		begin, end, err := ParseBeginEnd("20260101", "", 30, layout20060102, layout20060102)
		require.NoError(t, err)
		require.Equal(t, "20260101", begin)
		require.Equal(t, today.Format(layout20060102), end)
	})

	t.Run("only end", func(t *testing.T) {
		begin, end, err := ParseBeginEnd("", "20261231", 90, layout20060102, layout20060102)
		require.NoError(t, err)
		require.Equal(t, today.Add(-90*24*time.Hour).Format(layout20060102), begin)
		require.Equal(t, "20261231", end)
	})

	t.Run("invalid begin format", func(t *testing.T) {
		_, _, err := ParseBeginEnd("2026-01-01", "", 30, layout20060102, layout20060102)
		require.Error(t, err)
	})

	t.Run("invalid end format", func(t *testing.T) {
		_, _, err := ParseBeginEnd("", "2026-01-31", 30, layout20060102, layout20060102)
		require.Error(t, err)
	})

	t.Run("begin after end", func(t *testing.T) {
		_, _, err := ParseBeginEnd("20261231", "20260101", 30, layout20060102, layout20060102)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid time range")
	})

	t.Run("same day", func(t *testing.T) {
		begin, end, err := ParseBeginEnd("20260115", "20260115", 30, layout20060102, layout20060102)
		require.NoError(t, err)
		require.Equal(t, "20260115", begin)
		require.Equal(t, "20260115", end)
	})

	t.Run("custom default days", func(t *testing.T) {
		begin, end, err := ParseBeginEnd("", "", 7, layout20060102, layout20060102)
		require.NoError(t, err)
		require.Equal(t, today.Add(-7*24*time.Hour).Format(layout20060102), begin)
		require.Equal(t, today.Format(layout20060102), end)
	})
}

func TestSecDir(t *testing.T) {
	dir, err := SecDir("cache")
	require.Nil(t, err)
	require.NotEmpty(t, dir)
	require.Contains(t, dir, ".sec")
	require.Contains(t, dir, "cache")

	info, err := os.Stat(dir)
	require.Nil(t, err)
	require.True(t, info.IsDir())
}
