package fred

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func investmentSample(t *testing.T, quarterly bool) string {
	t.Helper()
	name := "testdata/us-investment-housing.csv"
	if quarterly {
		name = "testdata/us-investment-equipment.csv"
	}
	raw, err := os.ReadFile(name)
	require.NoError(t, err)
	return string(raw)
}

func TestInvestmentOfficialValuesAndCoverage(t *testing.T) {
	for _, test := range []struct {
		component, begin, end, id, first string
		quarterly                        bool
		level, previous, yoy             float64
	}{
		{"housing-starts", "202609", "202610", "HOUST", "195901", false, 1221, 1288, 1319},
		{"building-permits", "202609", "202610", "PERMIT", "196001", false, 1398, 1403, 1444},
		{"real", "2026Q2", "2026Q3", "Y033RX1Q020SBEA", "2007Q1", true, 1598.303, 1548.717, 1437.962},
		{"nominal", "2026Q2", "2026Q3", "Y033RC1Q027SBEA", "1947Q1", true, 1922.516, 1836.848, 1651.610},
	} {
		t.Run(test.component, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "curl", r.UserAgent())
				_, _ = w.Write([]byte(investmentSample(t, test.quarterly)))
			}))
			defer server.Close()
			client := NewClient("")
			client.housingInvestmentEndpoint, client.equipmentInvestmentEndpoint = server.URL, server.URL
			var result *InvestmentResult
			var err error
			if test.quarterly {
				result, err = client.EquipmentInvestment(context.Background(), &EquipmentInvestmentRequest{Begin: test.begin, End: test.end, PriceBasis: test.component})
			} else {
				result, err = client.HousingInvestment(context.Background(), &HousingInvestmentRequest{Begin: test.begin, End: test.end, Component: test.component})
			}
			require.NoError(t, err)
			require.Len(t, result.Observations, 2)
			require.Equal(t, test.id, result.SeriesID)
			require.Equal(t, test.first, result.FirstPeriod)
			require.Equal(t, "US", result.Region)
			require.True(t, result.SeasonallyAdjusted)
			require.True(t, result.LevelAnnualized)
			require.False(t, result.GrowthAnnualized)
			require.Nil(t, result.PublishedAt)
			require.Nil(t, result.RevisionID)
			require.Equal(t, "current_visible_revision", result.DataVersion)
			got := result.Observations[0]
			require.Equal(t, test.level, *got.Level)
			require.Equal(t, test.previous, *got.PreviousBase.Level)
			require.Equal(t, test.yoy, *got.YoYBase.Level)
			require.InDelta(t, (test.level/test.previous-1)*100, *got.PreviousGrowth, 1e-10)
			require.InDelta(t, (test.level/test.yoy-1)*100, *got.YoY, 1e-10)
			require.Nil(t, result.Observations[1].Level)
			require.Nil(t, result.Observations[1].PreviousGrowth)
			require.Contains(t, result.Observations[1].Note, "当期水平缺失")
			if test.quarterly {
				require.Equal(t, "quarterly", result.Frequency)
				require.Equal(t, equipmentInvestmentCSVURL, result.Source)
				require.Equal(t, "2026Q1", got.PreviousBase.Period)
				require.Equal(t, "2025Q2", got.YoYBase.Period)
				if test.component == "real" {
					require.Equal(t, 2017, *result.ReferenceYear)
					require.Equal(t, "Y033RX", result.BEAAccountCode)
				} else {
					require.Nil(t, result.ReferenceYear)
					require.Equal(t, "Y033RC", result.BEAAccountCode)
				}
			} else {
				require.Equal(t, "monthly", result.Frequency)
				require.Equal(t, "not_applicable", result.PriceBasis)
				require.Equal(t, housingInvestmentCSVURL, result.Source)
				require.Nil(t, result.ReferenceYear)
				require.Equal(t, "202608", got.PreviousBase.Period)
				require.Equal(t, "202509", got.YoYBase.Period)
			}
		})
	}
}

func TestInvestmentCacheRefreshRecoveryAndOverflow(t *testing.T) {
	for _, quarterly := range []bool{false, true} {
		t.Run(fmt.Sprint(quarterly), func(t *testing.T) {
			raw := investmentSample(t, quarterly)
			var calls atomic.Int32
			var response atomic.Value
			response.Store(raw)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				_, _ = w.Write([]byte(response.Load().(string)))
			}))
			defer server.Close()
			client := NewClient(t.TempDir())
			client.housingInvestmentEndpoint, client.equipmentInvestmentEndpoint = server.URL, server.URL
			fetch := func(alternate bool) (*InvestmentResult, error) {
				if quarterly {
					basis := "real"
					if alternate {
						basis = "nominal"
					}
					return client.EquipmentInvestment(context.Background(), &EquipmentInvestmentRequest{Begin: "2026Q2", End: "2026Q2", PriceBasis: basis})
				}
				component := "housing-starts"
				if alternate {
					component = "building-permits"
				}
				return client.HousingInvestment(context.Background(), &HousingInvestmentRequest{Begin: "202609", End: "202609", Component: component})
			}
			first, err := fetch(false)
			require.NoError(t, err)
			alternate, err := fetch(true)
			require.NoError(t, err)
			require.Equal(t, first.FetchedAt, alternate.FetchedAt)
			require.EqualValues(t, 1, calls.Load())
			name := "us-investment-housing.json"
			if quarterly {
				name = "us-investment-equipment.json"
			}
			path := filepath.Join(client.CacheDir, name)
			valid, err := os.ReadFile(path)
			require.NoError(t, err)
			client.Refresh = true
			for _, bad := range []string{"DATE,WRONG\n2026-09-01,1\n", investmentOverflowCSV(quarterly)} {
				response.Store(bad)
				got, err := fetch(false)
				require.Error(t, err)
				require.Nil(t, got)
				after, err := os.ReadFile(path)
				require.NoError(t, err)
				require.Equal(t, valid, after, "校验失败不覆盖完整有效快照")
			}
			response.Store(raw)
			_, err = fetch(false)
			require.NoError(t, err)
			client.Refresh = false
			for _, age := range []time.Duration{7 * time.Hour, -time.Hour} {
				entry := csvCache{Source: server.URL, FetchedAt: time.Now().Add(-age), CSV: raw}
				encoded, err := json.Marshal(entry)
				require.NoError(t, err)
				require.NoError(t, os.WriteFile(path, encoded, 0600))
				before := calls.Load()
				_, err = fetch(false)
				require.NoError(t, err)
				require.Equal(t, before+1, calls.Load())
			}
			entry := csvCache{Source: server.URL, FetchedAt: time.Now(), CSV: "DATE,WRONG\n2026-09-01,1\n"}
			encoded, err := json.Marshal(entry)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(path, encoded, 0600))
			before := calls.Load()
			_, err = fetch(false)
			require.NoError(t, err)
			require.Equal(t, before+1, calls.Load(), "坏缓存重新请求并恢复")
			client.CacheDir = ""
			before = calls.Load()
			_, err = fetch(false)
			require.NoError(t, err)
			_, err = fetch(false)
			require.NoError(t, err)
			require.Equal(t, before+2, calls.Load())
		})
	}
}

func investmentOverflowCSV(quarterly bool) string {
	if quarterly {
		return "observation_date,Y033RC1Q027SBEA,Y033RX1Q020SBEA\n2026-01-01,1e-308,1\n2026-04-01,1e308,2\n"
	}
	return "observation_date,HOUST,PERMIT\n2026-08-01,1e-308,1\n2026-09-01,1e308,2\n"
}

func TestInvestmentCalendarGapsZerosAndCurrentQuarter(t *testing.T) {
	for _, quarterly := range []bool{false, true} {
		raw := investmentSample(t, quarterly)
		previous, yearAgo := "2026-08-01,", "2025-09-01,"
		if quarterly {
			previous, yearAgo = "2026-01-01,", "2025-04-01,"
		}
		var kept []string
		for _, line := range strings.Split(raw, "\n") {
			if !strings.HasPrefix(line, previous) && !strings.HasPrefix(line, yearAgo) {
				kept = append(kept, line)
			}
		}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(strings.Join(kept, "\n"))) }))
		client := NewClient("")
		client.housingInvestmentEndpoint, client.equipmentInvestmentEndpoint = server.URL, server.URL
		var got *InvestmentResult
		var err error
		if quarterly {
			got, err = client.EquipmentInvestment(context.Background(), &EquipmentInvestmentRequest{Begin: "2026Q2", End: "2026Q2"})
		} else {
			got, err = client.HousingInvestment(context.Background(), &HousingInvestmentRequest{Begin: "202609", End: "202609"})
		}
		require.NoError(t, err)
		require.NotNil(t, got.Observations[0].Level)
		require.Nil(t, got.Observations[0].PreviousGrowth)
		require.Nil(t, got.Observations[0].YoY)
		if quarterly {
			now := time.Now().UTC()
			period := fmt.Sprintf("%04dQ%d", now.Year(), (int(now.Month())-1)/3+1)
			current, err := client.EquipmentInvestment(context.Background(), &EquipmentInvestmentRequest{Begin: period, End: period})
			require.NoError(t, err)
			require.Nil(t, current.Observations[0].Level)
		}
		server.Close()
	}
	raw := "DATE,HOUST,PERMIT\n2026-08-01,0,0\n2026-09-01,1221,.\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(raw)) }))
	defer server.Close()
	client := NewClient("")
	client.housingInvestmentEndpoint = server.URL
	got, err := client.HousingInvestment(context.Background(), &HousingInvestmentRequest{Begin: "202609", End: "202609"})
	require.NoError(t, err)
	require.Nil(t, got.Observations[0].PreviousGrowth)
	require.Equal(t, 0.0, *got.Observations[0].PreviousBase.Level)
	permit, err := client.HousingInvestment(context.Background(), &HousingInvestmentRequest{Begin: "202609", End: "202609", Component: "building-permits"})
	require.NoError(t, err)
	require.Nil(t, permit.Observations[0].Level)
	require.Equal(t, ".", permit.Observations[0].RawLevel)
}

func TestInvestmentEarliestPeriodsAndRequests(t *testing.T) {
	for _, quarterly := range []bool{false, true} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(investmentSample(t, quarterly))) }))
		client := NewClient("")
		client.housingInvestmentEndpoint, client.equipmentInvestmentEndpoint = server.URL, server.URL
		for _, test := range []struct {
			component, period string
			level             float64
		}{{"housing-starts", "195901", 1657}, {"building-permits", "196001", 1092}, {"real", "2007Q1", 827.856}, {"nominal", "1947Q1", 14.927}} {
			if quarterly != (test.component == "real" || test.component == "nominal") {
				continue
			}
			var got *InvestmentResult
			var err error
			if quarterly {
				got, err = client.EquipmentInvestment(context.Background(), &EquipmentInvestmentRequest{Begin: test.period, End: test.period, PriceBasis: test.component})
			} else {
				got, err = client.HousingInvestment(context.Background(), &HousingInvestmentRequest{Begin: test.period, End: test.period, Component: test.component})
			}
			require.NoError(t, err)
			require.Equal(t, test.level, *got.Observations[0].Level)
			require.Nil(t, got.Observations[0].PreviousGrowth)
			require.Nil(t, got.Observations[0].YoY)
		}
		server.Close()
	}
	for _, req := range []*HousingInvestmentRequest{nil, {Begin: "195812", End: "195901"}, {Begin: "195912", End: "196001", Component: "building-permits"}, {Begin: "202613", End: "202613"}, {Begin: "202602", End: "202601"}, {Begin: "202601", End: "299901"}, {Begin: "202601", End: "202601", Component: "other"}} {
		require.Error(t, req.Validate())
	}
	for _, req := range []*EquipmentInvestmentRequest{nil, {Begin: "2006Q4", End: "2007Q1"}, {Begin: "1946Q4", End: "1947Q1", PriceBasis: "nominal"}, {Begin: "2026Q0", End: "2026Q1"}, {Begin: "2026Q2", End: "2026Q1"}, {Begin: "2026Q1", End: "2999Q1"}, {Begin: "2026Q1", End: "2026Q1", PriceBasis: "other"}} {
		require.Error(t, req.Validate())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewClient("").HousingInvestment(ctx, &HousingInvestmentRequest{Begin: "202609", End: "202609"})
	require.ErrorIs(t, err, context.Canceled)
}

func TestInvestmentRejectsCorruptionAndAcceptsColumnOrder(t *testing.T) {
	for _, quarterly := range []bool{false, true} {
		rows, err := csv.NewReader(strings.NewReader(investmentSample(t, quarterly))).ReadAll()
		require.NoError(t, err)
		encode := func(rows [][]string) string {
			var out strings.Builder
			writer := csv.NewWriter(&out)
			require.NoError(t, writer.WriteAll(rows))
			return out.String()
		}
		for _, test := range []string{"invalid-date", "duplicate-period", "negative", "nan", "infinity", "wrong-id", "duplicate-column", "all-missing", "wrong-frequency", "truncated"} {
			t.Run(fmt.Sprintf("%t/%s", quarterly, test), func(t *testing.T) {
				copyRows := make([][]string, len(rows))
				for i, row := range rows {
					copyRows[i] = append([]string(nil), row...)
				}
				switch test {
				case "invalid-date":
					copyRows[1][0] = "2026-02-30"
				case "duplicate-period":
					copyRows = append(copyRows, copyRows[1])
				case "negative":
					copyRows[1][1] = "-1"
				case "nan":
					copyRows[1][1] = "NaN"
				case "infinity":
					copyRows[1][1] = "+Inf"
				case "wrong-id":
					copyRows[0][1] = "HOUSTNSA"
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
				_, err := investmentParse(encode(copyRows), quarterly)
				require.Error(t, err)
			})
		}
		if quarterly {
			copyRows := append([][]string(nil), rows...)
			copyRows[1] = append([]string(nil), rows[1]...)
			copyRows[1][0] = "2026-02-01"
			_, err := investmentParse(encode(copyRows), true)
			require.Error(t, err, "季度数据只能在季度首月")
		}
		for _, row := range rows {
			row[0], row[1] = row[1], row[0]
		}
		rows[0][1] = "\ufeffDATE"
		_, err = investmentParse(encode(rows), quarterly)
		require.NoError(t, err)
	}
}
