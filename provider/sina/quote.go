package sina

import (
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/gorilla/websocket"
)

// QuoteWs 通过 websocket 请求多个证券行情信息
func QuoteWs(keys []string) ([]*SecurityQuote, error) {
	formatKeys := formatQuoteKeys(keys)
	url := fmt.Sprintf("wss://hq.sinajs.cn/wskt?list=%s", strings.Join(formatKeys, ","))
	headers := make(http.Header)
	headers.Add("Origin", SinaReferer)
	conn, _, err := websocket.DefaultDialer.Dial(url, headers)
	if err != nil {
		slog.Error(err.Error())
		return nil, err
	}

	defer conn.Close()

	_, msg, err := conn.ReadMessage()
	if err != nil {
		slog.Error(err.Error())
		return nil, err
	}

	return parseQuoteWsBody(string(msg)), nil
}

// parseQuoteWsBody 解析 QuoteWs 请求的多个 证券信息
// sh600036=招商银行,36.350,35.630,37.610,38.000,35.920,37.610,37.620,256101260,9443438268.000,690801,37.610,286600,37.600,17000,37.590,55400,37.580,12200,37.570,161925,37.620,90600,37.630,50400,37.640,104100,37.650,126000,37.660,2024-09-30,15:00:00,00,
// sh688047=龙芯中科,106.000,99.680,119.620,119.620,104.500,119.620,0.000,8256723,938310086.000,25600,119.620,7255,119.610,3033,119.600,1767,119.570,6300,119.550,0,0.000,0,0.000,0,0.000,0,0.000,0,0.000,2024-09-30,15:00:01,00,
func parseQuoteWsBody(msg string) []*SecurityQuote {
	slog.Debug("parseQuoteWsBody", "msg", parseQuoteWsBody)
	if msg == "" {
		return nil
	}

	lines := strings.Split(msg, "\n")
	if len(lines) == 0 {
		return nil
	}

	res := make([]*SecurityQuote, 0, len(lines))
	for _, line := range lines {
		if line == "" || strings.Contains(line, "sys_nxkey") {
			continue
		}
		items := strings.Split(line, "=")
		if len(items) != 2 {
			slog.Error("parseQuoteWsBody", "invalid ws quote", line)
		}
		slog.Debug("parseQuoteWsBody", "items", items)
		quote := parseSecQuote(items[1])
		res = append(res, quote)
	}

	sort.Slice(res, func(i, j int) bool {
		return res[i].Name < res[j].Name
	})

	return res
}

// formatQuoteKeys
// A股格式
// 港股格式 rt_hk00700
// 港股指数 rt_hkHSI
// 美股格式 gb_baba
func formatQuoteKeys(keys []string) []string {
	res := make([]string, 0, len(keys))
	for _, key := range keys {
		var newKey string
		if strings.HasPrefix(key, "hk") {
			reg := regexp.MustCompile("^hk")
			tmp := reg.ReplaceAllString(key, "")
			newKey = "rt_hk" + strings.ToUpper(tmp)
		} else if strings.HasPrefix(key, "$") {
			newKey = strings.ReplaceAll(key, "$", "gb_")
		} else {
			newKey = strings.ToLower(key)
		}
		res = append(res, newKey)
	}

	return res
}
