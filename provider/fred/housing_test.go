package fred

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func housingSample(t *testing.T, price bool) string {
	t.Helper()
	name := "us-housing-sales.csv"
	if price {
		name = "us-housing-price.csv"
	}
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return string(raw)
}

func TestHousingOfficialSalesAndCoverage(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write([]byte(housingSample(t, false)))
	}))
	defer server.Close()
	client := NewClient(t.TempDir())
	client.housingSalesEndpoint = server.URL
	result, err := client.HousingSales(context.Background(), &HousingRequest{Begin: "202608", End: "202609"})
	require.NoError(t, err)
	require.Equal(t, "monthly", result.Frequency)
	require.False(t, result.GrowthAnnualized)
	require.Nil(t, result.PublishedAt)
	require.Nil(t, result.RevisionID)
	require.Len(t, result.Series, 2)
	require.Equal(t, "196301", result.Series[0].FirstPeriod)
	require.Equal(t, "202509", result.Series[1].FirstPeriod)
	require.Equal(t, "202609", result.Series[1].LastPeriod)
	require.Equal(t, "千套", result.Series[0].Unit)
	require.Equal(t, "套", result.Series[1].Unit)
	require.True(t, result.Series[0].LevelAnnualized)
	require.True(t, result.Series[1].SeasonallyAdjusted)
	require.Contains(t, result.Series[1].Copyright, "National Association of Realtors")
	require.Len(t, result.Observations, 4)
	new, existing := result.Observations[2], result.Observations[3]
	require.Equal(t, "HSN1F", new.SeriesID)
	require.Equal(t, 676.0, *new.Value)
	require.Equal(t, "676", new.Raw)
	require.Equal(t, 3990000.0, *existing.Value)
	require.Equal(t, "套", existing.Unit)
	require.Equal(t, "202608", existing.PreviousBase.Period)
	require.Equal(t, 3930000.0, *existing.PreviousBase.Value)
	require.Equal(t, "202509", existing.YoYBase.Period)
	require.Equal(t, 4080000.0, *existing.YoYBase.Value)
	require.InDelta(t, (3990000.0/4080000-1)*100, *existing.YoY, 1e-10)
	require.InDelta(t, (676.0/680-1)*100, *new.PreviousPeriodGrowth, 1e-10)
	require.Nil(t, result.Observations[1].YoY)
	for _, component := range []string{"new-sales", "existing-sales"} {
		got, err := client.HousingSales(context.Background(), &HousingRequest{Begin: "202609", End: "202609", Component: component})
		require.NoError(t, err)
		require.Len(t, got.Series, 1)
		require.Len(t, got.Observations, 1)
		require.Equal(t, component, got.Observations[0].Component)
		require.Equal(t, result.FetchedAt, got.FetchedAt)
	}
	require.Equal(t, 1, calls)
	early, err := client.HousingSales(context.Background(), &HousingRequest{Begin: "196301", End: "196301"})
	require.NoError(t, err)
	require.Equal(t, 591.0, *early.Observations[0].Value)
	require.Nil(t, early.Observations[0].PreviousPeriodGrowth)
	require.Nil(t, early.Observations[1].Value)
	require.Contains(t, early.Observations[1].Note, "当前成屋公开快照有效覆盖")
	future, err := client.HousingSales(context.Background(), &HousingRequest{Begin: "202610", End: "202610"})
	require.NoError(t, err)
	for _, item := range future.Observations {
		require.Nil(t, item.Value)
	}
}

func TestHousingOfficialPrice(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(housingSample(t, true)))
	}))
	defer server.Close()
	client := NewClient("")
	client.housePriceEndpoint = server.URL
	result, err := client.HousePrice(context.Background(), &HousePriceRequest{Begin: "2026Q2", End: "2026Q3"})
	require.NoError(t, err)
	require.Equal(t, "quarterly", result.Frequency)
	require.Equal(t, "USSTHPI", result.Series[0].SeriesID)
	require.Equal(t, "1980Q1=100", result.Series[0].IndexBase)
	require.Equal(t, "1975Q1", result.Series[0].FirstPeriod)
	require.False(t, result.Series[0].SeasonallyAdjusted)
	require.False(t, result.Series[0].LevelAnnualized)
	require.False(t, result.GrowthAnnualized)
	require.Contains(t, result.Series[0].Definition, "再融资评估值")
	require.Equal(t, 719.87, *result.Observations[0].Value)
	require.Equal(t, "2026Q1", result.Observations[0].PreviousBase.Period)
	require.Equal(t, "2025Q2", result.Observations[0].YoYBase.Period)
	require.InDelta(t, (719.87/712.2-1)*100, *result.Observations[0].PreviousPeriodGrowth, 1e-10)
	require.InDelta(t, (719.87/698.69-1)*100, *result.Observations[0].YoY, 1e-10)
	require.Contains(t, result.Observations[0].Note, "季环比未季调")
	require.Nil(t, result.Observations[1].Value)
}

func TestHousingStrictCalendarBases(t *testing.T) {
	for _, price := range []bool{false, true} {
		raw := housingSample(t, price)
		omitted := []string{"2026-08-01,", "2025-09-01,"}
		if price {
			omitted = []string{"2026-01-01,", "2025-04-01,"}
		}
		var kept []string
		for _, line := range strings.Split(raw, "\n") {
			if !strings.HasPrefix(line, omitted[0]) && !strings.HasPrefix(line, omitted[1]) {
				kept = append(kept, line)
			}
		}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(strings.Join(kept, "\n"))) }))
		client := NewClient("")
		client.housingSalesEndpoint, client.housePriceEndpoint = server.URL, server.URL
		var result *HousingResult
		var err error
		if price {
			result, err = client.HousePrice(context.Background(), &HousePriceRequest{Begin: "2026Q2", End: "2026Q2"})
		} else {
			result, err = client.HousingSales(context.Background(), &HousingRequest{Begin: "202609", End: "202609"})
		}
		require.NoError(t, err)
		for _, item := range result.Observations {
			require.NotNil(t, item.Value)
			require.Nil(t, item.YoY)
			require.Nil(t, item.PreviousPeriodGrowth)
			require.Nil(t, item.PreviousBase.Value)
			require.Nil(t, item.YoYBase.Value)
		}
		server.Close()
	}
}

func TestHousingCacheFailureAndRecovery(t *testing.T) {
	for _, price := range []bool{false, true} {
		t.Run(map[bool]string{false: "sales", true: "price"}[price], func(t *testing.T) {
			calls, invalid := 0, false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if invalid {
					w.Write([]byte("observation_date,wrong\n2026-01-01,1\n"))
					return
				}
				w.Write([]byte(housingSample(t, price)))
			}))
			defer server.Close()
			client := NewClient(t.TempDir())
			client.housingSalesEndpoint, client.housePriceEndpoint = server.URL, server.URL
			query := func() (*HousingResult, error) {
				if price {
					return client.HousePrice(context.Background(), &HousePriceRequest{Begin: "2026Q2", End: "2026Q2"})
				}
				return client.HousingSales(context.Background(), &HousingRequest{Begin: "202609", End: "202609"})
			}
			_, err := query()
			require.NoError(t, err)
			name := "us-housing-sales.json"
			if price {
				name = "us-housing-price.json"
			}
			path := filepath.Join(client.CacheDir, name)
			good, err := os.ReadFile(path)
			require.NoError(t, err)
			client.Refresh, invalid = true, true
			_, err = query()
			require.Error(t, err)
			after, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, good, after)
			invalid, client.Refresh = false, false
			var entry csvCache
			require.NoError(t, json.Unmarshal(good, &entry))
			entry.CSV = "broken"
			poisoned, err := json.Marshal(entry)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(path, poisoned, 0600))
			_, err = query()
			require.NoError(t, err)
			require.Equal(t, 3, calls)
			corrected, err := os.ReadFile(path)
			require.NoError(t, err)
			require.NoError(t, json.Unmarshal(corrected, &entry))
			_, err = housingParse(entry.CSV, price)
			require.NoError(t, err)
			// 未来缓存时间及来源不匹配均不能复用。
			for _, corrupt := range []func(){func() { entry.FetchedAt = time.Now().Add(time.Hour) }, func() { entry.Source = "https://wrong.invalid" }} {
				corrupt()
				data, err := json.Marshal(entry)
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(path, data, 0600))
				_, err = query()
				require.NoError(t, err)
			}
			require.Equal(t, 5, calls)
			client.CacheDir = ""
			_, err = query()
			require.NoError(t, err)
			_, err = query()
			require.NoError(t, err)
			require.Equal(t, 7, calls)
		})
	}
}

func TestHousingParseValidation(t *testing.T) {
	for _, price := range []bool{false, true} {
		rows, err := csv.NewReader(strings.NewReader(housingSample(t, price))).ReadAll()
		require.NoError(t, err)
		for _, name := range []string{"negative", "nan", "infinite", "duplicate-column", "unknown-column", "bad-date", "wrong-frequency", "duplicate-period", "truncated", "all-missing", "overflow"} {
			t.Run(name+map[bool]string{false: "-sales", true: "-price"}[price], func(t *testing.T) {
				copyRows := make([][]string, len(rows))
				for i, row := range rows {
					copyRows[i] = append([]string(nil), row...)
				}
				switch name {
				case "negative":
					copyRows[1][1] = "-1"
				case "nan":
					copyRows[1][1] = "NaN"
				case "infinite":
					copyRows[1][1] = "Inf"
				case "duplicate-column":
					copyRows[0][1] = copyRows[0][0]
				case "unknown-column":
					copyRows[0][1] = "MSPUS"
				case "bad-date":
					copyRows[1][0] = "2026-02-30"
				case "wrong-frequency":
					if price {
						copyRows[1][0] = "1975-02-01"
					} else {
						copyRows[1][0] = "1963-01-02"
					}
				case "duplicate-period":
					copyRows = append(copyRows, copyRows[1])
				case "truncated":
					copyRows[1] = copyRows[1][:1]
				case "all-missing":
					for i := 1; i < len(copyRows); i++ {
						copyRows[i][1] = "."
					}
				case "overflow":
					copyRows[1][1], copyRows[2][1] = "1e-100", "1e308"
				}
				var out strings.Builder
				writer := csv.NewWriter(&out)
				require.NoError(t, writer.WriteAll(copyRows))
				_, err := housingParse(out.String(), price)
				require.Error(t, err)
			})
		}
		// 支持按列名匹配及旧DATE表头，禁止按列位置误读。
		for _, row := range rows {
			row[0], row[1] = row[1], row[0]
		}
		rows[0][1] = "\ufeffDATE"
		var out strings.Builder
		require.NoError(t, csv.NewWriter(&out).WriteAll(rows))
		_, err = housingParse(out.String(), price)
		require.NoError(t, err)
	}
}

func TestHousingZeroAndValidation(t *testing.T) {
	rows, err := housingParse("observation_date,HSN1F,EXHOSLUSM495S\n2026-08-01,0,0\n2026-09-01,2,3\n", false)
	require.NoError(t, err)
	require.NotNil(t, rows["202608"]["HSN1F"].value)
	require.Zero(t, *rows["202608"]["HSN1F"].value)
	growth, err := housingChange(rows["202609"]["HSN1F"].value, rows["202608"]["HSN1F"].value)
	require.NoError(t, err)
	require.Nil(t, growth)
	_, err = housingParse("observation_date,USSTHPI\n2026-01-01,0\n", true)
	require.Error(t, err)
	for _, req := range []*HousingRequest{nil, {Begin: "196212", End: "196301"}, {Begin: "202613", End: "202601"}, {Begin: "202609", End: "202601"}, {Begin: "202601", End: "299901"}, {Begin: "202601", End: "202601", Component: "other"}} {
		require.Error(t, req.Validate())
	}
	for _, req := range []*HousePriceRequest{nil, {Begin: "1974Q4", End: "1975Q1"}, {Begin: "2026Q0", End: "2026Q1"}, {Begin: "2026Q2", End: "2026Q1"}, {Begin: "2026Q1", End: "2999Q1"}} {
		require.Error(t, req.Validate())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = NewClient("").HousingSales(ctx, &HousingRequest{Begin: "202601", End: "202601"})
	require.ErrorIs(t, err, context.Canceled)
}

func TestHousingOverflowRefreshPreservesCache(t *testing.T) {
	for _, price := range []bool{false, true} {
		t.Run(map[bool]string{false: "sales", true: "price"}[price], func(t *testing.T) {
			raw := housingSample(t, price)
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Write([]byte(raw))
			}))
			defer server.Close()
			client := NewClient(t.TempDir())
			client.housingSalesEndpoint, client.housePriceEndpoint = server.URL, server.URL
			query := func() (*HousingResult, error) {
				if price {
					return client.HousePrice(context.Background(), &HousePriceRequest{Begin: "2026Q2", End: "2026Q2"})
				}
				return client.HousingSales(context.Background(), &HousingRequest{Begin: "202609", End: "202609"})
			}
			goodResult, err := query()
			require.NoError(t, err)
			filename := "us-housing-sales.json"
			if price {
				filename = "us-housing-price.json"
			}
			path := filepath.Join(client.CacheDir, filename)
			goodCache, err := os.ReadFile(path)
			require.NoError(t, err)
			rows, err := csv.NewReader(strings.NewReader(raw)).ReadAll()
			require.NoError(t, err)
			// 两个有限正原值相除仍可能溢出，须在写缓存前拒绝整个快照。
			rows[len(rows)-2][1], rows[len(rows)-1][1] = "1e-100", "1e308"
			var broken strings.Builder
			require.NoError(t, csv.NewWriter(&broken).WriteAll(rows))
			raw, client.Refresh = broken.String(), true
			_, err = query()
			require.ErrorContains(t, err, "增速计算超出有限数值范围")
			after, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, goodCache, after)
			client.Refresh = false
			cached, err := query()
			require.NoError(t, err)
			require.Equal(t, goodResult, cached)
			require.Equal(t, 2, calls)
		})
	}
}
