package mof

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name + ".html")
	require.NoError(t, err)
	return string(raw)
}
func TestFiscalOfficialReleases(t *testing.T) {
	for _, test := range []struct {
		period, title, date string
		amounts, yoy        []float64
	}{
		{"201502", "2015年1-2月财政收支情况", "2015-03-16", []float64{25717, 18865, 5756, 4493}, []float64{3.2, 10.5, -33.7, -21}},
		{"202411", "2024年11月财政收支情况", "2024-12-16", []float64{199010, 245053, 42348, 81038}, []float64{-.6, 2.8, -18.4, -2.6}},
		{"202512", "2025年财政收支情况", "2026-01-30", []float64{216045, 287395, 57704, 112874}, []float64{-1.7, 1, -7, 11.3}},
		{"202609", "2026年前三季度财政收支情况", "2026-10-23", []float64{172292, 210199, 25010, 62754}, []float64{5.1, 1, -18.6, -16.2}},
	} {
		t.Run(test.period, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(fixture(t, "fiscal-"+test.period)))
			require.NoError(t, err)
			got, err := parseRelease(doc, release{Period: test.period, Title: test.title, PublishedDate: test.date})
			require.NoError(t, err)
			require.Len(t, got.Metrics, 4)
			for i, m := range got.Metrics {
				require.Equal(t, test.amounts[i], *m.YTDAmount)
				require.Equal(t, test.yoy[i], *m.YTDYoY)
				require.NotEmpty(t, m.RawParagraph)
			}
			if test.period == "201502" {
				for i, want := range []float64{1.7, 9.5, -30.7, -18.6} {
					require.Equal(t, want, *got.Metrics[i].ComparableYTDYoY)
				}
			}
			require.Nil(t, got.RevisionID)
		})
	}
}

func mockIndex(t *testing.T, page int) {
	name, address := "index-0", topicURL
	if page > 0 {
		name = "index-" + string(rune('0'+page))
		address += "index_" + string(rune('0'+page)) + ".html"
	}
	gock.New(address).Get("").Reply(200).BodyString(fixture(t, name))
}
func TestFiscalDirectoryPaginationAndGaps(t *testing.T) {
	defer gock.Off()
	for i := 0; i < 10; i++ {
		mockIndex(t, i)
	}
	gock.New("https://gks.mof.gov.cn").Get("/tongjishuju/201503/t20150316_1202393.htm").Reply(200).BodyString(fixture(t, "fiscal-201502"))
	result, err := NewClient("").Fiscal(context.Background(), &FiscalRequest{Begin: "201501", End: "201502", Budget: "all"})
	require.NoError(t, err)
	require.Len(t, result.Observations, 2)
	require.Equal(t, "missing-release", result.Observations[0].Status)
	require.Nil(t, result.Observations[0].Metrics[0].YTDAmount)
	require.Equal(t, "201501-201502", result.Observations[1].YTDPeriod)
	require.Equal(t, 25717.0, *result.Observations[1].Metrics[0].YTDAmount)
	require.False(t, result.SeasonallyAdjusted)
	require.False(t, result.Annualized)
	require.True(t, gock.IsDone())
}

func TestFiscalCacheProtectionAndRecovery(t *testing.T) {
	defer gock.Off()
	client := NewClient(t.TempDir())
	req := &FiscalRequest{Begin: "202609", End: "202609", Budget: "fund"}
	for i := 0; i < 10; i++ {
		mockIndex(t, i)
	}
	gock.New("https://gks.mof.gov.cn").Get("/tongjishuju/202610/t20261023_4003380.htm").Reply(200).BodyString(fixture(t, "fiscal-202609"))
	good, err := client.Fiscal(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, good.Observations[0].Metrics, 2)
	cached, err := client.Fiscal(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, good, cached)
	require.True(t, gock.IsDone())
	files, err := filepath.Glob(filepath.Join(client.CacheDir, "*.json"))
	require.NoError(t, err)
	require.Len(t, files, 11)
	saved := map[string][]byte{}
	for _, p := range files {
		saved[p], err = os.ReadFile(p)
		require.NoError(t, err)
	}
	client.Refresh = true
	for i := 0; i < 10; i++ {
		mockIndex(t, i)
	}
	bad := strings.Replace(fixture(t, "fiscal-202609"), "172292", "NaN", 1)
	gock.New("https://gks.mof.gov.cn").Get("/tongjishuju/202610/t20261023_4003380.htm").Reply(200).BodyString(bad)
	_, err = client.Fiscal(context.Background(), req)
	require.Error(t, err)
	for p, raw := range saved {
		var old, new cachedPage
		require.NoError(t, json.Unmarshal(raw, &old))
		now, e := os.ReadFile(p)
		require.NoError(t, e)
		require.NoError(t, json.Unmarshal(now, &new))
		require.Equal(t, old.Body, new.Body)
	}
	client.Refresh = false
	_, err = client.Fiscal(context.Background(), req)
	require.NoError(t, err)
}

func TestFiscalValidationAndMalformedSources(t *testing.T) {
	for _, r := range []*FiscalRequest{nil, {Begin: "201412", End: "201501", Budget: "all"}, {Begin: "202602", End: "202601", Budget: "all"}, {Begin: "202613", End: "202613", Budget: "all"}, {Begin: "202601", End: "999901", Budget: "all"}, {Begin: "202601", End: "202602", Budget: "other"}} {
		_, err := NewClient("").Fiscal(context.Background(), r)
		require.Error(t, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewClient("").Fiscal(ctx, &FiscalRequest{Begin: "202609", End: "202609", Budget: "all"})
	require.ErrorIs(t, err, context.Canceled)
	for _, title := range []string{"2026年一季度财政收支情况", "2026年上半年财政收支情况", "2026年前三季度财政收支情况", "2026年1-11月财政收支情况", "2026年财政收支情况"} {
		_, ok := titlePeriod(title)
		require.True(t, ok)
	}
	for _, title := range []string{"2026年1-13月财政收支情况", "2026年度财政收支情况新闻发布会文字实录", "2026年9月中央政府收支及融资数据"} {
		_, ok := titlePeriod(title)
		require.False(t, ok)
	}
	raw := fixture(t, "fiscal-202609")
	for _, bad := range []string{strings.Replace(raw, "172292", "NaN", 1), strings.Replace(raw, "亿元", "万元", 1), strings.ReplaceAll(raw, "2026年前三季度财政收支情况", "2026年上半年财政收支情况"), strings.Replace(raw, "全国一般公共预算收入", "中央一般公共预算收入", 1)} {
		doc, e := goquery.NewDocumentFromReader(strings.NewReader(bad))
		require.NoError(t, e)
		_, err := parseRelease(doc, release{Period: "202609", Title: "2026年前三季度财政收支情况", PublishedDate: "2026-10-23"})
		require.Error(t, err)
	}
}

func TestFiscalHistoricalWording(t *testing.T) {
	for _, test := range []struct {
		period, title, date string
		metric              int
		amount              float64
	}{
		{"201504", "2015年4月财政收支情况", "2015-05-14", 1, 45350},
		{"201602", "2016年1-2月财政收支情况", "2016-03-16", 2, 5698},
		{"201612", "2016年财政收支情况", "2017-01-23", 0, 159552},
		{"201711", "2017年11月财政收支情况", "2017-12-11", 1, 179560},
	} {
		t.Run(test.period, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(fixture(t, "fiscal-"+test.period)))
			require.NoError(t, err)
			got, err := parseRelease(doc, release{Period: test.period, Title: test.title, PublishedDate: test.date})
			require.NoError(t, err)
			require.Equal(t, test.amount, *got.Metrics[test.metric].YTDAmount)
			if test.period == "201602" {
				require.Nil(t, got.Metrics[2].YTDYoY)
				require.Contains(t, got.Metrics[2].Note, "持平")
				require.Equal(t, "missing-values", got.Status)
			}
			if test.period == "201504" {
				require.Equal(t, 13.8, *got.Metrics[1].YTDYoY)
				require.Equal(t, 12.7, *got.Metrics[1].ComparableYTDYoY)
			}
		})
	}
}

func TestFiscalMonthlyAndYTDSameParagraphAndGrowthBases(t *testing.T) {
	for _, test := range []struct {
		period, title, date string
		metric              int
		amount, rate        float64
	}{
		{"201811", "2018年11月财政收支情况", "2018-12-13", 0, 172333, 6.5},
		{"201902", "2019年1-2月财政收支情况", "2019-03-18", 3, 14374, 110},
		{"202004", "2020年4月财政收支情况", "2020-05-18", 3, 25766, 14.9},
		{"202204", "2022年4月财政收支情况", "2022-05-17", 0, 74293, -4.8},
	} {
		t.Run(test.period, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(fixture(t, "fiscal-"+test.period)))
			require.NoError(t, err)
			got, err := parseRelease(doc, release{Period: test.period, Title: test.title, PublishedDate: test.date})
			require.NoError(t, err)
			require.Equal(t, test.amount, *got.Metrics[test.metric].YTDAmount)
			require.InDelta(t, test.rate, *got.Metrics[test.metric].YTDYoY, 1e-10)
			if test.period == "202204" {
				require.Equal(t, 5.0, *got.Metrics[0].TaxRefundAdjustedYTDYoY)
				require.Nil(t, got.Metrics[0].ComparableYTDYoY)
			}
		})
	}
	_, err := rate([]string{"", "增长", strings.Repeat("9", 400), "%"})
	require.Error(t, err)
}
