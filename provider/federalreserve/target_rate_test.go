package federalreserve

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return body
}

func mockTable(t *testing.T) {
	t.Helper()
	gock.New("https://www.federalreserve.gov").Get("/monetarypolicy/openmarket.htm").Reply(200).BodyString(string(fixture(t, "openmarket.html")))
}

func mockCalendar(t *testing.T) {
	t.Helper()
	gock.New("https://www.federalreserve.gov").Get("/monetarypolicy/fomccalendars.htm").Reply(200).BodyString(string(fixture(t, "calendar.html")))
}

func mockStatement(t *testing.T, path, filename string) {
	t.Helper()
	gock.New("https://www.federalreserve.gov").Get(path).Reply(200).BodyString(string(fixture(t, filename)))
}

func TestOfficialTargetTable(t *testing.T) {
	events, err := parseTable(fixture(t, "openmarket.html"))
	require.NoError(t, err)
	require.Greater(t, len(events), 30)
	require.Equal(t, "2008-12-16", events[0].EffectiveDate)
	require.Nil(t, events[0].LowerChangeBasisPoints)
	require.Nil(t, events[0].UpperChangeBasisPoints)
	require.Equal(t, "75-100", events[0].Raw.Decrease)
	require.Equal(t, 0.0, events[0].LowerPercent)
	require.Equal(t, 0.25, events[0].UpperPercent)
	require.Equal(t, "2026-09-17", events[len(events)-1].EffectiveDate)
	require.Equal(t, 3.75, events[len(events)-1].LowerPercent)
	require.Equal(t, 4.0, events[len(events)-1].UpperPercent)
	for _, event := range events {
		if event.EffectiveDate == "2020-03-04" {
			require.Equal(t, "2019-10-31", event.PreviousEffectiveDate)
			require.Equal(t, -50.0, *event.LowerChangeBasisPoints)
			require.Equal(t, -50.0, *event.UpperChangeBasisPoints)
			require.Contains(t, event.Raw.Date, "March 4")
		}
	}
}

func TestTargetRateEmergencyDatesAndBaseline(t *testing.T) {
	defer gock.Off()
	mockTable(t)
	mockCalendar(t)
	mockStatement(t, "/newsevents/pressreleases/monetary20200303a.htm", "statement-20200303.html")
	mockStatement(t, "/newsevents/pressreleases/monetary20200315a.htm", "statement-20200315.html")
	result, err := NewClient("").TargetRate(context.Background(), &TargetRateRequest{Begin: "20200301", End: "20200316"})
	require.NoError(t, err)
	require.Equal(t, "target-rate", result.Indicator)
	require.Len(t, result.Events, 2)
	require.Equal(t, "2019-10-31", result.Baseline.EffectiveDate)
	require.Equal(t, "unavailable", result.Baseline.ReleaseStatus)
	require.Contains(t, result.Baseline.ReleaseNote, "未找到")
	first, second := result.Events[0], result.Events[1]
	require.Equal(t, "2020-03-04", first.EffectiveDate)
	require.Equal(t, "2020-03-03", *first.AnnouncedDate)
	require.Equal(t, "2020-03-03T10:00:00-05:00", first.PublishedAt.Format(time.RFC3339))
	require.Equal(t, "2020-03-16", second.EffectiveDate)
	require.Equal(t, "2020-03-15", *second.AnnouncedDate)
	require.Equal(t, "2020-03-15T17:00:00-04:00", second.PublishedAt.Format(time.RFC3339))
	require.Equal(t, -100.0, *second.LowerChangeBasisPoints)
	require.Equal(t, "available", second.ReleaseStatus)
	require.Equal(t, second, result.Current)
	require.NotZero(t, first.FetchedAt)
	require.NotNil(t, first.StatementFetchedAt)
	require.Contains(t, first.StatementRaw.Decision, "1‑1/4")
	require.True(t, gock.IsDone())
}

func TestTargetRateNoChangeAndNoFutureApplication(t *testing.T) {
	defer gock.Off()
	for _, dates := range [][2]string{{"20200320", "20200322"}, {"20200316", "20200316"}} {
		mockTable(t)
		mockCalendar(t)
		mockStatement(t, "/newsevents/pressreleases/monetary20200315a.htm", "statement-20200315.html")
		if dates[0] == "20200316" {
			mockStatement(t, "/newsevents/pressreleases/monetary20200303a.htm", "statement-20200303.html")
		}
		result, err := NewClient("").TargetRate(context.Background(), &TargetRateRequest{Begin: dates[0], End: dates[1]})
		require.NoError(t, err)
		require.Equal(t, "2020-03-16", result.Current.EffectiveDate)
		require.Equal(t, 0.25, result.Current.UpperPercent)
		if dates[0] == "20200320" {
			require.Empty(t, result.Events)
			require.Same(t, result.Baseline, result.Current)
		} else {
			require.Len(t, result.Events, 1)
		}
	}
	mockTable(t)
	mockCalendar(t)
	result, err := NewClient("").TargetRate(context.Background(), &TargetRateRequest{Begin: "20260916", End: "20260916"})
	require.NoError(t, err)
	require.Empty(t, result.Events)
	require.Equal(t, "2025-12-11", result.Current.EffectiveDate)
	require.Equal(t, 3.50, result.Current.LowerPercent)
	require.True(t, gock.IsDone())
}

func TestTargetRateInitialRangeAndUnavailableTime(t *testing.T) {
	defer gock.Off()
	mockTable(t)
	mockCalendar(t)
	mockStatement(t, "/newsevents/press/monetary/20081216b.htm", "statement-20081216.html")
	result, err := NewClient("").TargetRate(context.Background(), &TargetRateRequest{Begin: earliest, End: earliest})
	require.NoError(t, err)
	require.Nil(t, result.Baseline)
	require.Len(t, result.Events, 1)
	event := result.Events[0]
	require.Equal(t, "2008-12-16", *event.AnnouncedDate)
	require.Nil(t, event.PublishedAt)
	require.Equal(t, "date-only", event.ReleaseStatus)
	require.Nil(t, event.LowerChangeBasisPoints)
	require.Equal(t, "75-100", event.Raw.Decrease)
	encoded, err := json.Marshal(event)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"published_at":null`)
	require.True(t, gock.IsDone())
}

const smallTable = `<h4>FOMC's target federal funds rate or range</h4><h4>2015</h4><table><thead><tr><th>Date</th><th>Increase</th><th>Decrease</th><th>Level (%)</th></tr></thead><tbody><tr><td>December 17</td><td>25</td><td>0</td><td>0.25-0.50</td></tr></tbody></table><h4>2008</h4><table><thead><tr><th>Date</th><th>Increase</th><th>Decrease</th><th>Level (%)</th></tr></thead><tbody><tr><td>December 16</td><td>...</td><td>75-100</td><td>0-0.25</td></tr></tbody></table>`

func TestTargetTableRejectsInvalid(t *testing.T) {
	for name, body := range map[string]string{
		"错误页":      "<html>temporarily unavailable</html>",
		"列变化":      strings.Replace(smallTable, "Level (%)", "Level", 1),
		"区间缺失":     strings.Replace(smallTable, "0.25-0.50", "0.50", 1),
		"区间倒置":     strings.Replace(smallTable, "0.25-0.50", "0.50-0.25", 1),
		"单值冒充区间":   strings.Replace(smallTable, "0.25-0.50", "0.50-0.50", 1),
		"日期错误":     strings.Replace(smallTable, "December 17", "December 32", 1),
		"起点缺失":     strings.Replace(smallTable, "December 16", "December 15", 1),
		"基点不一致":    strings.Replace(smallTable, "<td>25</td>", "<td>50</td>", 1),
		"加息降息同时非零": strings.Replace(smallTable, "<td>0</td>", "<td>25</td>", 1),
		"重复日期":     smallTable + `<h4>2015</h4>` + strings.Split(strings.Split(smallTable, "<h4>2015</h4>")[1], "<h4>2008</h4>")[0],
	} {
		t.Run(name, func(t *testing.T) { _, err := parseTable([]byte(body)); require.Error(t, err) })
	}
}

func TestTargetRequestValidationAndCancellation(t *testing.T) {
	for _, req := range []*TargetRateRequest{nil, {}, {Begin: "20081215", End: earliest}, {Begin: "20250230", End: "20250301"}, {Begin: "20250102", End: "20250101"}, {Begin: "20250101", End: "29990101"}, {Begin: "2025-01-01", End: "20250102"}} {
		require.Error(t, req.Validate())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewClient("").TargetRate(ctx, &TargetRateRequest{Begin: earliest, End: earliest})
	require.ErrorIs(t, err, context.Canceled)
}

func TestTargetRateMainSourceFailure(t *testing.T) {
	defer gock.Off()
	for _, status := range []int{503, 200} {
		gock.New("https://www.federalreserve.gov").Get("/monetarypolicy/openmarket.htm").Reply(status).BodyString("<html>unavailable</html>")
		_, err := NewClient("").TargetRate(context.Background(), &TargetRateRequest{Begin: earliest, End: earliest})
		require.Error(t, err)
	}
	require.True(t, gock.IsDone())
}
