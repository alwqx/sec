package forex

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRequestAndNumberValidation(t *testing.T) {
	for _, req := range []*Request{nil, {Pairs: []string{"USD/CNY"}, Begin: "20240230", End: "20240301"}, {Pairs: []string{"USD/CNH"}, Begin: "20240101", End: "20240102"}, {Pairs: []string{"USD/CNY", "USD/CNY"}, Begin: "20240101", End: "20240102"}, {Pairs: []string{"USD/CNY"}, Begin: "20240102", End: "20240101"}, {Pairs: []string{"USD/CNY"}, Begin: "20050101", End: "20050102"}, {Pairs: []string{"USD/CNY"}, Begin: "20240101", End: time.Now().AddDate(1, 0, 0).Format("20060102")}} {
		require.Error(t, req.Validate(OnshorePairs, "20060104"))
	}
	require.NoError(t, (&Request{Pairs: OnshorePairs, Begin: "20240229", End: "20240229"}).Validate(OnshorePairs, "20060104"))
	for _, raw := range []string{"NaN", "Inf", "0", "-1", "bad"} {
		_, err := Number(raw)
		require.Error(t, err, raw)
	}
	for _, raw := range []string{"", ".", "-", "---", "/"} {
		v, err := Number(raw)
		require.NoError(t, err)
		require.Nil(t, v)
	}
	v, err := Number("0.85076")
	require.NoError(t, err)
	require.Equal(t, 0.85076, *v)
}

func TestSnapshotValidationRefreshAndBinaryCache(t *testing.T) {
	// 非 UTF-8 的原始字节必须无损缓存，例如新浪 GB18030 中文字段。
	body := []byte{0xd6, 0xd0, 0xce, 0xc4}
	calls := 0
	status := 200
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		require.NotEmpty(t, r.Header.Get("User-Agent"))
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
	defer server.Close()
	cache := &Cache{CacheDir: t.TempDir()}
	validate := func(raw []byte) error {
		if len(raw) != 4 {
			return errors.New("invalid payload")
		}
		return nil
	}
	first, fetched, err := cache.Snapshot(context.Background(), server.URL, time.Hour, nil, validate)
	require.NoError(t, err)
	second, secondFetched, err := cache.Snapshot(context.Background(), server.URL, time.Hour, nil, validate)
	require.NoError(t, err)
	count := calls
	require.Equal(t, 1, count)
	require.Equal(t, first, second)
	require.Equal(t, fetched, secondFetched)
	files, err := filepath.Glob(filepath.Join(cache.CacheDir, "*.json"))
	require.NoError(t, err)
	require.Len(t, files, 1)
	saved, err := os.ReadFile(files[0])
	require.NoError(t, err)
	cache.Refresh = true
	body = []byte("invalid")
	_, _, err = cache.Snapshot(context.Background(), server.URL, time.Hour, nil, validate)
	require.ErrorContains(t, err, "invalid payload")
	after, err := os.ReadFile(files[0])
	require.NoError(t, err)
	require.Equal(t, saved, after, "失败刷新不得覆盖旧缓存")
	status = 503
	_, _, err = cache.Snapshot(context.Background(), server.URL, time.Hour, nil, validate)
	require.ErrorContains(t, err, "HTTP 503")
	cache.Refresh = false
	status = 200
	body = []byte("good")
	var invalid snapshot
	require.NoError(t, json.Unmarshal(saved, &invalid))
	invalid.FetchedAt = time.Now().Add(time.Hour)
	bad, err := json.Marshal(invalid)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(files[0], bad, 0600))
	got, _, err := cache.Snapshot(context.Background(), server.URL, time.Hour, nil, validate)
	require.NoError(t, err)
	require.Equal(t, body, got, "未来时间缓存必须重新请求")
	require.NoError(t, os.WriteFile(files[0], []byte("broken"), 0600))
	_, _, err = cache.Snapshot(context.Background(), server.URL, time.Hour, nil, validate)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = cache.Snapshot(ctx, server.URL, time.Hour, nil, validate)
	require.ErrorIs(t, err, context.Canceled)
	before := calls
	none := &Cache{}
	_, _, err = none.Snapshot(context.Background(), server.URL, time.Hour, nil, validate)
	require.NoError(t, err)
	_, _, err = none.Snapshot(context.Background(), server.URL, time.Hour, nil, validate)
	require.NoError(t, err)
	require.Equal(t, before+2, calls)
}
