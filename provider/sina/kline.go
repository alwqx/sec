package sina

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/alwqx/sec/utils"
)

// MaxDailyBars 是日线接口一次能返回的最多条数，约 4 年。
const MaxDailyBars = 1023

// DailyBar 是一条不复权日线。接口没有成交额。
type DailyBar struct {
	Date   string // YYYY-MM-DD
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume int64 // 单位：股
}

// QueryDailyBars 查询沪深证券最近的不复权日线，按日期升序。symbol 形如 sh600036。没有数据时返回空列表。
func QueryDailyBars(ctx context.Context, symbol string, count int) ([]*DailyBar, error) {
	reqURL := fmt.Sprintf("https://money.finance.sina.com.cn/quotes_service/api/json_v2.php/CN_MarketData.getKLineData?symbol=%s&scale=240&ma=no&datalen=%d", symbol, count)
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
	return parseDailyBars(body)
}

func parseDailyBars(body []byte) ([]*DailyBar, error) {
	// 代码不存在时返回 null。
	if text := strings.TrimSpace(string(body)); text == "" || text == "null" {
		return nil, nil
	}
	var rows []struct {
		Day    string `json:"day"`
		Open   string `json:"open"`
		High   string `json:"high"`
		Low    string `json:"low"`
		Close  string `json:"close"`
		Volume string `json:"volume"`
	}
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("解析新浪日线失败: %w", err)
	}
	bars := make([]*DailyBar, 0, len(rows))
	for _, row := range rows {
		bar := &DailyBar{Date: row.Day}
		for _, field := range []struct {
			text   string
			target *float64
		}{{row.Open, &bar.Open}, {row.High, &bar.High}, {row.Low, &bar.Low}, {row.Close, &bar.Close}} {
			value, err := strconv.ParseFloat(field.text, 64)
			if err != nil {
				return nil, fmt.Errorf("解析新浪日线 %s 失败: %w", row.Day, err)
			}
			*field.target = value
		}
		volume, err := strconv.ParseInt(row.Volume, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("解析新浪日线 %s 失败: %w", row.Day, err)
		}
		bar.Volume = volume
		bars = append(bars, bar)
	}
	return bars, nil
}
