package federalreserve

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestValidatedCacheRefreshExpiryAndFailurePreservation(t *testing.T) {
	defer gock.Off()
	client := NewClient(t.TempDir())
	validate := func(body []byte) error { _, err := parseTable(body); return err }
	mockTable(t)
	body, fetched, err := client.fetch(context.Background(), openMarketURL, validate)
	require.NoError(t, err)
	path := cachePath(client.CacheDir, openMarketURL)
	stored, err := os.ReadFile(path)
	require.NoError(t, err)
	var cache cachedPage
	require.NoError(t, json.Unmarshal(stored, &cache))
	require.Equal(t, openMarketURL, cache.SourceURL)
	require.Equal(t, string(body), cache.Body)
	_, cachedAt, err := client.fetch(context.Background(), openMarketURL, validate)
	require.NoError(t, err)
	require.True(t, fetched.Equal(cachedAt))
	client.Refresh = true
	gock.New("https://www.federalreserve.gov").Get("/monetarypolicy/openmarket.htm").Reply(200).BodyString("error page")
	_, _, err = client.fetch(context.Background(), openMarketURL, validate)
	require.Error(t, err)
	unchanged, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, stored, unchanged)
	mockTable(t)
	_, _, err = client.fetch(context.Background(), openMarketURL, validate)
	require.NoError(t, err)
	client.Refresh = false
	for _, invalid := range []cachedPage{
		{openMarketURL, time.Now().Add(-6 * time.Hour), string(body)},
		{openMarketURL, time.Now().Add(time.Hour), string(body)},
		{"https://example.com/wrong-source", time.Now(), string(body)},
		{openMarketURL, time.Now(), "error page"},
	} {
		data, err := json.Marshal(invalid)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, data, 0644))
		mockTable(t)
		_, _, err = client.fetch(context.Background(), openMarketURL, validate)
		require.NoError(t, err)
	}
	files, err := filepath.Glob(filepath.Join(client.CacheDir, ".federalreserve-*"))
	require.NoError(t, err)
	require.Empty(t, files)
	require.True(t, gock.IsDone())
}

func TestNoCacheAndCancelledRead(t *testing.T) {
	defer gock.Off()
	client := NewClient("")
	validate := func(body []byte) error { _, err := parseTable(body); return err }
	for i := 0; i < 2; i++ {
		mockTable(t)
		_, _, err := client.fetch(context.Background(), openMarketURL, validate)
		require.NoError(t, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := client.fetch(ctx, openMarketURL, validate)
	require.ErrorIs(t, err, context.Canceled)
	require.True(t, gock.IsDone())
}
