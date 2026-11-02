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

// MaxBars 是 K 线接口一次能返回的最多条数：日线约 4 年，周线约 20 年，月线约 85 年。
const MaxBars = 1023

// K 线周期，对应接口的 scale 参数（分钟）。
const (
	ScaleDay   = 240
	ScaleWeek  = 1200
	ScaleMonth = 7200
)

// Bar 是一条不复权 K 线。接口没有成交额。
type Bar struct {
	Date   string // YYYY-MM-DD，周线和月线是该周期最后一个交易日
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume int64 // 单位：股
}

// QueryBars 查询沪深证券最近的不复权 K 线，按日期升序。symbol 形如 sh600036。没有数据时返回空列表。
func QueryBars(ctx context.Context, symbol string, scale, count int) ([]*Bar, error) {
	reqURL := fmt.Sprintf("https://money.finance.sina.com.cn/quotes_service/api/json_v2.php/CN_MarketData.getKLineData?symbol=%s&scale=%d&ma=no&datalen=%d", symbol, scale, count)
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
	return parseBars(body)
}

func parseBars(body []byte) ([]*Bar, error) {
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
		return nil, fmt.Errorf("解析新浪 K 线失败: %w", err)
	}
	bars := make([]*Bar, 0, len(rows))
	for _, row := range rows {
		bar := &Bar{Date: row.Day}
		for _, field := range []struct {
			text   string
			target *float64
		}{{row.Open, &bar.Open}, {row.High, &bar.High}, {row.Low, &bar.Low}, {row.Close, &bar.Close}} {
			value, err := strconv.ParseFloat(field.text, 64)
			if err != nil {
				return nil, fmt.Errorf("解析新浪 K 线 %s 失败: %w", row.Day, err)
			}
			*field.target = value
		}
		volume, err := strconv.ParseInt(row.Volume, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("解析新浪 K 线 %s 失败: %w", row.Day, err)
		}
		bar.Volume = volume
		bars = append(bars, bar)
	}
	return bars, nil
}
