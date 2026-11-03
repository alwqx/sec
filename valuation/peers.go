package valuation

import (
	"fmt"
	"sort"
	"strings"

	"github.com/alwqx/sec/provider/eastmoney"
)

const (
	// 参与比较的公司少于这个数时，中位数和排名没有意义，只列出同行。
	minPeerSamples = 5
	// 参与比较的公司少于这个数时，分位太粗，不计入规则汇总。
	minPeerSignalSamples = 10
)

// PeerMetric 是一个倍数在同行业的位置。倍数小于等于 0 或缺失的公司不参与比较，所以中位数偏向盈利公司。
type PeerMetric struct {
	Current    *float64 `json:"current"`
	Samples    int      `json:"samples"`  // 参与比较的公司数，包含自身
	Excluded   int      `json:"excluded"` // 倍数小于等于 0 或缺失的公司数
	Short      bool     `json:"short"`    // 样本不足，不计算中位数和排名
	Median     *float64 `json:"median"`
	Rank       *int     `json:"rank"`       // 从低到高，1 是最低
	Percentile *float64 `json:"percentile"` // 中位秩：（低于当前值的公司数 + 并列公司数的一半）÷ 公司数，单位 %
	Premium    *float64 `json:"premium"`    // 相对中位数，单位 %
}

// Peer 是一家同行业公司。倍数是上游原值，亏损或净资产为负时为负数，缺失时为 nil。
type Peer struct {
	Code      string   `json:"code"`
	Name      string   `json:"name"`
	Self      bool     `json:"self"`
	MarketCap *float64 `json:"market_cap"`
	PE        *float64 `json:"pe"`
	PB        *float64 `json:"pb"`
	PS        *float64 `json:"ps"`
}

// Peers 是同一交易日、东方财富同一行业的估值比较，Items 按总市值降序。
type Peers struct {
	Board string     `json:"board"`
	Date  string     `json:"date"`
	PE    PeerMetric `json:"pe"`
	PB    PeerMetric `json:"pb"`
	PS    PeerMetric `json:"ps"`
	Items []Peer     `json:"items"`
}

// buildPeerMetric 计算一个倍数的行业位置。ranked 为 false 表示列表里没有本公司，只给行业中位数。
func buildPeerMetric(items []Peer, current *float64, ranked bool, field func(Peer) *float64) PeerMetric {
	metric := PeerMetric{Current: current}
	valid := []float64{}
	for _, item := range items {
		if value := field(item); value != nil && *value > 0 {
			valid = append(valid, *value)
		}
	}
	metric.Samples, metric.Excluded = len(valid), len(items)-len(valid)
	if len(valid) < minPeerSamples {
		metric.Short = true
		return metric
	}
	sort.Float64s(valid)
	n := len(valid)
	median := medianSorted(valid)
	metric.Median = &median
	if current == nil || !ranked {
		return metric
	}
	// 同行家数少时，“不高于当前值的占比”会把自身算进去，分位偏高；中位秩两头对称。
	below := sort.SearchFloat64s(valid, *current)
	notAbove := sort.Search(n, func(i int) bool { return valid[i] > *current })
	rank := below + 1
	metric.Rank = &rank
	metric.Percentile = ptr((float64(below) + float64(notAbove-below)/2) / float64(n) * 100)
	metric.Premium = ptr((*current/median - 1) * 100)
	return metric
}

func peerSignal(name, board string, metric PeerMetric) (Signal, bool) {
	if metric.Samples < minPeerSignalSamples || metric.Percentile == nil {
		return Signal{}, false
	}
	value := *metric.Percentile
	return Signal{name, percentileLevel(value), fmt.Sprintf("%s %d 家中从低到高第 %d，%.0f%% 分位（%s）", board, metric.Samples, *metric.Rank, value, percentileBands)}, true
}

// applyPeers 写入同行业比较，并把 PE、PB 的行业位置计入规则汇总。没有同行数据时只写入说明。
func applyPeers(r *Result, rows []*eastmoney.PeerValuation) {
	if len(rows) == 0 {
		r.PeersNote = "没有同行业数据，规则汇总未计入行业规则"
		return
	}
	items := make([]Peer, 0, len(rows))
	hasSelf := false
	for _, row := range rows {
		self := strings.EqualFold(row.ExCode, r.Code)
		hasSelf = hasSelf || self
		items = append(items, Peer{Code: row.ExCode, Name: row.Name, Self: self, MarketCap: row.MarketCap, PE: row.PE, PB: row.PB, PS: row.PS})
	}
	peers := &Peers{Board: r.Industry, Date: r.TradeDate, Items: items}
	peers.PE = buildPeerMetric(items, r.PE.Current, hasSelf, func(p Peer) *float64 { return p.PE })
	peers.PB = buildPeerMetric(items, r.PB.Current, hasSelf, func(p Peer) *float64 { return p.PB })
	peers.PS = buildPeerMetric(items, r.PS.Current, hasSelf, func(p Peer) *float64 { return p.PS })
	r.Peers = peers
	if signal, ok := peerSignal("行业 PE", peers.Board, peers.PE); ok {
		r.Signals = append(r.Signals, signal)
	}
	if signal, ok := peerSignal("行业 PB", peers.Board, peers.PB); ok {
		r.Signals = append(r.Signals, signal)
	}
	r.Assessment = assess(r.Signals)
}
