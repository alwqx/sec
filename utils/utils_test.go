package utils

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
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

func TestMakeRequestKeepsUserAgent(t *testing.T) {
	defer gock.Off()
	version.Version = "test"
	gock.New("http://abc.xyz").Get("/foo").MatchHeader("User-Agent", "^sec/test").MatchHeader("Referer", "http://r").Times(1).Reply(200)
	headers := http.Header{"Referer": []string{"http://r"}}
	resp, err := MakeRequest(context.Background(), http.MethodGet, "http://abc.xyz/foo", headers, nil, 0)
	require.NoError(t, err)
	resp.Body.Close()
	require.Empty(t, headers.Get("User-Agent"), "不修改调用方的请求头")

	gock.New("http://abc.xyz").Get("/bar").MatchHeader("User-Agent", "^custom$").Reply(200)
	headers.Set("User-Agent", "custom")
	resp, err = MakeRequest(context.Background(), http.MethodGet, "http://abc.xyz/bar", headers, nil, 0)
	require.NoError(t, err, "调用方指定时用调用方的")
	resp.Body.Close()
}

func TestMakeRequestRetry(t *testing.T) {
	defer gock.Off()
	defer func(d time.Duration) { retryDelay = d }(retryDelay)
	retryDelay = time.Millisecond
	get := func(ctx context.Context, path string) (*http.Response, error) {
		return MakeRequest(ctx, http.MethodGet, "http://abc.xyz"+path, nil, nil, 0)
	}

	gock.New("http://abc.xyz").Get("/reset").ReplyError(errors.New("connection reset"))
	gock.New("http://abc.xyz").Get("/reset").Reply(200).BodyString("OK")
	resp, err := get(context.Background(), "/reset")
	require.NoError(t, err, "连接失败后重试一次")
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	require.NoError(t, err)
	require.Equal(t, "OK", string(body))
	require.True(t, gock.IsDone())

	gock.New("http://abc.xyz").Get("/down").Times(2).ReplyError(errors.New("connection refused"))
	_, err = get(context.Background(), "/down")
	require.ErrorContains(t, err, "connection refused", "只重试一次，返回第二次的错误")
	require.True(t, gock.IsDone())

	gock.New("http://abc.xyz").Get("/busy").Reply(503)
	gock.New("http://abc.xyz").Get("/busy").Reply(200)
	resp, err = get(context.Background(), "/busy")
	require.NoError(t, err)
	resp.Body.Close()
	require.Equal(t, 503, resp.StatusCode, "HTTP 错误状态不重试")
	gock.Off()

	gock.New("http://abc.xyz").Post("/form").ReplyError(errors.New("connection reset"))
	gock.New("http://abc.xyz").Post("/form").Reply(200)
	_, err = MakeRequest(context.Background(), http.MethodPost, "http://abc.xyz/form", nil, bytes.NewReader([]byte("a=1")), 0)
	require.ErrorContains(t, err, "connection reset", "POST 不重试")
	gock.Off()

	ctx, cancel := context.WithCancel(context.Background())
	gock.Observe(func(*http.Request, gock.Mock) { cancel() })
	defer gock.Observe(nil)
	gock.New("http://abc.xyz").Get("/cancel").ReplyError(errors.New("connection reset"))
	gock.New("http://abc.xyz").Get("/cancel").Reply(200)
	_, err = get(ctx, "/cancel")
	require.Error(t, err)
	require.True(t, gock.IsPending(), "取消后不再发第二次")
}

func TestCheckStatus(t *testing.T) {
	require.NoError(t, CheckStatus(&http.Response{StatusCode: 200}))
	require.ErrorContains(t, CheckStatus(&http.Response{StatusCode: 503}), "HTTP 503")
	reqURL, err := url.Parse("https://a.example/x")
	require.NoError(t, err)
	require.EqualError(t, CheckStatus(&http.Response{StatusCode: 404, Request: &http.Request{URL: reqURL}}), "a.example 返回 HTTP 404")
}

func TestWriteJson(t *testing.T) {
	tmpDir := t.TempDir()
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

func TestHumanByte(t *testing.T) {
	require.EqualValues(t, " - ", HumanByte(-1))
	require.EqualValues(t, " - ", HumanByte(0.0))
	require.EqualValues(t, "500 B", HumanByte(500))
	require.EqualValues(t, "1.00 KB", HumanByte(1024))
	require.EqualValues(t, "1.50 KB", HumanByte(1536))
	require.EqualValues(t, "1.00 MB", HumanByte(1048576))
	require.EqualValues(t, "2.50 MB", HumanByte(2621440))
	require.EqualValues(t, "1.00 GB", HumanByte(1073741824))
	require.EqualValues(t, "3.75 GB", HumanByte(4026531840))
}

func TestHumanNum(t *testing.T) {
	require.EqualValues(t, " - ", HumanNum(-1))
	require.EqualValues(t, " - ", HumanNum(0.0))
	require.EqualValues(t, "1.00万", HumanNum(10000))
	require.EqualValues(t, "10.09万", HumanNum(100900))
	require.EqualValues(t, "1000.09亿", HumanNum(100009000009))
}

func TestPrintJSON(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, PrintJSON(&out, map[string]any{"title": "A&B <年报>", "value": nil}))
	require.Equal(t, "{\n  \"title\": \"A&B <年报>\",\n  \"value\": null\n}\n", out.String(), "不转义 & 和 <，保留可读性")
	require.Error(t, PrintJSON(&out, math.NaN()))
}

func TestParseRange(t *testing.T) {
	begin, end, err := ParseRange("", "20261103", 30)
	require.NoError(t, err)
	require.Equal(t, "2026-10-04", begin.Format("2006-01-02"), "默认开始日期从结束日期往前推")
	require.Equal(t, "2026-11-03", end.Format("2006-01-02"))
	begin, _, err = ParseRange("20260101", "20261103", 30)
	require.NoError(t, err)
	require.Equal(t, "2026-01-01", begin.Format("2006-01-02"))
	_, _, err = ParseRange("20261104", "20261103", 30)
	require.ErrorContains(t, err, "晚于")
	_, _, err = ParseRange("2026-11-01", "", 30)
	require.ErrorContains(t, err, "YYYYMMDD")
}

// 只给今天作为 --begin 时，任何时刻都是有效区间。
func TestParseRangeToday(t *testing.T) {
	today := ShanghaiToday().Format("20060102")
	begin, end, err := ParseRange(today, "", 30)
	require.NoError(t, err)
	require.Equal(t, begin, end)
}
