//go:build windows || darwin

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

type bondTestResponse struct {
	Begin   string      `json:"begin"`
	End     string      `json:"end"`
	Latest  *bondDay    `json:"latest"`
	Results []bondDay   `json:"results"`
	Tenors  []bondTenor `json:"tenors"`
}

func treasuryFixture(entries string) string {
	return `<feed xmlns="http://www.w3.org/2005/Atom">` + entries + `</feed>`
}
func treasuryEntry(date string, yield float64, other string) string {
	return fmt.Sprintf(`<entry><content><m:properties xmlns:m="http://schemas.microsoft.com/ado/2007/08/dataservices/metadata" xmlns:d="http://schemas.microsoft.com/ado/2007/08/dataservices"><d:NEW_DATE>%sT00:00:00</d:NEW_DATE><d:BC_10YEAR>%.2f</d:BC_10YEAR>%s</m:properties></content></entry>`, date, yield, other)
}
func bondToday(t *testing.T) time.Time {
	t.Helper()
	location, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	today, err := time.Parse("2006-01-02", time.Now().In(location).Format("2006-01-02"))
	require.NoError(t, err)
	return today
}
func mockBondYears(t *testing.T, begin, end string, fixtures map[int]string, failed int) {
	t.Helper()
	start, err := time.Parse("2006-01-02", begin)
	require.NoError(t, err)
	stop, err := time.Parse("2006-01-02", end)
	require.NoError(t, err)
	today := bondToday(t)
	years := map[int]bool{}
	for year := start.AddDate(0, 0, -35).Year(); year <= stop.Year(); year++ {
		if year >= 1990 {
			years[year] = true
		}
	}
	for year := today.AddDate(0, 0, -35).Year(); year <= today.Year(); year++ {
		years[year] = true
	}
	ordered := make([]int, 0, len(years))
	for year := range years {
		ordered = append(ordered, year)
	}
	sort.Ints(ordered)
	for _, year := range ordered {
		mock := gock.New("https://home.treasury.gov").Get("/resource-center/data-chart-center/interest-rates/pages/xml").MatchParam("field_tdr_date_value", fmt.Sprint(year))
		if year == failed {
			mock.Reply(500).BodyString("failed")
			break
		}
		body := fixtures[year]
		if body == "" {
			body = treasuryFixture(treasuryEntry(fmt.Sprintf("%d-01-02", year), 4.0, ""))
		}
		mock.Reply(200).BodyString(body)
	}
}
func requestBond(t *testing.T, query string) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	(&desktopServer{dev: true}).Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/bond"+query, nil))
	return rr
}

func TestBondCrossYearAndLatest(t *testing.T) {
	defer gock.Off()
	today := bondToday(t)
	yesterday := today.AddDate(0, 0, -1).Format("2006-01-02")
	// 较早的历史范围与最新行情分开；首日的前值来自前一年。
	fixtures := map[int]string{
		2015:         treasuryFixture(treasuryEntry("2015-12-31", 4.0, "")),
		2016:         treasuryFixture(treasuryEntry("2016-01-04", 4.03, `<d:BC_1MONTH m:null="true"/><d:BC_3MONTH>0.00</d:BC_3MONTH>`)),
		today.Year(): treasuryFixture(treasuryEntry(today.Format("2006-01-02"), 4.12, "")),
	}
	previousYear := today.AddDate(0, 0, -1).Year()
	if previousYear == today.Year() {
		fixtures[previousYear] = treasuryFixture(treasuryEntry(yesterday, 4.1, "") + treasuryEntry(today.Format("2006-01-02"), 4.12, ""))
	} else {
		fixtures[previousYear] = treasuryFixture(treasuryEntry(yesterday, 4.1, ""))
	}
	mockBondYears(t, "2016-01-01", "2016-01-05", fixtures, 0)
	rr := requestBond(t, "?begin=2016-01-01&end=2016-01-05")
	require.Equal(t, 200, rr.Code, rr.Body.String())
	var response bondTestResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &response))
	require.Len(t, response.Results, 1)
	require.Len(t, response.Tenors, 11)
	require.Equal(t, today.Format("2006-01-02"), response.Latest.Date)
	require.InDelta(t, 3.0, *response.Results[0].ChangeBP, 1e-8)
	require.InDelta(t, 4.0, *response.Results[0].Previous10Y, 1e-8)
	require.Nil(t, response.Results[0].Yields["1m"])
	require.Equal(t, 0.0, *response.Results[0].Yields["3m"])
	require.InDelta(t, 2.0, *response.Latest.ChangeBP, 1e-8)
	require.True(t, gock.IsDone())
}

func TestBondEmptyRangeAndUnknownPrevious(t *testing.T) {
	defer gock.Off()
	today := bondToday(t)
	fixtures := map[int]string{2016: treasuryFixture(treasuryEntry("2016-01-04", 4.0, "")), today.Year(): treasuryFixture(treasuryEntry(today.Format("2006-01-02"), 4.12, ""))}
	mockBondYears(t, "2016-01-09", "2016-01-10", fixtures, 0)
	rr := requestBond(t, "?begin=2016-01-09&end=2016-01-10")
	require.Equal(t, 200, rr.Code)
	var response bondTestResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &response))
	require.NotNil(t, response.Results)
	require.Empty(t, response.Results)
	require.NotNil(t, response.Latest)
	// 多年前历史末日不能充当当前最新行情的前值。
	if today.AddDate(0, 0, -35).Year() == today.Year() {
		require.Nil(t, response.Latest.ChangeBP)
	}
}

func TestBondRejectsPartialYearFailure(t *testing.T) {
	defer gock.Off()
	mockBondYears(t, "2016-01-01", "2016-01-05", nil, 2016)
	rr := requestBond(t, "?begin=2016-01-01&end=2016-01-05")
	require.Equal(t, http.StatusBadGateway, rr.Code)
	require.Contains(t, rr.Body.String(), "美债收益率获取失败")
	require.True(t, gock.IsDone())
}

func TestBondValidationAndAuth(t *testing.T) {
	rr := httptest.NewRecorder()
	(&desktopServer{token: "secret"}).Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/v1/bond", nil))
	require.Equal(t, http.StatusForbidden, rr.Code)
	future := bondToday(t).AddDate(0, 0, 1).Format("2006-01-02")
	for _, query := range []string{"?begin=bad", "?end=2026-02-30", "?begin=2016-02-01&end=2016-01-01", "?begin=1989-01-01&end=1990-01-01", "?begin=2000-01-01&end=2006-01-01", "?end=" + future} {
		rr := requestBond(t, query)
		require.Equal(t, http.StatusBadRequest, rr.Code, query)
	}
}

func TestBondDefaultDates(t *testing.T) {
	defer gock.Off()
	end := bondToday(t).Format("2006-01-02")
	begin := bondToday(t).AddDate(0, 0, -30).Format("2006-01-02")
	mockBondYears(t, begin, end, nil, 0)
	rr := requestBond(t, "")
	require.Equal(t, 200, rr.Code)
	require.Contains(t, rr.Body.String(), `"begin":"`+begin+`"`)
	require.Contains(t, rr.Body.String(), `"end":"`+end+`"`)
	require.False(t, strings.Contains(rr.Body.String(), `"yields":{}`))
	require.True(t, gock.IsDone())
}
