package quote

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/sina"
	"github.com/alwqx/sec/types"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestPrintQuote(t *testing.T) {
	// 涨
	quote1 := &sina.SecurityQuote{
		Name:      "龙芯中科",
		Code:      "688047",
		ExCode:    "SH688047",
		TradeDate: "2024-09-30",
		Time:      "15:00:01",
		Current:   119.62,
		YClose:    99.68,
		Open:      106,
		High:      119.62,
		Low:       104.5,
		Volume:    938310086.000,
		TurnOver:  8256723,
	}
	fmt.Println("上涨")
	printQuote(io.Discard, []*sina.SecurityQuote{quote1})

	// 跌
	quote2 := &sina.SecurityQuote{
		Name:      "龙芯中科",
		Code:      "688047",
		ExCode:    "SH688047",
		TradeDate: "2024-09-30",
		Time:      "15:00:01",
		Current:   119.62,
		YClose:    199.68,
		Open:      106,
		High:      119.62,
		Low:       104.5,
		Volume:    938310086.000,
		TurnOver:  8256723,
	}
	fmt.Println("下跌")
	printQuote(io.Discard, []*sina.SecurityQuote{quote2})

	// 平
	quote3 := &sina.SecurityQuote{
		Name:      "龙芯中科",
		Code:      "688047",
		ExCode:    "SH688047",
		TradeDate: "2024-09-30",
		Time:      "15:00:01",
		Current:   119.62,
		YClose:    119.62,
		Open:      106,
		High:      119.62,
		Low:       104.5,
		Volume:    938310086.000,
		TurnOver:  8256723,
	}
	fmt.Println("不变")
	printQuote(io.Discard, []*sina.SecurityQuote{quote1, quote2, quote3})

	// 3. 从 string 解析
	str := `[
	{
		"TradeDate": "2024-09-30",
		"Code": "688047",
		"ExCode": "SH688047",
		"Name": "龙芯中科",
		"Current": 119.62,
		"Open": 106,
		"YClose": 99.68,
		"High": 119.62,
		"Low": 104.5,
		"Volume": 938310086,
		"TurnOver": 8256723,
		"Time": "15:00:01"
	},
	{
		"TradeDate": "2024-09-30",
		"Code": "002475",
		"ExCode": "SZ002475",
		"Name": "立讯精密",
		"Current": 43.46,
		"Open": 42,
		"YClose": 40.48,
		"High": 43.95,
		"Low": 41.01,
		"Volume": 6841666995.79,
		"TurnOver": 160489237,
		"Time": "15:00:00"
	}
]`
	var quotes []*sina.SecurityQuote
	err := json.Unmarshal([]byte(str), &quotes)
	require.Nil(t, err)
	printQuote(io.Discard, quotes)
}

func TestCurrentText(t *testing.T) {
	text, rate := currentText(&sina.SecurityQuote{Current: 11, YClose: 10})
	require.Equal(t, "11 1 10%", text)
	require.Equal(t, 10.0, rate)

	// 开盘前当前价是 0，不能算出 -100%。
	text, rate = currentText(&sina.SecurityQuote{YClose: 10})
	require.Contains(t, text, "昨收")
	require.NotContains(t, text, "-100")
	require.Zero(t, rate)

	text, rate = currentText(&sina.SecurityQuote{Current: 11})
	require.Equal(t, "11", text, "昨收缺失时不算涨跌")
	require.Zero(t, rate)

	text, _ = currentText(&sina.SecurityQuote{})
	require.Equal(t, "—", text)
}

// 按 Ctrl-C 取消初始搜索时正常退出，不报错。
func TestCanceledSearchIsNotAnError(t *testing.T) {
	defer gock.Off()
	gock.New("https://suggest3.sinajs.cn").Get("/suggest/").Persist().Reply(200).BodyString(`var suggestvalue="";`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	require.NoError(t, quoteMultiSec(ctx, io.Discard, []string{"a"}))
	require.NoError(t, quoteMultiSecRealtime(ctx, io.Discard, []string{"a"}))
}

func TestSearchFailureIsAnError(t *testing.T) {
	defer gock.Off()
	gock.New("https://suggest3.sinajs.cn").Get("/suggest/").Persist().Reply(500)
	require.ErrorContains(t, quoteMultiSec(context.Background(), io.Discard, []string{"a"}), "a 搜索失败")
}

func TestRetryProblems(t *testing.T) {
	defer gock.Off()
	gock.New("https://suggest3.sinajs.cn").Get("/suggest/").Reply(200).BodyString(`var suggestvalue="招商银行,11,600036,sh600036,招商银行,,招商银行,99,1,ESG,,";`)
	res := retryProblems(context.Background(), resolution{problems: []keyProblem{{"600036", "搜索失败", true}, {"nosuch", "未找到证券", false}}})
	require.Len(t, res.secs, 1)
	require.Equal(t, "SH600036", res.secs[0].ExCode)
	require.Equal(t, []keyProblem{{"nosuch", "未找到证券", false}}, res.problems, "找不到的不再重试")
	require.Empty(t, res.notes)

	same := retryProblems(context.Background(), resolution{secs: res.secs, problems: []keyProblem{{"nosuch", "未找到证券", false}}})
	require.Equal(t, res.secs, same.secs)
	require.Len(t, same.problems, 1)
}

func TestProblemText(t *testing.T) {
	require.Equal(t, "部分证券没有行情：a 未找到证券；SH600036 没有行情数据", problemText([]keyProblem{{"a", "未找到证券", false}, {"SH600036", "没有行情数据", false}}))
}

func suggestKey(key string) *gock.Request {
	return gock.New("https://suggest3.sinajs.cn").Get("/suggest/.*key=" + key + "$")
}

const cmbSuggest = `var suggestvalue="招商银行,11,600036,sh600036,招商银行,,招商银行,99,1,ESG,,";`
const cmbLine = `var hq_str_sh600036="招商银行,40.000,39.000,41.000,42.000,39.500,41.000,41.010,100,4100.000,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,2026-10-30,15:00:00,00,";`

func TestQuoteSearchWithExpandedFields(t *testing.T) {
	defer gock.Off()
	suggestKey("lxzk").Reply(200).BodyString(`var suggestvalue="龙芯中科,11,688047,sh688047,龙芯中科,,龙芯中科,99,1,ESG,,,";`)
	gock.New("https://hq.sinajs.cn").Get("/list=sh688047").Reply(200).
		BodyString(strings.NewReplacer("招商银行", "龙芯中科", "sh600036", "sh688047").Replace(cmbLine))
	var out bytes.Buffer
	err := quoteMultiSec(context.Background(), &out, []string{"lxzk"})
	require.NoError(t, err)
	require.Contains(t, out.String(), "龙芯中科")
	require.Contains(t, out.String(), "SH688047")
	require.True(t, gock.IsDone())
}

func TestResolveKeysKeepsOrderAndDedupes(t *testing.T) {
	defer gock.Off()
	suggestKey("600036").Reply(200).BodyString(cmbSuggest)
	suggestKey("cmb").Reply(200).BodyString(cmbSuggest)
	suggestKey("nosuch").Reply(200).BodyString(`var suggestvalue="";`)
	suggestKey("broken").Reply(500)
	res := resolveKeys(context.Background(), []string{"nosuch", "600036", "broken", "cmb"})
	require.Len(t, res.secs, 1, "搜到同一只证券的关键字只保留一个")
	require.Equal(t, "SH600036", res.secs[0].ExCode)
	require.Equal(t, []string{"cmb 和 600036 是同一只证券（SH600036），已合并为一条"}, res.notes)
	require.Equal(t, "600036", res.firstKey["SH600036"])
	require.Len(t, res.problems, 2)
	require.Equal(t, "nosuch", res.problems[0].key, "保持输入顺序")
	require.False(t, res.problems[0].retry)
	require.Equal(t, "broken", res.problems[1].key)
	require.True(t, res.problems[1].retry)
}

func TestQuoteFailureKeepsSearchProblems(t *testing.T) {
	defer gock.Off()
	suggestKey("600036").Reply(200).BodyString(cmbSuggest)
	suggestKey("nosuch").Reply(200).BodyString(`var suggestvalue="";`)
	gock.New("https://hq.sinajs.cn").Get("/list=sh600036").Reply(502)
	err := quoteMultiSec(context.Background(), io.Discard, []string{"600036", "nosuch"})
	require.ErrorContains(t, err, "nosuch 未找到证券")
	require.ErrorContains(t, err, "行情获取失败")
}

// fastRefresh 把实时模式的刷新改成毫秒级，并且不执行清屏命令。
func fastRefresh(t *testing.T) {
	interval, clear := refreshInterval, clearScreen
	refreshInterval, clearScreen = 20*time.Millisecond, func() {}
	t.Cleanup(func() { refreshInterval, clearScreen = interval, clear })
}

// 实时模式里行情接口偶尔失败，不能结束整个会话。
func TestRealtimeSurvivesQuoteFailure(t *testing.T) {
	defer gock.Off()
	fastRefresh(t)
	suggestKey("600036").Reply(200).BodyString(cmbSuggest)
	gock.New("https://hq.sinajs.cn").Get("/list=sh600036").Reply(200).BodyString(cmbLine)
	gock.New("https://hq.sinajs.cn").Get("/list=sh600036").Reply(502)
	gock.New("https://hq.sinajs.cn").Get("/list=sh600036").Persist().Reply(200).BodyString(cmbLine)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	var out bytes.Buffer
	require.NoError(t, quoteMultiSecRealtime(ctx, &out, []string{"600036"}))
	require.Contains(t, out.String(), "行情获取失败，显示的是上一次的行情")
	require.GreaterOrEqual(t, strings.Count(out.String(), "SH600036"), 2, "失败时仍显示上一次的行情")
}

func TestRealtimeFirstQuoteFailure(t *testing.T) {
	defer gock.Off()
	fastRefresh(t)
	suggestKey("600036").Reply(200).BodyString(cmbSuggest)
	gock.New("https://hq.sinajs.cn").Get("/list=sh600036").Persist().Reply(502)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	var out bytes.Buffer
	require.NoError(t, quoteMultiSecRealtime(ctx, &out, []string{"600036"}))
	require.Contains(t, out.String(), "行情获取失败，3 秒后重试")
	require.NotContains(t, out.String(), "上一次的行情", "还没有拿到过行情")
}

// 重试时按 Ctrl-C 是正常退出，不能报“没有可显示的证券”。
func TestRealtimeCancelDuringRetry(t *testing.T) {
	defer gock.Off()
	fastRefresh(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	gock.New("https://suggest3.sinajs.cn").Get("/suggest/").Persist().AddMatcher(func(*http.Request, *gock.Request) (bool, error) {
		calls++
		// 启动时的两次搜索失败；到了重试就取消。
		if calls > 2 {
			cancel()
		}
		return true, nil
	}).Reply(500)
	require.NoError(t, quoteMultiSecRealtime(ctx, io.Discard, []string{"a", "b"}))
}

func TestLimitKeys(t *testing.T) {
	keys, problems := limitKeys([]string{"a", "b", "c", "d", "e", "f", "g"})
	require.Equal(t, []string{"a", "b", "c", "d", "e"}, keys)
	require.Equal(t, []keyProblem{{"f", "超过 5 只，未查询", false}, {"g", "超过 5 只，未查询", false}}, problems)
	keys, problems = limitKeys([]string{"a"})
	require.Equal(t, []string{"a"}, keys)
	require.Empty(t, problems)
}

func TestTooManyKeysIsAnError(t *testing.T) {
	defer gock.Off()
	gock.New("https://suggest3.sinajs.cn").Get("/suggest/").Times(5).Reply(200).BodyString(cmbSuggest)
	gock.New("https://hq.sinajs.cn").Get("/list=sh600036").Reply(200).BodyString(cmbLine)
	err := quoteMultiSec(context.Background(), io.Discard, []string{"a", "b", "c", "d", "e", "f"})
	require.ErrorContains(t, err, "f 超过 5 只，未查询")
}

func TestRetryKeepsOrderAndDedupesCase(t *testing.T) {
	defer gock.Off()
	suggestKey("broken").Reply(200).BodyString(`var suggestvalue="招商银行,11,600036,sh600036,招商银行,,招商银行,99,1,ESG,,";`)
	existing := resolution{secs: []*sina.BasicSecurity{{ExCode: "sh600036"}}, firstKey: map[string]string{"SH600036": "600036"}, problems: []keyProblem{{"broken", "搜索失败", true}}}
	res := retryProblems(context.Background(), existing)
	require.Len(t, res.secs, 1, "大小写不同也是同一只证券")
	require.Equal(t, []string{"broken 和 600036 是同一只证券（SH600036），已合并为一条"}, res.notes, "重试搜到已有证券时也说明合并")

	ordered := inKeyOrder([]keyProblem{{"nosuch", "", false}, {"broken", "", true}}, []string{"broken", "nosuch"})
	require.Equal(t, "broken", ordered[0].key)
}

func TestQuoteTime(t *testing.T) {
	require.Equal(t, "2024-09-30 15:00:01", quoteTime(&sina.SecurityQuote{TradeDate: "2024-09-30", Time: "15:00:01", Timezone: types.TimezoneShanghai}))
	require.Equal(t, "2026-11-05 20:45:05 美东", quoteTime(&sina.SecurityQuote{TradeDate: "2026-11-05", Time: "20:45:05", Timezone: types.TimezoneNewYork}))
	require.Equal(t, "2026-11-05 净值", quoteTime(&sina.SecurityQuote{TradeDate: "2026-11-05", Timezone: types.TimezoneShanghai, NAV: true}))
}

// 基金净值没有开高低和成交，JSON 是 null、表格显示 -，并标出 nav。
func TestQuoteRowNAV(t *testing.T) {
	quote := &sina.SecurityQuote{ExCode: "OF005827", Name: "易方达蓝筹精选混合", Current: 1.4164, YClose: 1.4009, TradeDate: "2026-11-05", NAV: true}
	row := newQuoteRow(quote)
	require.True(t, row.NAV)
	require.Nil(t, row.Open)
	require.Nil(t, row.Volume, "净值没有成交，不是成交 0")
	require.Nil(t, row.Amount)
	require.InDelta(t, 1.1064, *row.ChangePct, 1e-4)

	// 交易所行情照常输出成交量和成交额，0 也是真实的 0（比如开盘前）。
	traded := newQuoteRow(&sina.SecurityQuote{ExCode: "SH600036", Current: 41, YClose: 40, TurnOver: 100, Volume: 4100})
	require.Equal(t, int64(100), *traded.Volume)
	require.Equal(t, 4100.0, *traded.Amount)
	beforeOpen := newQuoteRow(&sina.SecurityQuote{ExCode: "SH600036", YClose: 40})
	require.Equal(t, int64(0), *beforeOpen.Volume)

	var out bytes.Buffer
	printQuote(&out, []*sina.SecurityQuote{quote})
	require.Contains(t, out.String(), "2026-11-05 净值")
	require.NotContains(t, out.String(), "| 0 ", "没有的开高低不显示成 0")
}

// 表格模式在表格下方说明合并了哪些关键字，合并不算失败。
func TestQuoteMergeNote(t *testing.T) {
	defer gock.Off()
	suggestKey("600036").Reply(200).BodyString(cmbSuggest)
	suggestKey("cmb").Reply(200).BodyString(cmbSuggest)
	gock.New("https://hq.sinajs.cn").Get("/list=sh600036").Reply(200).BodyString(cmbLine)
	var out bytes.Buffer
	require.NoError(t, quoteMultiSec(context.Background(), &out, []string{"600036", "cmb"}))
	require.Equal(t, 1, strings.Count(out.String(), "SH600036  "), "只有一行")
	require.Contains(t, out.String(), "cmb 和 600036 是同一只证券（SH600036），已合并为一条")
}

// 实时模式每次刷新都在表格下方保留合并说明。
func TestRealtimeMergeNote(t *testing.T) {
	defer gock.Off()
	fastRefresh(t)
	suggestKey("600036").Reply(200).BodyString(cmbSuggest)
	suggestKey("cmb").Reply(200).BodyString(cmbSuggest)
	gock.New("https://hq.sinajs.cn").Get("/list=sh600036").Persist().Reply(200).BodyString(cmbLine)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	var out bytes.Buffer
	require.NoError(t, quoteMultiSecRealtime(ctx, &out, []string{"600036", "cmb"}))
	refreshes := strings.Count(out.String(), "招商银行")
	require.GreaterOrEqual(t, refreshes, 2)
	require.Equal(t, refreshes, strings.Count(out.String(), "cmb 和 600036 是同一只证券"), "每次刷新都显示")
}
