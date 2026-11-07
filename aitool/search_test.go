package aitool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func callSearchJSON(t *testing.T, input string) (*Result, *searchData, error) {
	t.Helper()
	result, err := SearchTool().Call(context.Background(), json.RawMessage(input))
	if err != nil {
		return nil, nil, err
	}
	data, ok := result.Data.(*searchData)
	require.True(t, ok)
	return result, data, nil
}

func TestSearch(t *testing.T) {
	defer gock.Off()
	// 新浪的真实返回：场外 ADR 排在前面，港股指数代码是小写字母。
	suggestKey("tx").Reply(200).BodyString(`var suggestvalue="腾讯,41,tcehy,tcehy,腾讯(ADR),,腾讯(ADR),99,1,ADR|ESG,,,;` +
		`腾讯控股,31,00700,00700,腾讯控股,,腾讯控股,99,1,ESG,,,;` +
		`腾讯济安,11,000847,sh000847,腾讯济安,,腾讯济安,99,1,,,,;` +
		`I300,11,399416,sz399416,I300,,I300,99,1,,,,;` +
		`HSI,33,hsi,hsi,恒生指数,,恒生指数,99,1,,,,;` +
		`立讯精密,11,002475,sz002475,立讯精密,,立讯精密,99,1,ESG,,,;` +
		`沪深300ETF华泰柏瑞,22,510300,of510300,沪深300ETF华泰柏瑞,,沪深300ETF华泰柏瑞,99,1,,,,;` +
		`已退市,11,600001,sh600001,已退市,,已退市,99,0,,,,";`)

	result, data, err := callSearchJSON(t, `{"query":" tx "}`)
	require.NoError(t, err)
	require.True(t, gock.IsDone())
	require.Equal(t, "search_security", result.Tool)
	require.Equal(t, "新浪财经搜索", result.Source)
	require.Empty(t, result.AsOf, "搜索结果没有数据日期")
	require.Equal(t, "tx", data.Query)
	require.Equal(t, 7, data.Total, "不在市的不算")
	require.Equal(t, []searchItem{
		{ExCode: "HK00700", Name: "腾讯控股", Market: "hk", Type: "stock"},
		{ExCode: "SH000847", Name: "腾讯济安", Market: "sse", Type: "index"},
		{ExCode: "SZ399416", Name: "I300", Market: "szse", Type: "index"},
		{ExCode: "HKhsi", Name: "恒生指数", Market: "hk", Type: "index"},
		{ExCode: "SZ002475", Name: "立讯精密", Market: "szse", Type: "stock"},
		{ExCode: "OF510300", Name: "沪深300ETF华泰柏瑞", Market: "fund", Type: "fund"},
		{ExCode: "$TCEHY", Name: "腾讯(ADR)", Market: "us", Type: "stock"},
	}, data.Results, "场外 ADR 排到最后")
	require.Equal(t, []string{"get_quote 目前查不到港股指数（如 HKhsi）的行情"}, result.Notes)
}

func TestSearchTruncated(t *testing.T) {
	defer gock.Off()
	lines := make([]string, 0, 12)
	for i := range 12 {
		code := fmt.Sprintf("3000%02d", i)
		lines = append(lines, fmt.Sprintf("sz%s,11,%s,sz%s,股票%d,,股票%d,99,1,,,,", code, code, code, i, i))
	}
	suggestKey("300").Reply(200).BodyString(`var suggestvalue="` + strings.Join(lines, ";") + `";`)

	result, data, err := callSearchJSON(t, `{"query":"300"}`)
	require.NoError(t, err)
	require.Len(t, data.Results, maxSearchResults)
	require.Equal(t, 12, data.Total)
	require.Equal(t, "SZ300000", data.Results[0].ExCode, "保持新浪的相关度顺序")
	require.Equal(t, []string{"新浪返回 12 条结果（接口最多约 50 条，不是匹配总数），只返回最相关的前 10 条，可以换更具体的关键字"}, result.Notes)
}

func TestSearchNotFound(t *testing.T) {
	defer gock.Off()
	suggestKey("nosuch").Reply(200).BodyString(`var suggestvalue="";`)

	result, data, err := callSearchJSON(t, `{"query":"nosuch"}`)
	require.NoError(t, err, "没搜到不算出错")
	require.Empty(t, data.Results)
	require.NotNil(t, data.Results, "是空数组，不是 null")
	require.Len(t, result.Notes, 1)
	require.Contains(t, result.Notes[0], "没有找到匹配 nosuch")
}

func TestSearchErrors(t *testing.T) {
	for _, input := range []string{`{}`, `{"query":"  "}`, `{"query":"x","limit":3}`} {
		_, _, err := callSearchJSON(t, input)
		require.True(t, IsInputError(err), input)
	}

	defer gock.Off()
	// 搜索失败是上游问题，不按代码猜结果。
	suggestKey("SH600519").Persist().Reply(500)
	_, _, err := callSearchJSON(t, `{"query":"SH600519"}`)
	require.False(t, IsInputError(err))
	require.ErrorContains(t, err, "搜索证券失败")
}
