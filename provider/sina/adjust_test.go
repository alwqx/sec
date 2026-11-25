package sina

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

// adjustAt 返回 date 所在段的前复权价。
func adjustAt(t *testing.T, segments []AdjustSegment, date string, raw float64) float64 {
	t.Helper()
	var found *AdjustSegment
	for i := range segments {
		if segments[i].Date <= date {
			found = &segments[i]
		}
	}
	require.NotNil(t, found, date)
	return raw*found.Scale + found.Offset
}

func TestQueryAdjustFactors(t *testing.T) {
	defer gock.Off()
	for _, tc := range []struct {
		exCode, path, fixture string
	}{
		{"SH600036", "/realstock/company/sh600036/qfq.js", "qfq_sh600036.js"},
		{"HK00700", "/stock/hkstock/00700/qfq.js", "qfq_hk00700.js"},
		{"$aapl", "/us_stock/company/reinstatement/AAPL_qfq.js", "qfq_AAPL.js"},
	} {
		body, err := os.ReadFile("testdata/" + tc.fixture)
		require.NoError(t, err)
		gock.New("https://finance.sina.com.cn").Get(tc.path).MatchHeader("Referer", "finance.sina.com.cn").Reply(200).BodyString(string(body))
		segments, err := QueryAdjustFactors(context.Background(), tc.exCode)
		require.NoError(t, err, tc.exCode)
		require.NotEmpty(t, segments, tc.exCode)
	}
	require.True(t, gock.IsDone())

	gock.New("https://finance.sina.com.cn").Get("/stock/hkstock/02800/qfq.js").Reply(404)
	_, err := QueryAdjustFactors(context.Background(), "HK02800")
	require.True(t, errors.Is(err, ErrNoAdjustFactors), "港股 ETF 和不存在的代码新浪都返回 404")
	require.True(t, gock.IsDone())

	gock.New("https://finance.sina.com.cn").Get("/realstock/company/sh600036/qfq.js").Reply(456)
	_, err = QueryAdjustFactors(context.Background(), "SH600036")
	require.ErrorContains(t, err, "HTTP 456")
	require.True(t, gock.IsDone())

	for _, bad := range []string{"", "600036", "$", "HK", "SH"} {
		_, err := QueryAdjustFactors(context.Background(), bad)
		require.Error(t, err, bad)
	}
}

func TestAdjustURL(t *testing.T) {
	for _, tc := range []struct {
		exCode, url string
		market      adjustMarket
	}{
		{"SH600036", "https://finance.sina.com.cn/realstock/company/sh600036/qfq.js", adjustA},
		{"sz159915", "https://finance.sina.com.cn/realstock/company/sz159915/qfq.js", adjustA},
		{"HK00700", "https://finance.sina.com.cn/stock/hkstock/00700/qfq.js", adjustHK},
		{"hk700", "https://finance.sina.com.cn/stock/hkstock/00700/qfq.js", adjustHK},
		{"HKhsi", "https://finance.sina.com.cn/stock/hkstock/HSI/qfq.js", adjustHK},
		{" $brk.b ", "https://finance.sina.com.cn/us_stock/company/reinstatement/BRK.B_qfq.js", adjustUS},
	} {
		got, market, err := adjustURL(tc.exCode)
		require.NoError(t, err, tc.exCode)
		require.Equal(t, tc.url, got, tc.exCode)
		require.Equal(t, tc.market, market, tc.exCode)
	}
}

func TestParseAdjustFactors(t *testing.T) {
	load := func(name string, market adjustMarket) []AdjustSegment {
		body, err := os.ReadFile("testdata/" + name)
		require.NoError(t, err)
		segments, err := parseAdjustFactors(body, market)
		require.NoError(t, err, name)
		for i := 1; i < len(segments); i++ {
			require.Less(t, segments[i-1].Date, segments[i].Date, "按日期升序")
		}
		return segments
	}

	// 沪深股票按比例调整：2025-07-11 除息 2 元，前收 48.24，比例为 46.24/48.24。
	cmb := load("qfq_sh600036.js", adjustA)
	require.Len(t, cmb, 32)
	require.Equal(t, AdjustSegment{Date: "1900-01-01", Scale: 1 / 6.7422879877129, Offset: 0}, cmb[0])
	require.Equal(t, AdjustSegment{Date: "2026-07-10", Scale: 1, Offset: 0}, cmb[len(cmb)-1])
	require.InDelta(t, 46.24/48.24, adjustAt(t, cmb, "2025-07-10", 1)/adjustAt(t, cmb, "2025-07-11", 1), 1e-4)
	require.Greater(t, adjustAt(t, cmb, "2002-04-09", 10.66), 0.0, "东方财富前复权这一天是 -13.08")

	// 港股按比例调整：2025-05-16 除息 4.5 港元，前收 520。
	tencent := load("qfq_hk00700.js", adjustHK)
	require.InDelta(t, (520-4.5)/520.0, adjustAt(t, tencent, "2025-05-15", 1)/adjustAt(t, tencent, "2025-05-16", 1), 1e-4)

	// 美股和东方财富前复权一样，分红按金额减去。
	aapl := load("qfq_AAPL.js", adjustUS)
	require.InDelta(t, 118.788, adjustAt(t, aapl, "2020-08-28", 499.23), 0.005, "2020-08-31 一拆四之前")
	require.InDelta(t, 13.054, adjustAt(t, aapl, "2014-06-06", 645.57), 0.005, "2014-06-09 一拆七之前")
	require.InDelta(t, 43.2, adjustAt(t, aapl, "2026-11-25", 43.2), 1e-9, "最新一段不调整")

	// 沪深基金：除以 s 再减去 u，和东方财富前复权一致。
	fund := load("qfq_sh510500.js", adjustA)
	require.InDelta(t, 5.757, adjustAt(t, fund, "2017-10-09", 7.039), 0.0005)
	require.InDelta(t, 2.632, adjustAt(t, fund, "2013-03-15", 0.97), 0.0005, "2015 年份额合并之前")

	segments, err := parseAdjustFactors([]byte(`var sh000001qfq={"total":0, "data":[]}
/* abc */`), adjustA)
	require.NoError(t, err)
	require.Empty(t, segments, "指数没有复权因子")

	for name, body := range map[string]string{
		"不是 JS":     "<html>",
		"日期无效":      `var a={"data":[{"d":"2026-13-01","f":"1"}]}`,
		"因子为 0":     `var a={"data":[{"d":"2026-01-01","f":"0"}]}`,
		"因子是 NaN":   `var a={"data":[{"d":"2026-01-01","f":"NaN"}]}`,
		"股票带分红金额":   `var a={"data":[{"d":"2026-01-01","f":"1","c":"-1"}]}`,
		"基金 f 不是 1": `var a={"data":[{"d":"2026-01-01","f":"2","s":"1","u":"0"}]}`,
		"基金缺 u":     `var a={"data":[{"d":"2026-01-01","f":"1","s":"1"}]}`,
		"基金和股票混在一起": `var a={"data":[{"d":"2026-01-01","f":"1","s":"1","u":"0"},{"d":"2025-01-01","f":"1.1"}]}`,
		"股票和基金混在一起": `var a={"data":[{"d":"2026-01-01","f":"1"},{"d":"2025-01-01","f":"1","s":"1","u":"0.1"}]}`,
		"日期重复":      `var a={"data":[{"d":"2026-01-01","f":"1"},{"d":"2026-01-01","f":"1.1"}]}`,
	} {
		_, err := parseAdjustFactors([]byte(body), adjustA)
		require.Error(t, err, name)
	}
	_, err = parseAdjustFactors([]byte(`var a={"data":[{"d":"2026-01-01","f":"1","s":"1","u":"0"}]}`), adjustHK)
	require.Error(t, err, "港股不应有基金字段")
}
