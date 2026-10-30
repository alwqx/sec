package mofcom

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func servicesSample(t *testing.T) servicesEnvelope {
	t.Helper()
	raw, err := os.ReadFile("testdata/services-monthly.json")
	require.NoError(t, err)
	var env servicesEnvelope
	require.NoError(t, json.Unmarshal(raw, &env))
	return env
}

func encodeServices(t *testing.T, env servicesEnvelope) string {
	t.Helper()
	raw, err := json.Marshal(env)
	require.NoError(t, err)
	return string(raw)
}

func TestParseServicesOfficialValues(t *testing.T) {
	env := servicesSample(t)
	_, rows, err := parseServices([]byte(encodeServices(t, env)), "202607", "202608", 1)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	row := rows[0]
	require.Equal(t, "202608", row.Period)
	require.Equal(t, "亿元人民币", row.CNY.Unit)
	require.Equal(t, 6387.6, *row.CNY.Total)
	require.Equal(t, 2452.5, *row.CNY.Exports)
	require.Equal(t, 3935.1, *row.CNY.Imports)
	require.Equal(t, -1482.6, *row.CNY.Balance)
	require.Equal(t, "亿美元", row.USD.Unit)
	require.Equal(t, 361.4, *row.USD.Exports)
	require.Equal(t, 579.8, *row.USD.Imports)
	require.Equal(t, -218.5, *row.USD.Balance, "官方差额独立舍入，不能用出口减进口替换")
	require.NotEqual(t, *row.USD.Exports-*row.USD.Imports, *row.USD.Balance)
	require.Equal(t, `"-218.5"`, string(row.RawValues["differenceUSD"]))
	require.Nil(t, row.PublishedAt)
	require.Nil(t, row.RevisionID)
}

func TestServicesMissingZeroAndInvalidResponses(t *testing.T) {
	env := servicesSample(t)
	env.Rows[0]["exportRMB"] = json.RawMessage(`null`)
	env.Rows[0]["exportUSD"] = json.RawMessage(`"0"`)
	_, rows, err := parseServices([]byte(encodeServices(t, env)), "202607", "202608", 1)
	require.NoError(t, err)
	require.Nil(t, rows[0].CNY.Exports)
	require.Equal(t, 0.0, *rows[0].USD.Exports)
	for name, change := range map[string]func(*servicesEnvelope){
		"missing field":    func(e *servicesEnvelope) { delete(e.Rows[0], "exportRMB") },
		"negative exports": func(e *servicesEnvelope) { e.Rows[0]["exportRMB"] = json.RawMessage(`"-1"`) },
		"nonfinite":        func(e *servicesEnvelope) { e.Rows[0]["differenceUSD"] = json.RawMessage(`"NaN"`) },
		"overflow":         func(e *servicesEnvelope) { e.Rows[0]["differenceUSD"] = json.RawMessage(`"1e999"`) },
		"boolean":          func(e *servicesEnvelope) { e.Rows[0]["exportUSD"] = json.RawMessage(`true`) },
		"bad period":       func(e *servicesEnvelope) { e.Rows[0]["mouth"] = json.RawMessage(`"202613"`) },
		"outside range":    func(e *servicesEnvelope) { e.Rows[0]["mouth"] = json.RawMessage(`"202609"`) },
		"duplicate period": func(e *servicesEnvelope) { e.Rows[1]["mouth"] = e.Rows[0]["mouth"] },
		"unsorted":         func(e *servicesEnvelope) { e.Rows[0], e.Rows[1] = e.Rows[1], e.Rows[0] },
		"wrong page":       func(e *servicesEnvelope) { e.Page = 2 },
		"wrong page count": func(e *servicesEnvelope) { e.Pages = 2 },
		"truncated":        func(e *servicesEnvelope) { e.Total = 3 },
		"negative total":   func(e *servicesEnvelope) { e.Total = -1 },
		"missing rows":     func(e *servicesEnvelope) { e.Rows = nil },
		"zero page size":   func(e *servicesEnvelope) { e.PageSize = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			e := servicesSample(t)
			change(&e)
			_, _, err := parseServices([]byte(encodeServices(t, e)), "202607", "202608", 1)
			require.Error(t, err)
		})
	}
	_, _, err = parseServices([]byte(`<html>error</html>`), "202607", "202608", 1)
	require.Error(t, err)
	_, _, err = parseServices([]byte(`{"rows":[],"pageSize":10,"pageNumber":1,"maxPageNum":1}`), "202607", "202608", 1)
	require.Error(t, err, "缺少total不能按无记录处理")
}

func TestServicesPaginationAndUnpublishedMonths(t *testing.T) {
	defer gock.Off()
	for i := 0; i < 2; i++ {
		e := servicesSample(t)
		e.Rows = e.Rows[i : i+1]
		e.Total, e.PageSize, e.Page, e.Pages = 2, 1, i+1, 2
		gock.New("https://data.mofcom.gov.cn").Get("/datamofcom/front/fwmy/mouthly").MatchParam("start_date", "202606").MatchParam("end_date", "202608").MatchParam("pageNumber", string(rune('1'+i))).Reply(200).BodyString(encodeServices(t, e))
	}
	result, err := NewClient("").Services(context.Background(), &ServicesRequest{Begin: "202606", End: "202608"})
	require.NoError(t, err)
	require.Len(t, result.Observations, 3)
	require.Nil(t, result.Observations[0].CNY)
	require.Nil(t, result.Observations[0].FetchedAt)
	require.NotEmpty(t, result.Observations[0].Note)
	require.Equal(t, "202607", result.FirstPeriod)
	require.Equal(t, "202608", result.LastPeriod)
	require.Equal(t, 2452.5, *result.Observations[2].CNY.Exports)
	require.NotEmpty(t, result.Observations[2].Source)
	require.NotNil(t, result.Observations[2].FetchedAt)
	require.Nil(t, result.Observations[2].PublishedAt)
	require.True(t, gock.IsDone())

	gock.New("https://data.mofcom.gov.cn").Get("/datamofcom/front/fwmy/mouthly").Reply(200).BodyString(`{"rows":[],"total":0,"pageSize":10,"pageNumber":1,"maxPageNum":1}`)
	result, err = NewClient("").Services(context.Background(), &ServicesRequest{Begin: "201601", End: "201602"})
	require.NoError(t, err)
	require.Len(t, result.Observations, 2)
	require.Empty(t, result.FirstPeriod)
	require.Nil(t, result.Observations[1].USD)
	require.True(t, gock.IsDone())
}

func TestServicesRejectsChangingAndOverlappingPages(t *testing.T) {
	for _, mode := range []string{"total", "overlap", "http"} {
		t.Run(mode, func(t *testing.T) {
			defer gock.Off()
			first := servicesSample(t)
			first.Rows = first.Rows[:1]
			first.Total, first.PageSize, first.Pages = 2, 1, 2
			gock.New("https://data.mofcom.gov.cn").Get("/datamofcom/front/fwmy/mouthly").MatchParam("pageNumber", "1").Reply(200).BodyString(encodeServices(t, first))
			second := servicesSample(t)
			second.Rows = second.Rows[1:]
			second.Total, second.PageSize, second.Pages, second.Page = 2, 1, 2, 2
			if mode == "total" {
				second.Total, second.Pages = 3, 3
			}
			if mode == "overlap" {
				second.Rows[0]["mouth"] = json.RawMessage(`"202608"`)
			}
			status := 200
			if mode == "http" {
				status = 503
			}
			gock.New("https://data.mofcom.gov.cn").Get("/datamofcom/front/fwmy/mouthly").MatchParam("pageNumber", "2").Reply(status).BodyString(encodeServices(t, second))
			_, err := NewClient("").Services(context.Background(), &ServicesRequest{Begin: "202607", End: "202608"})
			require.Error(t, err)
			require.True(t, gock.IsDone())
		})
	}
}

func TestServicesCacheValidationAndCancellation(t *testing.T) {
	defer gock.Off()
	client := NewClient(t.TempDir())
	req := &ServicesRequest{Begin: "202607", End: "202608"}
	gock.New("https://data.mofcom.gov.cn").Get("/datamofcom/front/fwmy/mouthly").Reply(200).BodyString(encodeServices(t, servicesSample(t)))
	first, err := client.Services(context.Background(), req)
	require.NoError(t, err)
	second, err := client.Services(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, first, second)
	files, err := filepath.Glob(filepath.Join(client.CacheDir, "*.json"))
	require.NoError(t, err)
	require.Len(t, files, 1)
	before, err := os.ReadFile(files[0])
	require.NoError(t, err)
	client.Refresh = true
	gock.New("https://data.mofcom.gov.cn").Get("/datamofcom/front/fwmy/mouthly").Reply(200).BodyString(`{"rows":[]}`)
	_, err = client.Services(context.Background(), req)
	require.Error(t, err)
	after, err := os.ReadFile(files[0])
	require.NoError(t, err)
	require.Equal(t, before, after)
	client.Refresh = false
	_, err = client.Services(context.Background(), req)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.Services(ctx, req)
	require.ErrorIs(t, err, context.Canceled)
	require.True(t, gock.IsDone())
}

func TestServicesRequestValidation(t *testing.T) {
	for _, req := range []*ServicesRequest{nil, {Begin: "201512", End: "201601"}, {Begin: "202613", End: "202601"}, {Begin: "202608", End: "202607"}, {Begin: "202601", End: time.Now().AddDate(1, 0, 0).Format("200601")}} {
		require.Error(t, req.Validate())
	}
}
