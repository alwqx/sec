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
	secs, problems := retryProblems(context.Background(), nil, []keyProblem{{"600036", "搜索失败", true}, {"nosuch", "未找到证券", false}})
	require.Len(t, secs, 1)
	require.Equal(t, "SH600036", secs[0].ExCode)
	require.Equal(t, []keyProblem{{"nosuch", "未找到证券", false}}, problems, "找不到的不再重试")

	same, kept := retryProblems(context.Background(), secs, []keyProblem{{"nosuch", "未找到证券", false}})
	require.Equal(t, secs, same)
	require.Len(t, kept, 1)
}

func TestProblemText(t *testing.T) {
	require.Equal(t, "部分证券没有行情：a 未找到证券；SH600036 没有行情数据", problemText([]keyProblem{{"a", "未找到证券", false}, {"SH600036", "没有行情数据", false}}))
}

func suggestKey(key string) *gock.Request {
	return gock.New("https://suggest3.sinajs.cn").Get("/suggest/.*key=" + key + "$")
}

const cmbSuggest = `var suggestvalue="招商银行,11,600036,sh600036,招商银行,,招商银行,99,1,ESG,,";`
const cmbLine = `var hq_str_sh600036="招商银行,40.000,39.000,41.000,42.000,39.500,41.000,41.010,100,4100.000,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,2026-10-30,15:00:00,00,";`

func TestResolveKeysKeepsOrderAndDedupes(t *testing.T) {
	defer gock.Off()
	suggestKey("600036").Reply(200).BodyString(cmbSuggest)
	suggestKey("cmb").Reply(200).BodyString(cmbSuggest)
	suggestKey("nosuch").Reply(200).BodyString(`var suggestvalue="";`)
	suggestKey("broken").Reply(500)
	secs, problems := resolveKeys(context.Background(), []string{"nosuch", "600036", "broken", "cmb"})
	require.Len(t, secs, 1, "搜到同一只证券的关键字只保留一个")
	require.Equal(t, "SH600036", secs[0].ExCode)
	require.Len(t, problems, 2)
	require.Equal(t, "nosuch", problems[0].key, "保持输入顺序")
	require.False(t, problems[0].retry)
	require.Equal(t, "broken", problems[1].key)
	require.True(t, problems[1].retry)
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
	existing := []*sina.BasicSecurity{{ExCode: "sh600036"}}
	secs, _ := retryProblems(context.Background(), existing, []keyProblem{{"broken", "搜索失败", true}})
	require.Len(t, secs, 1, "大小写不同也是同一只证券")

	ordered := inKeyOrder([]keyProblem{{"nosuch", "", false}, {"broken", "", true}}, []string{"broken", "nosuch"})
	require.Equal(t, "broken", ordered[0].key)
}
