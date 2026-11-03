//go:build windows || darwin

package main

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alwqx/sec/backtest"
	"github.com/alwqx/sec/indicator"
	"github.com/alwqx/sec/provider/eastmoney"
	quotehistory "github.com/alwqx/sec/provider/history"
)

type strategySeries struct {
	Label  string     `json:"label"`
	Panel  string     `json:"panel"` // price 或 indicator
	Style  string     `json:"style"` // line 或 bar
	Values []*float64 `json:"values"`
}

type strategySignal struct {
	Date   string  `json:"date"`
	Type   string  `json:"type"`
	Price  float64 `json:"price"`
	Reason string  `json:"reason"`
}

var strategyParamLabels = map[string]string{"fast": "快线周期", "slow": "慢线周期", "signal": "信号线周期", "period": "周期", "overbought": "超买阈值", "oversold": "超卖阈值", "k": "倍数"}

func intParam(q url.Values, name string, fallback, low, high int) (int, error) {
	text := q.Get(name)
	if text == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(text)
	if err != nil || value < low || value > high {
		return 0, fmt.Errorf("%s须为 %d 至 %d 的整数", strategyParamLabels[name], low, high)
	}
	return value, nil
}

func floatParam(q url.Values, name string, fallback, low, high float64) (float64, error) {
	text := q.Get(name)
	if text == "" {
		return fallback, nil
	}
	value, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(value) || value <= low || value > high {
		return 0, fmt.Errorf("%s须大于 %g 且不超过 %g", strategyParamLabels[name], low, high)
	}
	return value, nil
}

// strategyPlan 是校验后的策略：所需最少交易日数、参数和计算函数。compute 用完整数据计算，序列从下标 skip 开始返回。
type strategyPlan struct {
	required int
	params   map[string]any
	levels   []float64
	compute  func(closes []float64, skip int) ([]strategySeries, []indicator.Signal)
}

func trimSeries(label, panel, style string, values []float64, start, first int) strategySeries {
	return strategySeries{label, panel, style, nullBefore(values, start, first)}
}

func parseStrategy(q url.Values) (strategyPlan, error) {
	switch kind := q.Get("kind"); kind {
	case "", "ma":
		fast, err := intParam(q, "fast", 5, 1, maxOverlayPeriod)
		if err != nil {
			return strategyPlan{}, err
		}
		slow, err := intParam(q, "slow", 20, 1, maxOverlayPeriod)
		if err != nil {
			return strategyPlan{}, err
		}
		if fast >= slow {
			return strategyPlan{}, fmt.Errorf("快线周期须小于慢线周期")
		}
		return strategyPlan{slow, map[string]any{"fast": fast, "slow": slow}, nil, func(closes []float64, skip int) ([]strategySeries, []indicator.Signal) {
			fastMA, slowMA := indicator.SMA(closes, fast), indicator.SMA(closes, slow)
			return []strategySeries{
				trimSeries(fmt.Sprintf("MA%d", fast), "price", "line", fastMA, fast-1, skip),
				trimSeries(fmt.Sprintf("MA%d", slow), "price", "line", slowMA, slow-1, skip),
			}, indicator.MASignals(fastMA, slowMA, slow-1)
		}}, nil
	case "macd":
		fast, err := intParam(q, "fast", 12, 1, maxOverlayPeriod)
		if err != nil {
			return strategyPlan{}, err
		}
		slow, err := intParam(q, "slow", 26, 1, maxOverlayPeriod)
		if err != nil {
			return strategyPlan{}, err
		}
		signal, err := intParam(q, "signal", 9, 1, maxOverlayPeriod)
		if err != nil {
			return strategyPlan{}, err
		}
		if fast >= slow {
			return strategyPlan{}, fmt.Errorf("快线周期须小于慢线周期")
		}
		return strategyPlan{slow + signal, map[string]any{"fast": fast, "slow": slow, "signal": signal}, nil, func(closes []float64, skip int) ([]strategySeries, []indicator.Signal) {
			line, signalLine, histogram := indicator.MACD(closes, fast, slow, signal)
			start := indicator.MACDStart(fast, slow, signal)
			return []strategySeries{
				trimSeries("柱", "indicator", "bar", histogram, start, skip),
				trimSeries("MACD", "indicator", "line", line, slow-1, skip),
				trimSeries("信号线", "indicator", "line", signalLine, start, skip),
			}, indicator.MACDSignals(line, signalLine, start)
		}}, nil
	case "rsi":
		period, err := intParam(q, "period", 14, 1, maxOverlayPeriod)
		if err != nil {
			return strategyPlan{}, err
		}
		overbought, err := floatParam(q, "overbought", 70, 0, 100)
		if err != nil {
			return strategyPlan{}, err
		}
		oversold, err := floatParam(q, "oversold", 30, 0, 100)
		if err != nil {
			return strategyPlan{}, err
		}
		if oversold >= overbought {
			return strategyPlan{}, fmt.Errorf("超卖阈值须小于超买阈值")
		}
		return strategyPlan{period + 1, map[string]any{"period": period, "overbought": overbought, "oversold": oversold}, []float64{oversold, overbought}, func(closes []float64, skip int) ([]strategySeries, []indicator.Signal) {
			rsi := indicator.RSI(closes, period)
			return []strategySeries{trimSeries(fmt.Sprintf("RSI(%d)", period), "indicator", "line", rsi, period, skip)}, indicator.RSISignals(rsi, period, overbought, oversold)
		}}, nil
	case "boll":
		period, err := intParam(q, "period", 20, 1, maxOverlayPeriod)
		if err != nil {
			return strategyPlan{}, err
		}
		k, err := floatParam(q, "k", 2, 0, 10)
		if err != nil {
			return strategyPlan{}, err
		}
		return strategyPlan{period, map[string]any{"period": period, "k": k}, nil, func(closes []float64, skip int) ([]strategySeries, []indicator.Signal) {
			mid, upper, lower := indicator.Bollinger(closes, period, k)
			return []strategySeries{
				trimSeries("上轨", "price", "line", upper, period-1, skip),
				trimSeries("中轨", "price", "line", mid, period-1, skip),
				trimSeries("下轨", "price", "line", lower, period-1, skip),
			}, indicator.BollingerSignals(closes, upper, lower, period-1)
		}}, nil
	default:
		return strategyPlan{}, fmt.Errorf("不支持的策略 %s，可选 ma、macd、rsi、boll", kind)
	}
}

// parseBacktestConfig 读取回测参数，费率以 % 为单位，留空用默认值。
func parseBacktestConfig(q url.Values) (backtest.Config, error) {
	cfg := backtest.DefaultConfig()
	for _, item := range []struct {
		name, label string
		target      *float64
		percent     bool
	}{{"cash", "初始资金", &cfg.Cash, false}, {"commission", "佣金费率", &cfg.Commission, true}, {"min_commission", "最低佣金", &cfg.MinCommission, false}, {"stamp_tax", "印花税率", &cfg.StampTax, true}, {"slippage", "滑点", &cfg.Slippage, true}} {
		text := q.Get(item.name)
		if text == "" {
			continue
		}
		value, err := strconv.ParseFloat(text, 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			return cfg, fmt.Errorf("%s须为数字", item.label)
		}
		if item.percent {
			value /= 100
		}
		*item.target = value
	}
	return cfg, cfg.Validate()
}

// backtestNote 说明不能回测的原因；能回测时返回空字符串。用复权价格时不会退回备用数据，所以不用另外判断。
func backtestNote(code string, query historyQuery) string {
	switch {
	case strings.HasPrefix(code, "HK"):
		return "回测只支持 A 股个股：港股没有涨跌停、可以 T+0，每手股数也不同。"
	case backtest.Supported(code) != nil:
		return backtest.Supported(code).Error()
	case query.freq != eastmoney.FreqDay:
		return "回测只支持日线。"
	case query.fqt == eastmoney.QuoteFQTDefault:
		return "回测需要复权价格，不复权价格在除权日会出现虚假的涨跌，请选择前复权或后复权。"
	}
	return ""
}

// handleStrategy 计算技术指标和信号；沪深 A 股的复权日线同时按信号回测。结果由规则机械生成，不构成投资建议。
func (s *desktopServer) handleStrategy(w http.ResponseWriter, r *http.Request) {
	history, ok := parseHistoryQuery(w, r)
	if !ok {
		return
	}
	plan, err := parseStrategy(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// 回测参数有误时照常返回指标和信号，只在回测里说明，页面上的参数表单不会消失。
	cfg, cfgErr := parseBacktestConfig(r.URL.Query())
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	// 预热数据不进入结果。
	warmup := quotehistory.IndicatorWarmup(history.begin, plan.required, history.freq)
	fetched, err := quotehistory.GetFrom(ctx, &eastmoney.GetQuoteHistoryReq{
		Code: history.symbol, MarketCode: history.market, Begin: warmup.Format("20060102"), End: history.end.Format("20060102"), FQT: history.fqt, Freq: history.freq,
	}, history.begin.Format("20060102"))
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		slog.ErrorContext(ctx, "查询技术分析行情失败", "error", err)
		writeError(w, http.StatusBadGateway, historyErrorMessage(err))
		return
	}
	quotes := make([]*eastmoney.Quote, 0, len(fetched.Quotes))
	for _, item := range fetched.Quotes {
		if item != nil {
			quotes = append(quotes, item)
		}
	}
	beginDay := history.begin.Format("2006-01-02")
	first := sort.Search(len(quotes), func(i int) bool { return quotes[i].Date.Format("2006-01-02") >= beginDay })
	closes := make([]float64, len(quotes))
	for i, quote := range quotes {
		closes[i] = quote.Close
	}
	dates := make([]string, 0, len(quotes)-first)
	for _, quote := range quotes[first:] {
		dates = append(dates, quote.Date.Format("2006-01-02"))
	}
	series, rawSignals := plan.compute(closes, first)
	signals := []strategySignal{}
	for _, signal := range rawSignals {
		if signal.Index >= first {
			signals = append(signals, strategySignal{quotes[signal.Index].Date.Format("2006-01-02"), signal.Type, closes[signal.Index], signal.Reason})
		}
	}
	code := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("code")))
	note := backtestNote(code, history)
	if note == "" && cfgErr != nil {
		note = cfgErr.Error()
	}
	var result *backtest.Result
	if note == "" && len(quotes) >= plan.required && first < len(quotes) {
		if result, err = backtest.RunQuotes(code, quotes, first, rawSignals, cfg); err != nil {
			note = err.Error()
		}
	}
	writeJSON(w, map[string]any{
		"params": plan.params, "levels": plan.levels, "dates": dates, "closes": closes[first:], "series": series, "signals": signals,
		"required": plan.required, "insufficient": len(quotes) < plan.required, "source_note": fetched.Note,
		"backtest": result, "backtest_note": note,
	})
}
