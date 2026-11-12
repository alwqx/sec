package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alwqx/sec/provider/sina"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func commodityRequest(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	(&desktopServer{dev: true}).Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
	return rr
}

func TestCommodities(t *testing.T) {
	defer gock.Off()
	body := ""
	for _, line := range []string{
		`var hq_str_nf_SC0="上海原油连续,150000,717.6,719.7,677.3,698.2,698.2,698.5,698.2,694.9,745.2,1,5,24109,177041,沪,上海原油,2026-11-03,1";`,
		`var hq_str_nf_CU0="铜连续,150000,1,1,1,1,1,1,106090,1,107580,1,1,1,1,沪,铜,2026-11-03,1";`,
		`var hq_str_nf_I0="铁矿石连续,150456,1,1,1,1,1,1,671,1,676,1,1,1,1,连,铁矿石,2026-11-03,1";`,
		`var hq_str_nf_RB0="螺纹钢连续,150000,1,1,1,1,1,1,2991,1,3019,1,1,1,1,沪,螺纹钢,2026-11-03,1";`,
		`var hq_str_nf_M0="豆粕连续,150456,1,1,1,1,1,1,3147,1,3187,1,1,1,1,连,豆粕,2026-11-03,1";`,
		`var hq_str_hf_CL="98.456,,1,1,98.69,96.12,19:28:50,96.46,96.3,0,7,4,2026-11-03,纽约原油,0";`,
		`var hq_str_hf_OIL="106.495,,1,1,107.24,104.27,19:28:58,104.77,104.45,0,5,5,2026-11-03,布伦特原油,0";`,
		`var hq_str_hf_HG="635.804,,1,1,655.25,631.8,19:28:59,656.75,653.75,0,4,1,2026-11-03,美铜,0";`,
	} {
		body += line + "\n"
	}
	gock.New("https://hq.sinajs.cn").Get("/list=").Reply(200).BodyString(body)
	rr := commodityRequest(t, "/api/v1/commodity")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var data struct {
		Results []sina.CommodityQuote `json:"results"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &data))
	require.Len(t, data.Results, 8)
	require.Equal(t, "sc", data.Results[0].ID)
	require.Equal(t, 745.2, *data.Results[0].PrevSettle)
	require.Equal(t, "美分/磅", data.Results[7].Unit)

	// 个别品种失败时其余照常返回；全部失败时 502。
	gock.New("https://hq.sinajs.cn").Get("/list=").Reply(200).BodyString(body[:len(body)/2])
	rr = commodityRequest(t, "/api/v1/commodity")
	require.Equal(t, http.StatusOK, rr.Code)
	var partial struct {
		Results []sina.CommodityQuote `json:"results"`
		Failed  []string              `json:"failed"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &partial))
	require.NotEmpty(t, partial.Results)
	require.Contains(t, partial.Failed, "COMEX 铜：新浪没有返回行情")
	gock.New("https://hq.sinajs.cn").Get("/list=").Reply(200).BodyString("")
	require.Equal(t, http.StatusBadGateway, commodityRequest(t, "/api/v1/commodity").Code)
	gock.New("https://hq.sinajs.cn").Get("/list=").Reply(502)
	require.Equal(t, http.StatusBadGateway, commodityRequest(t, "/api/v1/commodity").Code)
}

func TestCommodityHistory(t *testing.T) {
	defer gock.Off()
	t.Cleanup(func() { commodityCache.entries = map[string]commodityEntry{} })
	gock.New("https://stock2.finance.sina.com.cn").Get("/futures/api/jsonp.php/var.t=/InnerFuturesNewService.getDailyKLine").MatchParam("symbol", "CU0").
		Reply(200).BodyString(`var t=([{"d":"2026-11-02","o":"107720","h":"108010","l":"107160","c":"107810","v":"104635","s":"107580"},{"d":"2026-11-03","o":"107810","h":"108470","l":"106090","c":"106090","v":"126807","s":"107230"}]);`)
	rr := commodityRequest(t, "/api/v1/commodity/history?id=CU&begin=2026-11-03&end=2026-11-03")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var data struct {
		Commodity sina.Commodity      `json:"commodity"`
		Results   []sina.CommodityBar `json:"results"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &data))
	require.Equal(t, "铜", data.Commodity.Name)
	require.Len(t, data.Results, 1)
	require.InDelta(t, 106090-107580, *data.Results[0].Change, 1e-9)
	require.True(t, gock.IsDone())

	// 10 分钟内再查同一品种的另一个区间，用缓存，不再请求新浪。
	rr = commodityRequest(t, "/api/v1/commodity/history?id=cu&begin=2026-11-02&end=2026-11-03")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &data))
	require.Len(t, data.Results, 2)

	require.Equal(t, http.StatusBadRequest, commodityRequest(t, "/api/v1/commodity/history?id=au").Code)
	require.Equal(t, http.StatusBadRequest, commodityRequest(t, "/api/v1/commodity/history?id=cu&begin=2026-11-05&end=2026-11-01").Code)
}
