package sina

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alwqx/sec/types"
	"github.com/alwqx/sec/utils"
)

// AdjustSegment 是新浪前复权的一段：从 Date（含）到下一段的 Date 之前，前复权价 = 不复权价 × Scale + Offset。
// 沪深股票和港股只按比例调整，Offset 为 0；美股和沪深基金的现金分红按金额从价格里减去，和东方财富的前复权相同。
// 口径和核对记录见 docs/adjust.md。
type AdjustSegment struct {
	Date   string // YYYY-MM-DD，除权除息日；最早一段从 1900-01-01 或 1970-01-01 开始
	Scale  float64
	Offset float64
}

// ErrNoAdjustFactors 表示新浪没有这只证券的复权因子文件：代码不存在，或者是港股 ETF、港股指数等新浪不提供因子的证券，两者分不开。
var ErrNoAdjustFactors = errors.New("新浪没有这只证券的复权因子")

// QueryAdjustFactors 查询前复权因子，按日期升序。exCode 形如 SH600036、HK00700、$AAPL。
// 没有除权记录时一般只有一段 Scale 为 1 的数据（美股指数如 .DJI 也是这样），沪深指数返回空列表，
// 新浪没有因子文件时返回 ErrNoAdjustFactors。
// 各段按新浪发布的原样返回，不一定和价格吻合（见 docs/adjust.md 的已知差异），使用前要和东方财富前复权核对。
func QueryAdjustFactors(ctx context.Context, exCode string) ([]AdjustSegment, error) {
	reqURL, market, err := adjustURL(exCode)
	if err != nil {
		return nil, err
	}
	resp, err := utils.MakeRequest(ctx, http.MethodGet, reqURL, defaultHTTPHeaders(), nil, 0)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%w: %s", ErrNoAdjustFactors, exCode)
	}
	if err := utils.CheckStatus(resp); err != nil {
		return nil, err
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return parseAdjustFactors(body, market)
}

// adjustMarket 区分复权因子文件的口径：同样的 f 字段，沪深股票是除数，港股是乘数。
type adjustMarket int

const (
	adjustA adjustMarket = iota
	adjustHK
	adjustUS
)

func adjustURL(exCode string) (string, adjustMarket, error) {
	code := strings.TrimSpace(exCode)
	switch {
	case types.IsMCode(code):
		// 美股代码要大写，小写返回 404；类别股用点号，如 BRK.B。
		symbol := strings.ToUpper(strings.TrimPrefix(code, "$"))
		if symbol != "" {
			return "https://finance.sina.com.cn/us_stock/company/reinstatement/" + url.PathEscape(symbol) + "_qfq.js", adjustUS, nil
		}
	case types.IsHCode(code):
		symbol := code[2:]
		// 地址里的数字代码要补足 5 位，HK700 要写成 00700。
		if m := hkCode.FindStringSubmatch(code); m != nil {
			symbol = strings.Repeat("0", 5-len(m[1])) + m[1]
		}
		if symbol != "" {
			return "https://finance.sina.com.cn/stock/hkstock/" + url.PathEscape(strings.ToUpper(symbol)) + "/qfq.js", adjustHK, nil
		}
	case types.IsACode(code):
		if len(code) > 2 {
			return "https://finance.sina.com.cn/realstock/company/" + url.PathEscape(strings.ToLower(code)) + "/qfq.js", adjustA, nil
		}
	}
	return "", 0, fmt.Errorf("不支持查询复权因子的代码: %s", exCode)
}

// parseAdjustFactors 解析 var xxx={...} 形式的复权因子，后面跟着一段 /* */ 注释。
func parseAdjustFactors(body []byte, market adjustMarket) ([]AdjustSegment, error) {
	text, _, _ := strings.Cut(string(body), "/*")
	_, text, ok := strings.Cut(text, "=")
	if !ok {
		return nil, fmt.Errorf("新浪复权因子格式无效")
	}
	var payload struct {
		Data []struct {
			D, F, C, S, U string
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSuffix(strings.TrimSpace(text), ";")), &payload); err != nil {
		return nil, fmt.Errorf("新浪复权因子格式无效: %w", err)
	}
	// 基金和股票的口径按整个文件判断，混着两种字段的文件报错。
	fund := market == adjustA && len(payload.Data) > 0 && payload.Data[0].S != ""
	segments := make([]AdjustSegment, 0, len(payload.Data))
	for _, row := range payload.Data {
		if _, err := time.Parse(time.DateOnly, row.D); err != nil {
			return nil, fmt.Errorf("新浪复权因子日期无效: %q", row.D)
		}
		segment, err := adjustSegment(market, fund, row.D, row.F, row.C, row.S, row.U)
		if err != nil {
			return nil, fmt.Errorf("新浪复权因子 %s: %w", row.D, err)
		}
		segments = append(segments, segment)
	}
	sort.Slice(segments, func(i, j int) bool { return segments[i].Date < segments[j].Date })
	// 同一天两段时不知道用哪一段。
	for i := 1; i < len(segments); i++ {
		if segments[i].Date == segments[i-1].Date {
			return nil, fmt.Errorf("新浪复权因子 %s 重复", segments[i].Date)
		}
	}
	return segments, nil
}

// adjustSegment 按市场和字段换算成统一的 Scale、Offset：
//   - 沪深股票：前复权价 = 不复权价 ÷ f
//   - 沪深基金：前复权价 = 不复权价 ÷ s − u，f 都是 1
//   - 港股：前复权价 = 不复权价 × f
//   - 美股：前复权价 = 不复权价 × f + c
func adjustSegment(market adjustMarket, fund bool, date, f, c, s, u string) (AdjustSegment, error) {
	segment := AdjustSegment{Date: date}
	factor, err := adjustNumber("f", f)
	if err != nil {
		return segment, err
	}
	// 只认上面四种字段组合，其他组合的口径不清楚，不猜。
	if fund && (s == "" || u == "" || c != "") || !fund && (s != "" || u != "" || (c != "" && market != adjustUS)) {
		return segment, fmt.Errorf("未知的字段组合")
	}
	switch {
	case fund:
		if factor != 1 {
			return segment, fmt.Errorf("基金复权因子 f=%s 不是 1", f)
		}
		scale, err := adjustNumber("s", s)
		if err != nil {
			return segment, err
		}
		cash, err := adjustCash("u", u)
		if err != nil {
			return segment, err
		}
		segment.Scale, segment.Offset = 1/scale, -cash
	case market == adjustA:
		segment.Scale = 1 / factor
	case market == adjustHK:
		segment.Scale = factor
	default:
		segment.Scale = factor
		if c != "" {
			if segment.Offset, err = adjustCash("c", c); err != nil {
				return segment, err
			}
		}
	}
	return segment, nil
}

// adjustNumber 解析比例因子，必须是正数。
func adjustNumber(name, text string) (float64, error) {
	value, err := strconv.ParseFloat(text, 64)
	if err != nil || !(value > 0) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("%s=%q 无效", name, text)
	}
	return value, nil
}

// adjustCash 解析累计分红金额，可以是 0 或负数。
func adjustCash(name, text string) (float64, error) {
	value, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("%s=%q 无效", name, text)
	}
	return value, nil
}
