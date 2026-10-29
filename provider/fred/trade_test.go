package fred

import (
	"context"
	"encoding/csv"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func tradeSample(t *testing.T, real bool) string {
	t.Helper()
	name := "testdata/us-trade-monthly.csv"
	if real {
		name = "testdata/us-trade-quarterly.csv"
	}
	raw, err := os.ReadFile(name)
	require.NoError(t, err)
	return string(raw)
}

func TestTradeOfficialValuesCacheAndComponents(t *testing.T) {
	raw := tradeSample(t, false)
	calls := 0
	broken := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		require.Equal(t, "curl", r.UserAgent())
		if broken {
			_, _ = w.Write([]byte("DATE,BOPXGS\n2026-08-01,315182\n"))
			return
		}
		_, _ = w.Write([]byte(raw))
	}))
	defer server.Close()
	client := NewClient(t.TempDir())
	client.tradeEndpoint = server.URL
	result, err := client.Trade(context.Background(), &TradeRequest{Begin: "202608", End: "202609"})
	require.NoError(t, err)
	require.Len(t, result.Observations, 2)
	require.Equal(t, "monthly", result.Frequency)
	require.Equal(t, "BOP", result.AccountingBasis)
	require.False(t, result.LevelAnnualized)
	require.Nil(t, result.ReferenceYear)
	require.Nil(t, result.PublishedAt)
	require.Nil(t, result.RevisionID)
	require.Equal(t, tradeCSVURL, result.Source)
	require.Len(t, result.Series, 3)
	require.Equal(t, "BOPTEXP", result.Series[0].SeriesID)
	require.Equal(t, "199201", result.Series[0].FirstPeriod)
	aug := result.Observations[0]
	require.Equal(t, 315182.0, *aug.Exports)
	require.Equal(t, 420754.0, *aug.Imports)
	require.Equal(t, -105572.0, *aug.Balance)
	require.Equal(t, "-105572", aug.RawBalance)
	require.Equal(t, "202607", aug.PreviousBase.Period)
	require.Equal(t, "202508", aug.YoYBase.Period)
	require.Equal(t, -92826.0, *aug.PreviousBase.Balance)
	require.NotEqual(t, *aug.PreviousBase.Exports-*aug.PreviousBase.Imports, *aug.PreviousBase.Balance, "保留官方独立舍入后的余额")
	require.InDelta(t, (315182.0/310723-1)*100, *aug.ExportsPreviousGrowth, 1e-10)
	require.Equal(t, -12746.0, *aug.BalancePreviousChange)
	require.Nil(t, result.Observations[1].Exports)
	require.Nil(t, result.Observations[1].BalancePreviousChange)
	for _, test := range []struct {
		component                 string
		exports, imports, balance float64
		id                        string
	}{{"goods", 205660, 342232, -136572, "BOPGEXP"}, {"services", 109522, 78522, 31000, "BOPSEXP"}} {
		got, err := client.Trade(context.Background(), &TradeRequest{Begin: "202608", End: "202608", Component: test.component})
		require.NoError(t, err)
		require.Equal(t, test.exports, *got.Observations[0].Exports)
		require.Equal(t, test.imports, *got.Observations[0].Imports)
		require.Equal(t, test.balance, *got.Observations[0].Balance)
		require.Equal(t, test.id, got.Series[0].SeriesID)
		require.Equal(t, result.FetchedAt, got.FetchedAt)
	}
	require.Equal(t, 1, calls)
	path := filepath.Join(client.CacheDir, "us-trade.json")
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	client.Refresh, broken = true, true
	_, err = client.Trade(context.Background(), &TradeRequest{Begin: "202608", End: "202608"})
	require.Error(t, err)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, before, after)
	broken = false
	_, err = client.Trade(context.Background(), &TradeRequest{Begin: "202608", End: "202608"})
	require.NoError(t, err)
	require.Equal(t, 3, calls)
}

func TestRealTradeOfficialValuesAndCoverage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(tradeSample(t, true))) }))
	defer server.Close()
	client := NewClient(t.TempDir())
	client.realTradeEndpoint = server.URL
	result, err := client.RealTrade(context.Background(), &RealTradeRequest{Begin: "2026Q2", End: "2026Q3"})
	require.NoError(t, err)
	require.Equal(t, "quarterly", result.Frequency)
	require.Equal(t, "NIPA", result.AccountingBasis)
	require.True(t, result.LevelAnnualized)
	require.False(t, result.GrowthAnnualized)
	require.Equal(t, 2017, *result.ReferenceYear)
	require.Equal(t, "NETEXC", result.Series[2].SeriesID)
	require.Equal(t, "1970Q1", result.Series[2].FirstPeriod)
	q2 := result.Observations[0]
	require.Equal(t, 2814.538, *q2.Exports)
	require.Equal(t, 3913.833, *q2.Imports)
	require.Equal(t, -1099.295, *q2.Balance)
	require.Equal(t, "2026Q1", q2.PreviousBase.Period)
	require.Equal(t, "2025Q2", q2.YoYBase.Period)
	require.InDelta(t, (2814.538/2780.137-1)*100, *q2.ExportsPreviousGrowth, 1e-10)
	require.InDelta(t, -79.824, *q2.BalancePreviousChange, 1e-9)
	require.Nil(t, result.Observations[1].Exports)
	for _, test := range []struct {
		component        string
		exports, imports float64
	}{{"goods", 1868.076, 3179.506}, {"services", 951.004, 733.223}} {
		got, err := client.RealTrade(context.Background(), &RealTradeRequest{Begin: "2026Q2", End: "2026Q2", Component: test.component})
		require.NoError(t, err)
		require.Equal(t, test.exports, *got.Observations[0].Exports)
		require.Equal(t, test.imports, *got.Observations[0].Imports)
		require.Nil(t, got.Observations[0].Balance)
		require.Nil(t, got.Observations[0].BalancePreviousChange)
		require.Len(t, got.Series, 2)
		require.Contains(t, got.Observations[0].Note, "未采集")
	}
	early, err := client.RealTrade(context.Background(), &RealTradeRequest{Begin: "1947Q1", End: "1947Q1"})
	require.NoError(t, err)
	require.Equal(t, 93.767, *early.Observations[0].Exports)
	require.Nil(t, early.Observations[0].Balance)
	require.Nil(t, early.Observations[0].ExportsPreviousGrowth)
	goods, err := client.RealTrade(context.Background(), &RealTradeRequest{Begin: "1947Q1", End: "1947Q1", Component: "goods"})
	require.NoError(t, err)
	require.Nil(t, goods.Observations[0].Exports)
}

func TestTradeStrictCalendarAndOfficialBalance(t *testing.T) {
	raw := tradeSample(t, false)
	// 删除上期行而保留更早月份，不能回退到最近一条观测。
	lines := strings.Split(raw, "\n")
	var kept []string
	for _, line := range lines {
		if !strings.HasPrefix(line, "2026-07-01,") && !strings.HasPrefix(line, "2025-08-01,") {
			kept = append(kept, line)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(strings.Join(kept, "\n"))) }))
	defer server.Close()
	client := NewClient("")
	client.tradeEndpoint = server.URL
	result, err := client.Trade(context.Background(), &TradeRequest{Begin: "202608", End: "202608"})
	require.NoError(t, err)
	got := result.Observations[0]
	require.Nil(t, got.ExportsPreviousGrowth)
	require.Nil(t, got.ImportsPreviousGrowth)
	require.Nil(t, got.ExportsYoY)
	require.Nil(t, got.BalancePreviousChange)
	require.Equal(t, "202607", got.PreviousBase.Period)
	require.Equal(t, "202508", got.YoYBase.Period)
	// 原始系列各自舍入；官方余额与自行相减可能不同，必须保留官方值。
	require.Equal(t, -92826.0, func() float64 {
		rows, err := parseTrade(raw, false)
		require.NoError(t, err)
		return *rows["202607"]["BOPGSTB"].number
	}())
}

func TestTradeZeroBaseAndMissingMarkers(t *testing.T) {
	raw := tradeSample(t, false)
	raw = strings.Replace(raw, "2026-07-01,310723,403550,-92826,", "2026-07-01,0,0,0,", 1)
	raw = strings.Replace(raw, "2026-08-01,315182,420754,-105572,", "2026-08-01,315182,.,-105572,", 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(raw)) }))
	defer server.Close()
	client := NewClient("")
	client.tradeEndpoint = server.URL
	result, err := client.Trade(context.Background(), &TradeRequest{Begin: "202608", End: "202608"})
	require.NoError(t, err)
	got := result.Observations[0]
	require.Nil(t, got.ExportsPreviousGrowth, "零基数不能计算百分比")
	require.Nil(t, got.Imports)
	require.Equal(t, ".", got.RawImports)
	require.Equal(t, -105572.0, *got.BalancePreviousChange, "零余额基数可以计算金额变化")
}

func TestParseTradeRejectsCorruption(t *testing.T) {
	for _, real := range []bool{false, true} {
		raw := tradeSample(t, real)
		rows, err := csv.NewReader(strings.NewReader(raw)).ReadAll()
		require.NoError(t, err)
		encode := func(rows [][]string) string {
			var out strings.Builder
			writer := csv.NewWriter(&out)
			require.NoError(t, writer.WriteAll(rows))
			return out.String()
		}
		for _, test := range []string{"date", "duplicate", "negative", "nan", "infinity", "unknown-column", "duplicate-column", "all-missing", "wrong-frequency", "truncated"} {
			t.Run(test+map[bool]string{false: "-monthly", true: "-quarterly"}[real], func(t *testing.T) {
				copyRows := make([][]string, len(rows))
				for i, row := range rows {
					copyRows[i] = append([]string(nil), row...)
				}
				switch test {
				case "date":
					copyRows[1][0] = "2026-02-30"
				case "duplicate":
					copyRows = append(copyRows, copyRows[1])
				case "negative":
					copyRows[1][1] = "-1"
				case "nan":
					copyRows[1][1] = "NaN"
				case "infinity":
					copyRows[1][1] = "+Inf"
				case "unknown-column":
					copyRows[0][1] = "BOPXGS"
				case "duplicate-column":
					copyRows[0][1] = copyRows[0][2]
				case "all-missing":
					for i := 1; i < len(copyRows); i++ {
						copyRows[i][1] = "."
					}
				case "wrong-frequency":
					copyRows[1][0] = "2026-02-02"
				case "truncated":
					copyRows[1] = copyRows[1][:2]
				}
				_, err := parseTrade(encode(copyRows), real)
				require.Error(t, err)
			})
		}
		// 列顺序与DATE旧表头可变，缺失标记保留原样。
		for _, row := range rows {
			row[0], row[1] = row[1], row[0]
		}
		rows[0][1] = "\ufeffDATE"
		_, err = parseTrade(encode(rows), real)
		require.NoError(t, err)
	}
}

func TestTradeRequestValidation(t *testing.T) {
	for _, req := range []*TradeRequest{nil, {Begin: "199112", End: "199201"}, {Begin: "202513", End: "202601"}, {Begin: "202602", End: "202601"}, {Begin: "202601", End: "299901"}, {Begin: "202601", End: "202601", Component: "other"}} {
		require.Error(t, req.Validate())
	}
	for _, req := range []*RealTradeRequest{nil, {Begin: "1946Q4", End: "1947Q1"}, {Begin: "2026Q0", End: "2026Q1"}, {Begin: "2026Q2", End: "2026Q1"}, {Begin: "2026Q1", End: "2999Q1"}, {Begin: "2026Q1", End: "2026Q1", Component: "other"}} {
		require.Error(t, req.Validate())
	}
	client := NewClient("")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := client.Trade(ctx, &TradeRequest{Begin: "202601", End: "202601"})
	require.ErrorIs(t, err, context.Canceled)
}
