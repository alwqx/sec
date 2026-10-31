package macro

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/alwqx/sec/provider/chinabond"
	"github.com/alwqx/sec/provider/macrodata"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func newTreasuryCLI() *cobra.Command {
	command := &cobra.Command{Use: "treasury", Short: "中债国债到期收益率曲线（3个月、6个月及1、2、5、10年期）", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		begin, end, err := treasuryDates(cmd, time.Now())
		if err != nil {
			return err
		}
		start, _ := time.Parse("20060102", begin)
		finish, _ := time.Parse("20060102", end)
		result, err := chinabond.GetChinaBond(cmd.Context(), &chinabond.GetChinaBondReq{
			Start: start.Format("2006-01-02"), End: finish.Format("2006-01-02"),
		})
		if err != nil {
			return err
		}
		standard, _ := cmd.Flags().GetBool("standard-json")
		if standard {
			dataset, err := macrodata.CNTreasury(result, chinabond.HistoryQueryURL, begin, end)
			return writeDataset(cmd, dataset, err)
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(result)
		}
		return printTreasury(cmd.OutOrStdout(), result)
	}}
	command.Flags().StringP("begin", "b", "", "开始日期 YYYYMMDD，默认截至日前29天，最早20060301")
	command.Flags().StringP("end", "e", "", "截至日期 YYYYMMDD，默认北京时间今天；单次区间小于一年")
	command.Flags().Bool("json", false, "输出中债原始字段 JSON")
	addStandardJSONFlag(command, "按统一宏观序列格式输出JSON，保留全部关键期限和原文")
	command.Flags().BoolP("debug", "D", false, "显示调试日志")
	return command
}

func treasuryDates(cmd *cobra.Command, now time.Time) (string, string, error) {
	begin, _ := cmd.Flags().GetString("begin")
	end, _ := cmd.Flags().GetString("end")
	if end == "" {
		end = now.In(time.FixedZone("CST", 8*60*60)).Format("20060102")
	}
	if begin == "" {
		date, err := time.Parse("20060102", end)
		if err != nil {
			return "", "", fmt.Errorf("无效截至日期: %s", end)
		}
		begin = date.AddDate(0, 0, -29).Format("20060102")
		if begin < chinabond.CoverageStart {
			begin = chinabond.CoverageStart
		}
	}
	for _, date := range []string{begin, end} {
		if _, err := time.Parse("20060102", date); err != nil || len(date) != 8 {
			return "", "", fmt.Errorf("无效日期 %q，请使用 YYYYMMDD", date)
		}
	}
	if begin > end {
		return "", "", fmt.Errorf("开始日期不能晚于截至日期")
	}
	if begin < chinabond.CoverageStart {
		return "", "", fmt.Errorf("国债收益率曲线查询支持自20060301起")
	}
	if end > now.In(time.FixedZone("CST", 8*60*60)).Format("20060102") {
		return "", "", fmt.Errorf("截至日期不能晚于北京时间今天")
	}
	return begin, end, nil
}

func printTreasury(out io.Writer, result *chinabond.GetChinaBondResp) error {
	if len(result.HeList) == 0 {
		_, err := fmt.Fprintln(out, "所选日期区间未返回国债收益率数据（不代表收益率为零；未区分休市、尚未发布或上游缺口）")
		return err
	}
	items := append([]*chinabond.ChinaBondItem(nil), result.HeList...)
	sort.Slice(items, func(i, j int) bool { return items[i].Date < items[j].Date })
	table := tablewriter.NewWriter(out)
	table.SetBorder(false)
	table.SetHeader([]string{"日期", "3个月（%）", "6个月（%）", "1年期（%）", "2年期（%）", "5年期（%）", "10年期（%）", "状态"})
	for _, item := range items {
		row := []string{item.Date}
		status := "完整"
		for _, text := range []string{item.ThreeMonth, item.SixMonth, item.OneYear, item.TwoYear, item.FiveYear, item.TenYear} {
			text = strings.TrimSpace(text)
			if text == "" {
				text = "—"
				status = "期限缺失"
			}
			row = append(row, text)
		}
		table.Append(append(row, status))
	}
	table.Render()
	_, err := fmt.Fprintln(out, "来源：中央国债登记结算有限责任公司；中债国债到期收益率曲线（CNY）。空值为 —，不补值；整天无记录不自动判为休市。JSON输出接口字段，逐日公布时间与历史修订版本不可得。")
	return err
}
