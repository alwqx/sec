package valuation

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/alwqx/sec/provider/eastmoney"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func peerRows(pes ...float64) []*eastmoney.PeerValuation {
	rows := make([]*eastmoney.PeerValuation, len(pes))
	for i, pe := range pes {
		rows[i] = &eastmoney.PeerValuation{ExCode: fmt.Sprintf("SH60000%d", i), Name: fmt.Sprintf("公司%d", i), PE: num(pe), PB: num(pe / 10)}
	}
	return rows
}

func TestBuildPeerMetric(t *testing.T) {
	items := []Peer{{PE: num(10)}, {PE: num(20)}, {PE: num(20)}, {PE: num(40)}, {PE: num(50)}, {PE: num(-5)}, {PE: nil}}
	field := func(p Peer) *float64 { return p.PE }
	metric := buildPeerMetric(items, num(20), true, field)
	require.Equal(t, 5, metric.Samples)
	require.Equal(t, 2, metric.Excluded, "亏损和缺失的公司不参与比较")
	require.Equal(t, 20.0, *metric.Median)
	require.Equal(t, 2, *metric.Rank, "并列时取靠前的名次")
	require.Equal(t, 40.0, *metric.Percentile, "并列时算一半")
	require.InDelta(t, 0, *metric.Premium, 1e-9)

	lossMaking := buildPeerMetric(items, nil, true, field)
	require.NotNil(t, lossMaking.Median, "自身亏损时仍显示行业中位数")
	require.Nil(t, lossMaking.Rank)
	require.Nil(t, lossMaking.Percentile)
	require.Nil(t, lossMaking.Premium)

	short := buildPeerMetric(items[:4], num(20), true, field)
	require.True(t, short.Short)
	require.Nil(t, short.Median)
	require.Nil(t, short.Rank)

	even := buildPeerMetric([]Peer{{PE: num(1)}, {PE: num(2)}, {PE: num(3)}, {PE: num(4)}, {PE: num(5)}, {PE: num(6)}}, num(1), true, field)
	require.Equal(t, 3.5, *even.Median)
	require.Equal(t, 1, *even.Rank)
}

// 第 k 低和第 k 高离两端一样远，判断要对称，否则结论会偏向偏高。
func TestPeerPercentileIsSymmetric(t *testing.T) {
	items := make([]Peer, 10)
	for i := range items {
		items[i] = Peer{PE: num(float64(i + 1))}
	}
	field := func(p Peer) *float64 { return p.PE }
	counts := map[string]int{}
	for i := range items {
		value := *buildPeerMetric(items, items[i].PE, true, field).Percentile
		mirror := *buildPeerMetric(items, items[len(items)-1-i].PE, true, field).Percentile
		require.InDelta(t, 100, value+mirror, 1e-9)
		counts[percentileLevel(value)]++
	}
	require.Equal(t, counts["low"], counts["high"])
}

func TestPeersWithoutSelfHaveNoRank(t *testing.T) {
	r := Compute(flat(1, 1000, 1, 2), nil, DefaultDCFParams())
	r.Code = "SZ000001"
	before := len(r.Signals)
	applyPeers(r, peerRows(10, 20, 30, 40, 50, 60, 70, 80, 90, 100))
	require.Equal(t, 1000.0, *r.Peers.PE.Current)
	require.NotNil(t, r.Peers.PE.Median)
	require.Nil(t, r.Peers.PE.Rank, "不能出现 11/10")
	require.Nil(t, r.Peers.PE.Percentile)
	require.Len(t, r.Signals, before, "不计入规则汇总")
}

func TestApplyPeers(t *testing.T) {
	r := Compute(flat(1, 10, 1, 2), nil, DefaultDCFParams())
	r.Code = "SH600000"
	require.Equal(t, "数据不足", r.Assessment)

	applyPeers(r, nil)
	require.Nil(t, r.Peers, "没有同行数据时不写入")
	require.Contains(t, r.PeersNote, "没有同行业数据")

	r.PeersNote = ""
	applyPeers(r, peerRows(10, 20, 30, 40, 50, 60, 70, 80, 90, 100))
	require.Equal(t, r.Industry, r.Peers.Board)
	require.Equal(t, r.TradeDate, r.Peers.Date)
	require.Empty(t, r.PeersNote)
	require.True(t, r.Peers.Items[0].Self)
	require.False(t, r.Peers.Items[1].Self)
	require.True(t, r.Peers.PS.Short, "PS 全部缺失")
	names := []string{}
	for _, signal := range r.Signals {
		names = append(names, signal.Name)
	}
	require.Contains(t, names, "行业 PE")
	require.Contains(t, names, "行业 PB")
	require.Equal(t, "偏低区间", r.Assessment)
}

func TestFewPeersStayOutOfSignals(t *testing.T) {
	r := Compute(flat(1, 10, 1, 2), nil, DefaultDCFParams())
	r.Code = "SH600000"
	before := len(r.Signals)
	applyPeers(r, peerRows(10, 20, 30, 40, 50))
	require.NotNil(t, r.Peers.PE.Median, "可以比较，但分位太粗")
	require.Len(t, r.Signals, before)
}

const (
	peerHistoryBody = `{"success":true,"code":0,"result":{"data":[{"SECURITY_NAME_ABBR":"招商银行","BOARD_CODE":"016029","BOARD_NAME":"银行Ⅱ","TRADE_DATE":"2026-10-30 00:00:00","CLOSE_PRICE":40,"TOTAL_MARKET_CAP":4e11,"TOTAL_SHARES":1e10,"PE_TTM":8,"PB_MRQ":0.8,"PS_TTM":3}]}}`
	emptyReports    = `{"success":false,"code":9201,"message":"返回数据为空","result":null}`
)

func mockPeerUpstream() {
	gock.New("https://datacenter-web.eastmoney.com").Get("/api/data/v1/get").MatchParam("reportName", "RPT_VALUEANALYSIS_DET").MatchParam("filter", "SECUCODE").Reply(200).BodyString(peerHistoryBody)
	gock.New("https://datacenter-web.eastmoney.com").Get("/api/data/v1/get").MatchParam("reportName", "RPT_LICO_FN_CPD").Reply(200).BodyString(emptyReports)
}

func TestAnalyzePeers(t *testing.T) {
	defer gock.Off()
	mockPeerUpstream()
	gock.New("https://datacenter-web.eastmoney.com").Get("/api/data/v1/get").MatchParam("filter", `BOARD_CODE="016029".*TRADE_DATE='2026-10-30'`).Reply(200).
		BodyString(`{"success":true,"code":0,"result":{"data":[{"SECUCODE":"600036.SH","SECURITY_NAME_ABBR":"招商银行","TOTAL_MARKET_CAP":4e11,"PE_TTM":8,"PB_MRQ":0.8,"PS_TTM":3},{"SECUCODE":"bad","SECURITY_NAME_ABBR":"坏数据"}]}}`)
	r, err := Analyze(context.Background(), "sh600036", DefaultDCFParams())
	require.NoError(t, err)
	require.Equal(t, "2026-10-30", r.Peers.Date)
	require.Len(t, r.Peers.Items, 1, "代码无效的行跳过")
	require.True(t, r.Peers.Items[0].Self)
}

func TestAnalyzePeerFailureKeepsValuation(t *testing.T) {
	defer gock.Off()
	mockPeerUpstream()
	gock.New("https://datacenter-web.eastmoney.com").Get("/api/data/v1/get").MatchParam("filter", "BOARD_CODE").Reply(500)
	r, err := Analyze(context.Background(), "SH600036", DefaultDCFParams())
	require.NoError(t, err, "同行业对比失败不影响其他结果")
	require.Equal(t, 40.0, r.Price)
	require.Nil(t, r.Peers)
	require.Contains(t, r.PeersNote, "未计入行业规则")
	for _, signal := range r.Signals {
		require.NotContains(t, signal.Name, "行业")
	}
}

func TestAnalyzeWithoutBoard(t *testing.T) {
	defer gock.Off()
	gock.New("https://datacenter-web.eastmoney.com").Get("/api/data/v1/get").MatchParam("reportName", "RPT_VALUEANALYSIS_DET").Reply(200).
		BodyString(strings.Replace(peerHistoryBody, `"BOARD_CODE":"016029",`, "", 1))
	gock.New("https://datacenter-web.eastmoney.com").Get("/api/data/v1/get").MatchParam("reportName", "RPT_LICO_FN_CPD").Reply(200).BodyString(emptyReports)
	r, err := Analyze(context.Background(), "SH600036", DefaultDCFParams())
	require.NoError(t, err)
	require.Contains(t, r.PeersNote, "没有同行业数据")
}
