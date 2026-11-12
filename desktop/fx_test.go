package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestFXOptions(t *testing.T) {
	rr := macroRequest(t, "/api/v1/fx/options")
	require.Equal(t, http.StatusOK, rr.Code)
	var options struct {
		CrossPairs map[string][]string `json:"cross_pairs"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &options))
	for _, source := range []string{"fred", "cfets", "sina"} {
		require.Contains(t, options.CrossPairs[source], "HKD/CNY", source)
	}
	require.Contains(t, options.CrossPairs["sina"], "HKD/CNH")
	require.NotContains(t, options.CrossPairs["fred"], "HKD/CNH", "CNH只有新浪报价")
}

func TestFXData(t *testing.T) {
	defer gock.Off()
	// 命令会写缓存，用临时目录，不读写真实的 ~/.sec；Windows 读 USERPROFILE。
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	line := func(symbol, name string) string {
		return `var hq_str_` + symbol + `="10:45:32,6.655000,6.655100,6.655800,52,6.656000,6.657800,6.652600,6.655000,` + name + `,-0.01,-0.0008,0.0007,,6.99,6.65,,2026-10-30";`
	}
	gock.New("https://hq.sinajs.cn").Get("/").Reply(200).BodyString(line("fx_susdcny", "在岸") + "\n" + line("fx_susdcnh", "离岸") + "\n" + line("fx_susdhkd", "港币") + "\n" + line("fx_shkdcny", "港币人民币"))
	rr := macroRequest(t, "/api/v1/fx/data?command=quote")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var data struct {
		PriceType    string `json:"price_type"`
		Observations []struct {
			Pair  string   `json:"pair"`
			Value *float64 `json:"value"`
		} `json:"observations"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &data))
	require.Equal(t, "indicative-quote", data.PriceType)
	require.Len(t, data.Observations, 4)
	require.True(t, gock.IsDone())
}

func TestFXDataErrors(t *testing.T) {
	defer gock.Off()
	for path, status := range map[string]int{
		"/api/v1/fx/data?command=nope":                           http.StatusBadRequest,
		"/api/v1/fx/data?command=fixing&begin=2026":              http.StatusBadRequest,
		"/api/v1/fx/data?command=reference&at=9:00":              http.StatusBadRequest,
		"/api/v1/fx/data?command=cross&source=fred&pair=CNH/USD": http.StatusBadRequest,
	} {
		rr := macroRequest(t, path)
		require.Equal(t, status, rr.Code, path)
		require.Contains(t, rr.Body.String(), "error")
	}
	// 覆盖范围由命令校验，错误信息显示给用户。
	rr := macroRequest(t, "/api/v1/fx/data?command=fixing&begin=20000101&end=20000201")
	require.Equal(t, http.StatusBadGateway, rr.Code)
	require.Contains(t, rr.Body.String(), "error")
}

func TestFXRequiresToken(t *testing.T) {
	srv := &desktopServer{token: "secret"}
	for _, path := range []string{"/api/v1/fx/options", "/api/v1/fx/data?command=quote"} {
		rr := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusForbidden, rr.Code, path)
	}
}
