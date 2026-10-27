package macro

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/alwqx/sec/provider/chinabond"
	"github.com/alwqx/sec/utils"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func newTreasuryCLI() *cobra.Command {
	command := &cobra.Command{Use: "treasury", Short: "中债国债到期收益率曲线（1、2、5、10年期）", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		begin, end, err := treasuryDates(cmd, time.Now())
		if err != nil {
			return err
		}
		req := &chinabond.TreasuryRequest{Begin: begin, End: end}
		if err := req.Validate(); err != nil {
			return err
		}
		cacheDir := ""
		noCache, _ := cmd.Flags().GetBool("no-cache")
		if !noCache {
			cacheDir, err = utils.SecDir("cache", "chinabond")
			if err != nil {
				return err
			}
		}
		client := chinabond.NewClient(cacheDir)
		client.Refresh, _ = cmd.Flags().GetBool("refresh")
		result, err := client.Treasury(cmd.Context(), req)
		if err != nil {
			return err
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(result)
		}
		return printTreasury(cmd.OutOrStdout(), result)
	}}
	addQueryFlags(command)
	command.Flags().Lookup("begin").Usage = "开始日期 YYYYMMDD，默认截至日前29天，最早20060301"
	command.Flags().Lookup("end").Usage = "截至日期 YYYYMMDD，默认北京时间今天"
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
	return begin, end, nil
}

func printTreasury(out io.Writer, result *chinabond.TreasuryResult) error {
	if len(result.Observations) == 0 {
		_, err := fmt.Fprintln(out, "所选日期区间未返回国债收益率数据（不代表收益率为零；未区分休市、尚未发布或上游缺口）")
		return err
	}
	table := tablewriter.NewWriter(out)
	table.SetBorder(false)
	table.SetHeader([]string{"日期", "1年期（%）", "2年期（%）", "5年期（%）", "10年期（%）", "状态"})
	for _, item := range result.Observations {
		date := item.Date
		if parsed, err := time.Parse("20060102", date); err == nil {
			date = parsed.Format("2006-01-02")
		}
		row := []string{date}
		for _, key := range []string{"1y", "2y", "5y", "10y"} {
			value := "—"
			if n := item.Yields[key]; n != nil {
				value = strconv.FormatFloat(*n, 'f', 4, 64)
			}
			row = append(row, value)
		}
		status := "完整"
		if len(item.MissingTenors) > 0 {
			status = "期限缺失"
		}
		table.Append(append(row, status))
	}
	table.Render()
	_, err := fmt.Fprintln(out, "来源：中央国债登记结算有限责任公司；中债国债到期收益率曲线（CNY）。日期按北京时间显示，空值为 —，不补值；整天无记录不自动判为休市。逐日公布时间与历史修订版本不可得，JSON保留来源与抓取时间。")
	return err
}
