package aitool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/alwqx/sec/provider/sina"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func suggestKey(key string) *gock.Request {
	return gock.New("https://suggest3.sinajs.cn").Get("/suggest/.*key=" + key + "$")
}

const cmbSuggest = `var suggestvalue="招商银行,11,600036,sh600036,招商银行,,招商银行,99,1,ESG,,";`
const cmbLine = `var hq_str_sh600036="招商银行,40.000,39.000,41.000,42.000,39.500,41.000,41.010,100,4100.000,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,2026-10-30,15:00:00,00,";`
const moutaiSuggest = `var suggestvalue="贵州茅台,11,600519,sh600519,贵州茅台,,贵州茅台,99,1,ESG,,";`

func callQuoteJSON(t *testing.T, input string) (*Result, *quoteData, error) {
	t.Helper()
	result, err := QuoteTool().Call(context.Background(), json.RawMessage(input))
	if err != nil {
		return nil, nil, err
	}
	data, ok := result.Data.(*quoteData)
	require.True(t, ok)
	return result, data, nil
}

func TestQuote(t *testing.T) {
	defer gock.Off()
	suggestKey("600036").Reply(200).BodyString(cmbSuggest)
	gock.New("https://hq.sinajs.cn").Get("/list=sh600036").Reply(200).BodyString(cmbLine)

	result, data, err := callQuoteJSON(t, `{"codes":["600036"]}`)
	require.NoError(t, err)
	require.True(t, gock.IsDone())
	require.Equal(t, "get_quote", result.Tool)
	require.Equal(t, SchemaVersion, result.SchemaVersion)
	require.Equal(t, "2026-10-30", result.AsOf)
	require.Empty(t, result.Notes)
	require.Empty(t, data.Missing)
	require.Len(t, data.Quotes, 1)

	quote := data.Quotes[0]
	require.Equal(t, "SH600036", quote.ExCode)
	require.Equal(t, "招商银行", quote.Name)
	require.Equal(t, "15:00:00", quote.Time)
	require.Equal(t, "Asia/Shanghai", quote.Timezone)
	require.Equal(t, 41.0, *quote.Price)
	require.Equal(t, 39.0, *quote.YClose)
	require.Equal(t, 2.0, *quote.Change)
	require.Equal(t, 5.13, *quote.ChangePct, "2/39 = 5.128%，保留 2 位")
	require.Equal(t, int64(100), *quote.Volume)
	require.Equal(t, 4100.0, *quote.Amount)
	require.False(t, quote.PriceIsYClose)
}

func TestQuoteBeforeOpen(t *testing.T) {
	defer gock.Off()
	suggestKey("600036").Reply(200).BodyString(cmbSuggest)
	// 现价为 0：开盘前没有成交价。
	line := strings.Replace(cmbLine, "39.000,41.000,", "39.000,0.000,", 1)
	gock.New("https://hq.sinajs.cn").Get("/list=sh600036").Reply(200).BodyString(line)

	result, data, err := callQuoteJSON(t, `{"codes":["600036"]}`)
	require.NoError(t, err)
	quote := data.Quotes[0]
	require.True(t, quote.PriceIsYClose)
	require.Equal(t, 39.0, *quote.Price)
	require.Nil(t, quote.Change)
	require.Nil(t, quote.ChangePct)
	require.Len(t, result.Notes, 1)
	require.Contains(t, result.Notes[0], "昨收")
}

func TestQuotePartial(t *testing.T) {
	defer gock.Off()
	suggestKey("600036").Reply(200).BodyString(cmbSuggest)
	suggestKey("cmb").Reply(200).BodyString(cmbSuggest)
	suggestKey("nosuch").Reply(200).BodyString(`var suggestvalue="";`)
	suggestKey("broken").Reply(500)
	suggestKey("600519").Reply(200).BodyString(moutaiSuggest)
	// 茅台搜到了但行情里没有，比如停牌。
	gock.New("https://hq.sinajs.cn").Get("/list=sh600036,sh600519").Reply(200).BodyString(cmbLine)

	result, data, err := callQuoteJSON(t, `{"codes":["600036","cmb","nosuch","broken","600519"]}`)
	require.NoError(t, err)
	require.Len(t, data.Quotes, 1, "cmb 和 600036 是同一只，只保留一个")
	require.Equal(t, "SH600036", data.Quotes[0].ExCode)

	reasons := map[string]string{}
	for _, item := range data.Missing {
		reasons[item.Code] = item.Reason
	}
	require.Len(t, reasons, 3, "合并的 cmb 不算缺失")
	require.Equal(t, "未找到证券", reasons["nosuch"])
	require.Contains(t, reasons["broken"], "搜索失败")
	require.Contains(t, reasons["SH600519"], "没有行情数据")
	notes := strings.Join(result.Notes, "\n")
	require.Contains(t, notes, "cmb 和 600036 是同一只证券（SH600036）")
	require.Contains(t, notes, "data.missing")
}

func TestQuoteMixedDates(t *testing.T) {
	defer gock.Off()
	suggestKey("600036").Reply(200).BodyString(cmbSuggest)
	suggestKey("600519").Reply(200).BodyString(moutaiSuggest)
	moutaiLine := strings.NewReplacer("招商银行", "贵州茅台", "sh600036", "sh600519", "2026-10-30", "2026-10-29").Replace(cmbLine)
	gock.New("https://hq.sinajs.cn").Get("/list=sh600036,sh600519").Reply(200).BodyString(cmbLine + "\n" + moutaiLine)

	result, data, err := callQuoteJSON(t, `{"codes":["600036","600519"]}`)
	require.NoError(t, err)
	require.Len(t, data.Quotes, 2)
	require.Empty(t, result.AsOf, "日期不同时不给统一的 as_of")
	require.Equal(t, "2026-10-29", data.Quotes[1].TradeDate)
}

func TestQuoteErrors(t *testing.T) {
	_, _, err := callQuoteJSON(t, `{"codes":[" ",""]}`)
	require.True(t, IsInputError(err))
	require.ErrorContains(t, err, "不能为空")

	_, _, err = callQuoteJSON(t, `{"codes":["1","2","3","4","5","6"]}`)
	require.True(t, IsInputError(err))
	require.ErrorContains(t, err, "最多")

	defer gock.Off()
	// 都找不到是参数问题，模型应改代码重试。
	suggestKey("nosuch").Reply(200).BodyString(`var suggestvalue="";`)
	_, _, err = callQuoteJSON(t, `{"codes":["nosuch"]}`)
	require.True(t, IsInputError(err))
	require.ErrorContains(t, err, "nosuch 未找到证券")

	// 有搜索失败时是上游问题。
	suggestKey("nosuch").Reply(200).BodyString(`var suggestvalue="";`)
	suggestKey("broken").Persist().Reply(500)
	_, _, err = callQuoteJSON(t, `{"codes":["nosuch","broken"]}`)
	require.False(t, IsInputError(err))
	require.ErrorContains(t, err, "搜索失败")

	suggestKey("600036").Reply(200).BodyString(cmbSuggest)
	gock.New("https://hq.sinajs.cn").Get("/list=sh600036").Persist().Reply(502)
	_, _, err = callQuoteJSON(t, `{"codes":["600036"]}`)
	require.ErrorContains(t, err, "行情获取失败")
}

func TestQuoteCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := QuoteTool().Call(ctx, json.RawMessage(`{"codes":["600036"]}`))
	require.ErrorContains(t, err, "搜索证券超时或被取消")
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, IsInputError(err))
}

func TestQuoteItemNAV(t *testing.T) {
	// 基金净值没有成交，成交量和成交额是 null 而不是 0。
	item := newQuoteItem(&sina.BasicSecurity{ExCode: "OF005827", Name: "易方达蓝筹精选混合"},
		&sina.SecurityQuote{ExCode: "OF005827", TradeDate: "2026-10-30", Current: 1.5, YClose: 1.4, NAV: true})
	require.True(t, item.NAV)
	require.Nil(t, item.Volume)
	require.Nil(t, item.Amount)
	require.Equal(t, "易方达蓝筹精选混合", item.Name, "净值行没有名称时用搜索结果的名称")
	data, err := json.Marshal(item)
	require.NoError(t, err)
	require.Contains(t, string(data), `"volume":null,"amount":null`)
}
