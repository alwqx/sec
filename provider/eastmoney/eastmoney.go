package eastmoney

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/alwqx/sec/utils"
)

// 东方财富接口封装

const (
	MarketTypeSzSe MarketType = 0 // 深圳证券交易所
	MarketTypeSse  MarketType = 1 // 上海证券交易所

	EastMoney80Push2ApiBase             = "http://80.push2.eastmoney.com"
	EastMoneyPush2ApiBase               = "http://push2.eastmoney.com"
	EastMoneyPush2HisApiBase            = "https://push2his.eastmoney.com"
	QuoteFQTDefault          FuQuanType = 0 // 不复权
	QuoteFQTFront            FuQuanType = 1 // 前复权
	QuoteFQTPost             FuQuanType = 2 // 后复权
)

var (
	ErrInvalidKLine = errors.New("invalid kline data")
	ErrSkipValue    = errors.New("skip value")
)

// getOriginQuoteHistory 获取原始的证券历史行情信息
// api: https://efinance.readthedocs.io/en/latest/api.html#efinance.stock.get_quote_history
func getOriginQuoteHistory(req *GetQuoteHistoryReq) (res *QuoteHistoryResp, err error) {
	if req == nil {
		err = errors.New("req is nil")
		return
	}

	// 1.600036 1表示上证 0表示深证，这里要用 东方财富的 code 格式
	code := fmt.Sprintf("%d.%s", req.MarketCode, req.Code)
	values := url.Values{}
	values.Add("secid", code)
	values.Add("beg", req.Begin)
	values.Add("end", req.End)

	values.Add("fields1", "f1,f2,f3,f4,f5,f6,f7,f8,f9,f10,f11,f12,f13")
	values.Add("fields2", "f51,f52,f53,f54,f55,f56,f57,f58,f59,f60,f61")
	values.Add("rtntype", "6")
	values.Add("klt", "101")
	switch req.FQT {
	case QuoteFQTDefault:
		values.Add("fqt", "0")
	case QuoteFQTFront:
		values.Add("fqt", "1")
	case QuoteFQTPost:
		values.Add("fqt", "2")
	default:
		values.Add("fqt", "0")
	}

	reqURL := fmt.Sprintf("%s/api/qt/stock/kline/get?%s", EastMoneyPush2HisApiBase, values.Encode())
	resp, err := utils.MakeRequest(http.MethodGet, reqURL, nil, nil)
	if err != nil {
		slog.Error("[GetCurrentStockInfo]", "request", reqURL, "error: %v", err)
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("[GetCurrentStockInfo]", "read body error: %v", err)
		return nil, err
	}
	err = json.Unmarshal(body, &res)

	return
}

// GetQuoteHistory 获取标准证券历史行情信息
// api: https://efinance.readthedocs.io/en/latest/api.html#efinance.stock.get_quote_history
func GetQuoteHistory(req *GetQuoteHistoryReq) (res []*Quote, err error) {
	if req == nil {
		err = errors.New("req is nil")
		return
	}

	tmpRes, err := getOriginQuoteHistory(req)
	if err != nil {
		slog.Error("[GetCurrentStockInfo]", "request", req.Code, "error: %v", err)
		return nil, err
	}

	return ParseQuoteHistoryResp(tmpRes)
}

// ParseQuoteHistoryResp 解析数据结构到标准结构体
func ParseQuoteHistoryResp(resp *QuoteHistoryResp) ([]*Quote, error) {
	if resp == nil || resp.Data == nil {
		return nil, errors.New("nil data")
	}

	data := resp.Data
	res := make([]*Quote, 0, len(data.Klines))
	for _, kline := range data.Klines {
		kl, err := ParseKlineItem(kline)
		if err != nil {
			slog.Error("[ParseQuoteHistoryResp] parse %s error: %v", kline, err)
			return nil, err
		}
		item := &Quote{
			Date:       kl.Date,
			Code:       data.Code,
			Name:       data.Name,
			Market:     data.Market,
			Open:       kl.Open,
			Close:      kl.Close,
			High:       kl.High,
			Low:        kl.Low,
			Volume:     kl.Volume,
			TurnOver:   kl.TurnOver,
			Amplitude:  kl.Amplitude,
			ChangeRate: kl.ChangeRate,
			Change:     kl.Change,
			Velocity:   kl.Velocity,
		}
		res = append(res, item)
	}

	return res, nil
}

// ParseKlineItem 解析单条 k 线数据
// "2024-12-26,39.40,39.48,39.54,39.01,539252,2125139425.00,1.35,0.20,0.08,0.26"
func ParseKlineItem(line string) (kline KLineQuote, err error) {
	toks := strings.Split(line, ",")
	if len(toks) != 11 {
		slog.Error("ParseKlineItem", "invalid kline data %s", line)
		err = ErrInvalidKLine
		return
	}

	kline.Date, err = time.Parse(utils.LayoutYYMMDD, toks[0])
	if err != nil {
		return
	}
	kline.Open, err = strconv.ParseFloat(toks[1], 64)
	if err != nil {
		return
	}
	kline.Close, err = strconv.ParseFloat(toks[2], 64)
	if err != nil {
		return
	}

	kline.High, err = strconv.ParseFloat(toks[3], 64)
	if err != nil {
		return
	}
	kline.Low, err = strconv.ParseFloat(toks[4], 64)
	if err != nil {
		return
	}

	kline.Volume, err = strconv.ParseInt(toks[5], 10, 64)
	if err != nil {
		return
	}
	kline.TurnOver, err = strconv.ParseFloat(toks[6], 64)
	if err != nil {
		return
	}

	kline.Amplitude, err = strconv.ParseFloat(toks[7], 64)
	if err != nil {
		return
	}
	kline.ChangeRate, err = strconv.ParseFloat(toks[8], 64)
	if err != nil {
		return
	}
	kline.Change, err = strconv.ParseFloat(toks[9], 64)
	if err != nil {
		return
	}

	kline.Velocity, err = strconv.ParseFloat(toks[10], 64)
	return
}
