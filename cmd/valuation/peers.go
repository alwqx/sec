package valuation

import (
	"fmt"
	"io"

	shared "github.com/alwqx/sec/valuation"
)

// peerListSize 是文本输出列出的公司数，按总市值排序；--json 输出全部。
const peerListSize = 10

func peerMetricRow(name string, m shared.PeerMetric) []string {
	if m.Short {
		return []string{name, ff(m.Current), "样本不足", "-", "-", "-", fmt.Sprintf("%d 家", m.Samples)}
	}
	rank, percent, premium := "-", "-", "-"
	if m.Rank != nil {
		rank = fmt.Sprintf("%d/%d", *m.Rank, m.Samples)
		percent = fmt.Sprintf("%.0f%%", *m.Percentile)
		premium = fmt.Sprintf("%+.1f%%", *m.Premium)
	}
	return []string{name, ff(m.Current), ff(m.Median), premium, rank, percent, fmt.Sprintf("%d 家，排除 %d 家", m.Samples, m.Excluded)}
}

func printPeerMetrics(out io.Writer, peers *shared.Peers) {
	fmt.Fprintf(out, "\n【同行业对比】%s，%s，共 %d 家\n", peers.Board, peers.Date, len(peers.Items))
	richTable(out, []string{"指标", "当前值", "行业中位", "相对中位", "排名(低到高)", "分位", "样本"}, [][]string{
		peerMetricRow("P/E", peers.PE), peerMetricRow("P/B", peers.PB), peerMetricRow("P/S", peers.PS),
	})
}

// peerMultiple 显示倍数，小于等于 0 或缺失时没有比较意义。
func peerMultiple(v *float64) string {
	if v == nil || *v <= 0 {
		return "-"
	}
	return fmt.Sprintf("%.2f", *v)
}

func printPeers(out io.Writer, r *shared.Result) {
	fmt.Fprintln(out, "方法: 比较同一交易日、东方财富同一行业公司的 PE、PB、PS")
	if r.Peers == nil {
		fmt.Fprintf(out, "\n【评估】%s\n", r.PeersNote)
		return
	}
	printPeerMetrics(out, r.Peers)
	fmt.Fprintln(out, "\n倍数小于等于 0 或缺失的公司不参与比较，中位数偏向盈利公司。行业内公司的业务和增长可能差别很大。")
	rows := [][]string{}
	listed := 0
	for _, item := range r.Peers.Items {
		if listed >= peerListSize && !item.Self {
			continue
		}
		listed++
		name := item.Name
		if item.Self {
			name = "▶ " + name
		}
		rows = append(rows, []string{item.Code, name, moneyOrDash(item.MarketCap), peerMultiple(item.PE), peerMultiple(item.PB), peerMultiple(item.PS)})
	}
	fmt.Fprintf(out, "\n【总市值前 %d 家及本公司】\n", peerListSize)
	richTable(out, []string{"代码", "名称", "总市值", "P/E", "P/B", "P/S"}, rows)
}
