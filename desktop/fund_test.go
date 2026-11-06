//go:build windows || darwin

package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func fundTestdata(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "provider", "eastmoney", "testdata", name))
	require.NoError(t, err)
	return string(data)
}

func TestFundOverview(t *testing.T) {
	defer gock.Off()
	srv := usServer(t)
	gock.New("https://fundmobapi.eastmoney.com").Get("/FundMNewApi/FundMNBasicInformation").Reply(200).BodyString(fundTestdata(t, "fund_basic_005827.json"))
	gock.New("https://fundf10.eastmoney.com").Get("/jbgk_005827.html").Reply(200).BodyString(fundTestdata(t, "fund_archive_005827.html"))
	rr := usRequest(t, srv, "/api/v1/fund/overview?code=of005827")
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var data map[string]any
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &data))
	require.Equal(t, "易方达蓝筹精选混合", data["name"])
	require.Equal(t, "中国银行", data["custodian"])
	require.Equal(t, 1.4164, data["nav"])
	require.Equal(t, 3.9, data["returns"].(map[string]any)["1w"])
	require.Empty(t, data["notes"])
}

// 没有这只基金时 404，两个数据源都失败时 502，不是基金代码时 400。
func TestFundOverviewErrors(t *testing.T) {
	defer gock.Off()
	srv := usServer(t)
	gock.New("https://fundmobapi.eastmoney.com").Get("/FundMNewApi/FundMNBasicInformation").Reply(200).BodyString(`{"Datas":null,"Success":true}`)
	gock.New("https://fundf10.eastmoney.com").Get("/jbgk_999999.html").Reply(200).BodyString(`<html></html>`)
	require.Equal(t, http.StatusNotFound, usRequest(t, srv, "/api/v1/fund/overview?code=OF999999").Code)

	gock.New("https://fundmobapi.eastmoney.com").Get("/FundMNewApi/FundMNBasicInformation").Reply(503)
	gock.New("https://fundf10.eastmoney.com").Get("/jbgk_005827.html").Reply(503)
	require.Equal(t, http.StatusBadGateway, usRequest(t, srv, "/api/v1/fund/overview?code=OF005827").Code)

	for _, code := range []string{"SH600519", "OF5827", "HK00700", ""} {
		require.Equal(t, http.StatusBadRequest, usRequest(t, srv, "/api/v1/fund/overview?code="+code).Code, code)
	}
}
