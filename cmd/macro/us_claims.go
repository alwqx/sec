package macro

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/alwqx/sec/provider/fred"
	"github.com/alwqx/sec/provider/macrodata"
	"github.com/alwqx/sec/utils"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func newUSClaimsCLI(indicator string) *cobra.Command {
	short := "美国初请失业金（季调周度申请数及四周均值）"
	if indicator == "continued-claims" {
		short = "美国续请失业金（季调参保失业人数及四周均值）"
	}
	command := &cobra.Command{Use: indicator, Short: short, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		begin, end, err := queryUSWeeks(cmd, time.Now())
		if err != nil {
			return err
		}
		req := &fred.ClaimsRequest{Indicator: indicator, Begin: begin, End: end}
		if err := req.Validate(); err != nil {
			return err
		}
		cacheDir := ""
		noCache, _ := cmd.Flags().GetBool("no-cache")
		if !noCache {
			cacheDir, err = utils.SecDir("cache", "fred")
			if err != nil {
				return err
			}
		}
		client := fred.NewClient(cacheDir)
		client.Refresh, _ = cmd.Flags().GetBool("refresh")
		result, err := client.Claims(cmd.Context(), req)
		if err != nil {
			return err
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		standard, _ := cmd.Flags().GetBool("standard-json")
		if standard {
			dataset, err := macrodata.Claims(result)
			if err != nil {
				return err
			}
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(dataset)
		}
		if asJSON {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(result)
		}
		return printUSClaims(cmd.OutOrStdout(), result)
	}}
	addQueryFlags(command)
	command.Flags().Bool("standard-json", false, "按统一宏观序列格式输出JSON，保留所属周、前周基期与四周均值窗口")
	command.MarkFlagsMutuallyExclusive("json", "standard-json")
	command.Flags().Lookup("begin").Usage = "开始日期 YYYYMMDD，默认截至日期前最近12个周六；最早19670107"
	command.Flags().Lookup("end").Usage = "截至日期 YYYYMMDD，默认UTC今天；按区间内的周六所属周筛选"
	return command
}

func queryUSWeeks(cmd *cobra.Command, now time.Time) (string, string, error) {
	begin, _ := cmd.Flags().GetString("begin")
	end, _ := cmd.Flags().GetString("end")
	if end == "" {
		end = now.UTC().Format("20060102")
	}
	if begin == "" {
		date, err := time.Parse("20060102", end)
		if err != nil || len(end) != 8 {
			return "", "", fmt.Errorf("无效截至日期 %q，请使用YYYYMMDD", end)
		}
		days := (int(date.Weekday()) - int(time.Saturday) + 7) % 7
		begin = date.AddDate(0, 0, -days-11*7).Format("20060102")
		if begin < "19670107" {
			begin = "19670107"
		}
	}
	return begin, end, nil
}

func printUSClaims(out io.Writer, result *fred.ClaimsResult) error {
	if len(result.Observations) == 0 {
		_, err := fmt.Fprintln(out, "所选日期区间内没有周六所属周")
		return err
	}
	table := tablewriter.NewWriter(out)
	table.SetBorder(false)
	label := "初请申请数（件）"
	if result.Indicator == "continued-claims" {
		label = "续请参保失业数（人）"
	}
	table.SetHeader([]string{"所属周截至（周六）", label, "较上周变化", "四周均值（计算值）", "说明"})
	format := func(value *float64, digits int) string {
		if value == nil {
			return "—"
		}
		return strconv.FormatFloat(*value, 'f', digits, 64)
	}
	for _, item := range result.Observations {
		table.Append([]string{item.WeekEnding, format(item.Count, 0), format(item.WeeklyChange, 0), format(item.FourWeekAverage, 2), item.Note})
	}
	table.Render()
	_, err := fmt.Fprintf(out, "来源：美国劳工部ETA，经FRED转接；常规州失业保险项目，季调，单位：%s。四周均值为当周及前三个连续周的计算值，缺口不补。初请、续请按各自周六所属日期记录，最新所属周可能不同；初请不等于全部新增失业人数，续请不等于全部失业人口。快照中本系列最新可用所属周：%s。未发布周显示为 —；历史为当前可见修订值。\n", result.Unit, result.LatestAvailableWeek)
	return err
}
