package sina

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/alwqx/sec/utils"
)

// usDailyBody 取出 JSONP 回调里的数组：var t=([...]);
var usDailyBody = regexp.MustCompile(`(?s)\((\[.*\])\)`)

// QueryUSDailyBars 查询美股全部日线，按日期升序，从上市（最早 1984 年）到最近一个交易日。
// 价格不复权：拆股当天价格会跳变，比如苹果 2020-08-31 从 499 变成 129。symbol 是代码，比如 aapl。
func QueryUSDailyBars(ctx context.Context, symbol string) ([]*Bar, error) {
	symbol = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(symbol), "$"))
	reqURL := "https://stock.finance.sina.com.cn/usstock/api/jsonp.php/var%20t=/US_MinKService.getDailyK?symbol=" + url.QueryEscape(symbol)
	resp, err := utils.MakeRequest(ctx, http.MethodGet, reqURL, defaultHTTPHeaders(), nil, 0)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := utils.CheckStatus(resp); err != nil {
		return nil, err
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return parseUSDailyBars(body)
}

func parseUSDailyBars(body []byte) ([]*Bar, error) {
	match := usDailyBody.FindSubmatch(body)
	// 代码不存在时返回 null 或空数组。
	if match == nil {
		return nil, nil
	}
	var rows []struct {
		D, O, H, L, C, V string
	}
	if err := json.Unmarshal(match[1], &rows); err != nil {
		return nil, fmt.Errorf("新浪美股日线格式无效: %w", err)
	}
	bars := make([]*Bar, 0, len(rows))
	for _, row := range rows {
		bar := &Bar{Date: row.D}
		values := []*float64{&bar.Open, &bar.High, &bar.Low, &bar.Close}
		valid := len(row.D) == 10
		for i, text := range []string{row.O, row.H, row.L, row.C} {
			value, err := strconv.ParseFloat(text, 64)
			if err != nil || value <= 0 {
				valid = false
				break
			}
			*values[i] = value
		}
		if !valid {
			continue
		}
		bar.Volume, _ = strconv.ParseInt(row.V, 10, 64)
		bars = append(bars, bar)
	}
	return bars, nil
}
