package federalreserve

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestParseStatementsAndExplicitTimeZones(t *testing.T) {
	for _, item := range []struct {
		file, date, stamp string
		lower, upper      float64
	}{
		{"statement-20081216.html", "2008-12-16", "", 0, .25},
		{"statement-20200303.html", "2020-03-03", "2020-03-03T15:00:00Z", 1, 1.25},
		{"statement-20200315.html", "2020-03-15", "2020-03-15T21:00:00Z", 0, .25},
		{"statement-20260916.html", "2026-09-16", "2026-09-16T18:00:00Z", 3.75, 4},
	} {
		t.Run(item.date, func(t *testing.T) {
			parsed, err := parseStatement(fixture(t, item.file))
			require.NoError(t, err)
			require.Equal(t, item.date, parsed.date)
			require.Equal(t, item.lower, parsed.lower)
			require.Equal(t, item.upper, parsed.upper)
			if item.stamp == "" {
				require.Nil(t, parsed.published)
			} else {
				require.Equal(t, item.stamp, parsed.published.UTC().Format(time.RFC3339))
			}
		})
	}
	for _, body := range []string{"unavailable", strings.Replace(string(fixture(t, "statement-20260916.html")), "September 16, 2026", "September 32, 2026", 1), strings.Replace(string(fixture(t, "statement-20260916.html")), "2:00 p.m.", "25:99 p.m.", 1)} {
		_, err := parseStatement([]byte(body))
		require.Error(t, err)
	}
}

func TestCalendarRestrictsOfficialStatementURLs(t *testing.T) {
	links, err := parseCalendar(fixture(t, "calendar.html"), calendarURL)
	require.NoError(t, err)
	require.Len(t, links, 6)
	require.Equal(t, "2008-12-16", links[0].date)
	require.NotContains(t, links[3].url, "a1.htm")
	_, err = parseCalendar([]byte("<html>error page</html>"), calendarURL)
	require.Error(t, err)
}

func TestReleaseFailureIsExplicitAndNotGuessed(t *testing.T) {
	defer gock.Off()
	for _, body := range []string{"error page", strings.Replace(string(fixture(t, "statement-20260916.html")), "September 16, 2026", "September 15, 2026", 1), strings.Replace(string(fixture(t, "statement-20260916.html")), "3-3/4", "3-1/2", 1)} {
		mockTable(t)
		mockCalendar(t)
		gock.New("https://www.federalreserve.gov").Get("/newsevents/pressreleases/monetary20260916a.htm").Reply(200).BodyString(body)
		result, err := NewClient("").TargetRate(context.Background(), &TargetRateRequest{Begin: "20260917", End: "20260917"})
		require.NoError(t, err)
		require.Len(t, result.Events, 1)
		event := result.Events[0]
		require.Equal(t, "unavailable", event.ReleaseStatus)
		require.Nil(t, event.AnnouncedDate)
		require.Nil(t, event.PublishedAt)
		require.Contains(t, event.ReleaseNote, "不可用")
		require.Equal(t, "2026-09-17", event.EffectiveDate)
	}
	require.True(t, gock.IsDone())
}

func TestReleaseFetchFailureIsDistinguished(t *testing.T) {
	for _, test := range []struct {
		name     string
		mock     func()
		failed   bool
		noteText string
	}{
		{"statement outage", func() {
			mockCalendar(t)
			gock.New("https://www.federalreserve.gov").Get("/newsevents/pressreleases/monetary20260916a.htm").Reply(503)
		}, true, "503"},
		{"statement missing", func() {
			mockCalendar(t)
			gock.New("https://www.federalreserve.gov").Get("/newsevents/pressreleases/monetary20260916a.htm").Reply(404)
		}, false, "404"},
		{"calendar outage hides history 404", func() {
			gock.New("https://www.federalreserve.gov").Get("/monetarypolicy/fomccalendars.htm").Reply(503)
			gock.New("https://www.federalreserve.gov").Get("/monetarypolicy/fomchistorical2026.htm").Reply(404)
		}, true, "503"},
		{"history page missing", func() {
			gock.New("https://www.federalreserve.gov").Get("/monetarypolicy/fomccalendars.htm").Reply(200).BodyString(`<div><strong>Statement:</strong><br><a href="/newsevents/pressreleases/monetary20200303a.htm">HTML</a></div>`)
			gock.New("https://www.federalreserve.gov").Get("/monetarypolicy/fomchistorical2026.htm").Reply(404)
		}, false, "404"},
		{"unmatched content", func() {
			mockCalendar(t)
			gock.New("https://www.federalreserve.gov").Get("/newsevents/pressreleases/monetary20260916a.htm").Reply(200).BodyString("error page")
		}, false, "不可用"},
	} {
		t.Run(test.name, func(t *testing.T) {
			defer gock.Off()
			mockTable(t)
			test.mock()
			result, err := NewClient("").TargetRate(context.Background(), &TargetRateRequest{Begin: "20260917", End: "20260917"})
			require.NoError(t, err, "普通查询仍保留调整表结果")
			event := result.Events[0]
			require.Equal(t, test.failed, event.ReleaseFetchFailed)
			require.Equal(t, "unavailable", event.ReleaseStatus)
			require.Contains(t, event.ReleaseNote, test.noteText)
			encoded, err := json.Marshal(event)
			require.NoError(t, err)
			require.Equal(t, test.failed, strings.Contains(string(encoded), "release_fetch_failed"), "未失败时原JSON不变")
			require.True(t, gock.IsDone())
		})
	}
}

func TestHistoricalCalendarFallback(t *testing.T) {
	defer gock.Off()
	current := `<div><strong>Statement:</strong><br><a href="/newsevents/pressreleases/monetary20260916a.htm">HTML</a></div>`
	gock.New("https://www.federalreserve.gov").Get("/monetarypolicy/fomccalendars.htm").Reply(200).BodyString(current)
	gock.New("https://www.federalreserve.gov").Get("/monetarypolicy/fomchistorical2020.htm").Reply(200).BodyString(string(fixture(t, "calendar.html")))
	mockStatement(t, "/newsevents/pressreleases/monetary20200303a.htm", "statement-20200303.html")
	mockStatement(t, "/newsevents/pressreleases/monetary20200315a.htm", "statement-20200315.html")
	resolver := newReleaseResolver(NewClient(""))
	first := &TargetRateEvent{EffectiveDate: "2020-03-04", LowerPercent: 1, UpperPercent: 1.25}
	second := &TargetRateEvent{EffectiveDate: "2020-03-16", LowerPercent: 0, UpperPercent: .25}
	require.NoError(t, resolver.enrich(context.Background(), first))
	require.NoError(t, resolver.enrich(context.Background(), second))
	require.Equal(t, "2020-03-03", *first.AnnouncedDate)
	require.Equal(t, "2020-03-15", *second.AnnouncedDate)
	require.True(t, gock.IsDone())
}

func TestSupplementaryAnnouncementsAreNotStatementCandidates(t *testing.T) {
	defer gock.Off()
	calendar := `<div><strong>Statement:</strong><br><a href="/newsevents/pressreleases/monetary20230201a.htm">HTML</a></div>
<div><a href="/newsevents/pressreleases/monetary20230201b.htm">Statement on Longer-Run Goals and Monetary Policy Strategy</a></div>
<div><a href="/newsevents/pressreleases/monetary20220504b.htm">Plans for Reducing the Size of the Federal Reserve's Balance Sheet</a></div>`
	links, err := parseCalendar([]byte(calendar), calendarURL)
	require.NoError(t, err)
	require.Len(t, links, 1)
	require.Equal(t, "https://www.federalreserve.gov/newsevents/pressreleases/monetary20230201a.htm", links[0].url)
	gock.New("https://www.federalreserve.gov").Get("/monetarypolicy/fomccalendars.htm").Reply(200).BodyString(calendar)
	statement := `<div id="article"><p class="article__time">February 01, 2023</p><p class="releaseTime">For release at 2:00 p.m. EST</p><p>The Committee decided to raise the target range for the federal funds rate to 4-1/2 to 4-3/4 percent.</p></div>`
	gock.New("https://www.federalreserve.gov").Get("/newsevents/pressreleases/monetary20230201a.htm").Reply(200).BodyString(statement)
	event := &TargetRateEvent{EffectiveDate: "2023-02-02", LowerPercent: 4.5, UpperPercent: 4.75}
	require.NoError(t, newReleaseResolver(NewClient("")).enrich(context.Background(), event))
	require.Equal(t, "2023-02-01", *event.AnnouncedDate)
	require.Equal(t, "available", event.ReleaseStatus)
	require.True(t, gock.IsDone())
}
