package fred

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/alwqx/sec/provider/forex"
)

const forexCSVURL = "https://fred.stlouisfed.org/graph/fredgraph.csv?id=DEXCHUS,DEXHKUS"

func parseForexCSV(raw string) ([]forex.Observation, map[string][2]string, error) {
	r := csv.NewReader(strings.NewReader(raw))
	header, err := r.Read()
	if err != nil || len(header) != 3 || (header[0] != "observation_date" && header[0] != "DATE") || header[1] != "DEXCHUS" || header[2] != "DEXHKUS" {
		return nil, nil, fmt.Errorf("FRED汇率 CSV 表头无效")
	}
	items := []forex.Observation{}
	coverage := map[string][2]string{}
	previous := ""
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("FRED汇率 CSV 记录无效：%w", err)
		}
		date, err := time.Parse("2006-01-02", row[0])
		if err != nil || row[0] <= previous {
			return nil, nil, fmt.Errorf("FRED汇率日期重复、无效或未按日期排序")
		}
		previous = row[0]
		for i, pair := range forex.FREDPairs {
			value, err := forex.Number(row[i+1])
			if err != nil {
				return nil, nil, err
			}
			item := forex.NewObservation(pair, date.Format("2006-01-02"), forexCSVURL, time.Time{})
			item.Value = value
			item.RawValues[header[i+1]] = row[i+1]
			if value == nil {
				item.Note = "来源当日无观测值"
			} else {
				bounds := coverage[pair]
				if bounds[0] == "" {
					bounds[0] = item.Date
				}
				bounds[1] = item.Date
				coverage[pair] = bounds
			}
			items = append(items, item)
		}
	}
	if len(items) == 0 || len(coverage) == 0 {
		return nil, nil, fmt.Errorf("FRED汇率 CSV 未包含有效历史")
	}
	return items, coverage, nil
}

// ForexHistory 是美联储 H.10 的纽约中午买入汇率，不能与北京时间的市场收盘价拼接。
func (c *Client) ForexHistory(ctx context.Context, req *forex.Request) (*forex.Result, error) {
	if err := req.Validate(forex.FREDPairs, ""); err != nil {
		return nil, err
	}
	var items []forex.Observation
	var coverage map[string][2]string
	_, fetched, err := c.csvSnapshot(ctx, forexCSVURL, "forex.json", func(raw string) error { var err error; items, coverage, err = parseForexCSV(raw); return err })
	if err != nil {
		return nil, err
	}
	result := &forex.Result{Provider: "Federal Reserve via FRED", PriceType: "noon-buying-rate", Frequency: "daily", Timezone: "America/New_York", ObservationTime: "12:00", DateBasis: "observation-date", Begin: req.Begin, End: req.End, DataVersion: "current-visible-revision", Observations: []forex.Observation{}, SnapshotCoverage: map[string][2]string{}, Notes: []string{"美联储H.10纽约中午电汇买入汇率，按周发布日度观测；不是人民币中间价或中国市场收盘价。", "不进行季调，缺失值保留为null；各币种历史起点独立，不补周末、不拼接不同来源。"}}
	selected := map[string]bool{}
	for _, pair := range req.Pairs {
		selected[pair] = true
		if bound, ok := coverage[pair]; ok {
			result.SnapshotCoverage[pair] = bound
		}
	}
	for _, item := range items {
		date := strings.ReplaceAll(item.Date, "-", "")
		if selected[item.Pair] && date >= req.Begin && date <= req.End {
			item.FetchedAt = fetched
			result.Observations = append(result.Observations, item)
		}
	}
	return result, nil
}
