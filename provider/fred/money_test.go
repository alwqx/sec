package fred

import (
	"context"
	"encoding/json"
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

func moneySample(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("testdata/money.csv")
	require.NoError(t, err)
	return string(raw)
}

func TestMoneyOfficialValuesAndCache(t *testing.T) {
	raw := moneySample(t)
	var calls atomic.Int32
	var broken atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, "M2SL", r.URL.Query().Get("id"))
		require.Equal(t, "curl", r.UserAgent())
		if broken.Load() {
			_, _ = w.Write([]byte("DATE,M2NS\n2026-09-01,23339.6\n"))
			return
		}
		_, _ = w.Write([]byte(raw))
	}))
	defer server.Close()
	client := NewClient(t.TempDir())
	client.moneyEndpoint = server.URL + "?id=M2SL"
	req := &MoneyRequest{Begin: "202609", End: "202610"}
	got, err := client.Money(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, got.Observations, 2)
	require.Equal(t, "US", got.Region)
	require.Equal(t, "M2SL", got.SeriesID)
	require.Equal(t, "monthly", got.Frequency)
	require.Equal(t, "nominal-money-stock", got.Measure)
	require.Equal(t, "monthly-average", got.PeriodBasis)
	require.Equal(t, "十亿美元", got.Unit)
	require.True(t, got.SeasonallyAdjusted)
	require.False(t, got.LevelAnnualized)
	require.False(t, got.GrowthAnnualized)
	require.Equal(t, moneyCSVURL, got.Source)
	require.Equal(t, "FRED", got.Distributor)
	require.Equal(t, "latest-available", got.DataVersion)
	require.Nil(t, got.PublishedAt)
	require.Nil(t, got.RevisionID)
	require.Contains(t, got.MethodologyNotes[0], "不构成M2总量断点")
	require.Contains(t, got.MethodologyNotes[1], "2026-07-28")
	sep := got.Observations[0]
	require.Equal(t, 23413.9, *sep.Level)
	require.Equal(t, "23413.9", sep.RawValue)
	require.Equal(t, "202608", sep.MoMBase.Period)
	require.Equal(t, 23300.9, *sep.MoMBase.Level)
	require.Equal(t, "202509", sep.YoYBase.Period)
	require.Equal(t, 22149.2, *sep.YoYBase.Level)
	require.InDelta(t, (23413.9/23300.9-1)*100, *sep.MoM, 1e-10)
	require.InDelta(t, (23413.9/22149.2-1)*100, *sep.YoY, 1e-10)
	require.Nil(t, got.Observations[1].Level)
	require.Nil(t, got.Observations[1].MoM)
	first, err := client.Money(context.Background(), &MoneyRequest{Begin: "195901", End: "195901"})
	require.NoError(t, err)
	require.Equal(t, 286.6, *first.Observations[0].Level)
	require.Nil(t, first.Observations[0].MoM)
	require.Nil(t, first.Observations[0].YoY)
	require.Equal(t, got.FetchedAt, first.FetchedAt)
	may, err := client.Money(context.Background(), &MoneyRequest{Begin: "202005", End: "202005"})
	require.NoError(t, err)
	require.Equal(t, 17931.9, *may.Observations[0].Level)
	require.Equal(t, 17064.0, *may.Observations[0].MoMBase.Level)
	require.NotNil(t, may.Observations[0].YoY, "不将M1重分类误当成M2断点")
	require.EqualValues(t, 1, calls.Load())
	client.Refresh = true
	_, err = client.Money(context.Background(), req)
	require.NoError(t, err)
	cachePath := filepath.Join(client.CacheDir, "money.json")
	before, err := os.ReadFile(cachePath)
	require.NoError(t, err)
	broken.Store(true)
	got, err = client.Money(context.Background(), req)
	require.Error(t, err)
	require.Nil(t, got)
	after, err := os.ReadFile(cachePath)
	require.NoError(t, err)
	require.Equal(t, before, after, "不以错误季调口径更新有效缓存")
	broken.Store(false)
	client.Refresh = false
	for _, cache := range []csvCache{
		{Source: client.moneyEndpoint, FetchedAt: time.Now().Add(-7 * time.Hour), CSV: raw},
		{Source: client.moneyEndpoint, FetchedAt: time.Now().Add(time.Hour), CSV: raw},
		{Source: client.moneyEndpoint, FetchedAt: time.Now(), CSV: "invalid"},
		{Source: "wrong-source", FetchedAt: time.Now(), CSV: raw},
	} {
		data, err := json.Marshal(cache)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(cachePath, data, 0600))
		_, err = client.Money(context.Background(), req)
		require.NoError(t, err)
	}
	client.CacheDir = ""
	_, err = client.Money(context.Background(), req)
	require.NoError(t, err)
	require.EqualValues(t, 8, calls.Load())
}

func TestMoneyStrictBaselinesMissingAndZero(t *testing.T) {
	raw := "M2SL,DATE\n100,2024-01-01\n0,2025-01-01\n110,2025-02-01\n.,2025-03-01\n120,2025-04-01\n130,2025-06-01\n,2025-07-01\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(raw)) }))
	defer server.Close()
	client := NewClient("")
	client.moneyEndpoint = server.URL
	got, err := client.Money(context.Background(), &MoneyRequest{Begin: "202501", End: "202507"})
	require.NoError(t, err)
	require.Len(t, got.Observations, 7)
	require.Zero(t, *got.Observations[0].Level)
	require.Equal(t, -100.0, *got.Observations[0].YoY)
	require.Nil(t, got.Observations[1].MoM)
	require.Zero(t, *got.Observations[1].MoMBase.Level)
	require.Nil(t, got.Observations[2].Level)
	require.Equal(t, ".", got.Observations[2].RawValue)
	require.Nil(t, got.Observations[3].MoM)
	require.Nil(t, got.Observations[4].Level)
	require.Nil(t, got.Observations[5].MoM)
	require.Equal(t, "202505", got.Observations[5].MoMBase.Period)
	require.Nil(t, got.Observations[5].YoY)
	require.Nil(t, got.Observations[6].Level)
}

func TestMoneyCSVAndRequestValidation(t *testing.T) {
	values, err := parseMoney(moneySample(t))
	require.NoError(t, err)
	require.Len(t, values, 813)
	valid := "observation_date,M2SL\n2025-01-01,100\n"
	for _, raw := range []string{
		"<html>error</html>", "DATE,M2SL\n", "DATE,M2SL\n2025-01-01,.\n", "DATE,DATE\n2025-01-01,100\n",
		strings.Replace(valid, "M2SL", "M2NS", 1), strings.Replace(valid, "2025-01-01", "2025-01-02", 1),
		strings.Replace(valid, "2025-01-01", "1958-12-01", 1), valid + "2025-01-01,200\n", valid + "2025-02-01\n",
		strings.Replace(valid, "100", "NaN", 1), strings.Replace(valid, "100", "+Inf", 1), strings.Replace(valid, "100", "-1", 1),
	} {
		_, err := parseMoney(raw)
		require.Error(t, err, raw)
	}
	values, err = parseMoney("\ufeffM2SL,DATE\n0,1959-01-01\n")
	require.NoError(t, err)
	require.Zero(t, *values["195901"].Level)
	client := NewClient("")
	for _, req := range []*MoneyRequest{nil, {Begin: "195812", End: "195901"}, {Begin: "202513", End: "202601"}, {Begin: "202502", End: "202501"}, {Begin: "202501", End: "299901"}, {Begin: "20251", End: "202501"}} {
		got, err := client.Money(context.Background(), req)
		require.Error(t, err)
		require.Nil(t, got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := client.Money(ctx, &MoneyRequest{Begin: "202501", End: "202502"})
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, got)
}

func TestMoneyCacheIsolation(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/money" {
			_, _ = w.Write([]byte(moneySample(t)))
		} else {
			_, _ = w.Write([]byte(realIncomeSample(t)))
		}
	}))
	defer server.Close()
	client := NewClient(t.TempDir())
	client.moneyEndpoint, client.incomeEndpoint = server.URL+"/money", server.URL+"/income"
	for iteration := 0; iteration < 2; iteration++ {
		money, err := client.Money(context.Background(), &MoneyRequest{Begin: "202609", End: "202609"})
		require.NoError(t, err)
		require.Equal(t, 23413.9, *money.Observations[0].Level)
		income, err := client.RealIncome(context.Background(), &RealIncomeRequest{Begin: "202608", End: "202608"})
		require.NoError(t, err)
		require.Equal(t, 18410.5, *income.Observations[0].Level)
	}
	require.EqualValues(t, 2, calls.Load())
}
