package macro

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/alwqx/sec/provider/federalreserve"
	"github.com/alwqx/sec/provider/macrodata"
	"github.com/alwqx/sec/utils"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func newUSTargetRateCLI() *cobra.Command {
	command := &cobra.Command{Use: "target-rate", Aliases: []string{"fed-funds-target"}, Short: "美国联邦基金目标区间及调整事件（美联储官方）", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			begin, end, err := queryTargetRateDates(cmd, time.Now())
			if err != nil {
				return err
			}
			req := &federalreserve.TargetRateRequest{Begin: begin, End: end}
			if err := req.Validate(); err != nil {
				return err
			}
			cacheDir := ""
			noCache, _ := cmd.Flags().GetBool("no-cache")
			if !noCache {
				cacheDir, err = utils.SecDir("cache", "federalreserve")
				if err != nil {
					return err
				}
			}
			client := federalreserve.NewClient(cacheDir)
			client.Refresh, _ = cmd.Flags().GetBool("refresh")
			result, err := client.TargetRate(cmd.Context(), req)
			if err != nil {
				return err
			}
			standard, _ := cmd.Flags().GetBool("standard-json")
			if standard {
				dataset, err := macrodata.TargetRate(result)
				return writeDataset(cmd, dataset, err)
			}
			asJSON, _ := cmd.Flags().GetBool("json")
			if asJSON {
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(result)
			}
			return printUSTargetRate(cmd.OutOrStdout(), result)
		}}
	addQueryFlags(command)
	addStandardJSONFlag(command, "按统一宏观序列格式输出JSON，保留调整事件、生效与公布日期，只输出窗口内实际调整")
	command.Flags().Lookup("begin").Usage = "开始生效日期 YYYYMMDD，默认截至日期前29天；最早" + federalreserve.EarliestDate
	command.Flags().Lookup("end").Usage = "截至生效日期 YYYYMMDD，默认UTC今天；保留截至当日有效的区间"
	return command
}

func queryTargetRateDates(cmd *cobra.Command, now time.Time) (string, string, error) {
	begin, _ := cmd.Flags().GetString("begin")
	end, _ := cmd.Flags().GetString("end")
	if end == "" {
		end = now.UTC().Format("20060102")
	}
	date, err := time.Parse("20060102", end)
	if err != nil || date.Format("20060102") != end {
		return "", "", fmt.Errorf("无效截至日期 %q，请使用YYYYMMDD", end)
	}
	if begin == "" {
		begin = date.AddDate(0, 0, -29).Format("20060102")
		if begin < federalreserve.EarliestDate {
			begin = federalreserve.EarliestDate
		}
	}
	return begin, end, nil
}

func printUSTargetRate(out io.Writer, result *federalreserve.TargetRateResult) error {
	if result.Current == nil {
		_, err := fmt.Fprintln(out, "截至查询日期没有可用的目标区间")
		return err
	}
	formatRate := func(value float64) string { return strconv.FormatFloat(value, 'f', 2, 64) }
	current := result.Current
	asOf := result.End
	if date, err := time.Parse("20060102", asOf); err == nil {
		asOf = date.Format("2006-01-02")
	}
	if _, err := fmt.Fprintf(out, "截至 %s 有效目标区间：%s%%–%s%%（自 %s 生效）\n", asOf, formatRate(current.LowerPercent), formatRate(current.UpperPercent), current.EffectiveDate); err != nil {
		return err
	}
	table := tablewriter.NewWriter(out)
	table.SetBorder(false)
	table.SetHeader([]string{"记录", "生效日期", "下限（%）", "上限（%）", "下限变化（基点）", "上限变化（基点）", "公布日期", "公布时间"})
	selected := make([]*federalreserve.TargetRateEvent, 0, len(result.Events)+1)
	if result.Baseline != nil {
		selected = append(selected, result.Baseline)
	}
	selected = append(selected, result.Events...)
	for _, event := range selected {
		label := "调整事件"
		if event == result.Baseline {
			label = "区间前基准"
		}
		announced, published := "未取得", "未取得"
		if event.AnnouncedDate != nil {
			announced = *event.AnnouncedDate
		}
		if event.PublishedAt != nil {
			published = event.PublishedAt.Format("15:04 MST")
		}
		change := func(value *float64) string {
			if value == nil {
				return "—"
			}
			return strconv.FormatFloat(*value, 'f', 0, 64)
		}
		table.Append([]string{label, event.EffectiveDate, formatRate(event.LowerPercent), formatRate(event.UpperPercent), change(event.LowerChangeBasisPoints), change(event.UpperChangeBasisPoints), announced, published})
	}
	table.Render()
	if len(result.Events) == 0 {
		if _, err := fmt.Fprintln(out, "所选生效日期区间内没有目标区间调整事件，以上目标区间仍然有效。"); err != nil {
			return err
		}
	}
	for _, event := range selected {
		if event.ReleaseStatus != "available" {
			if _, err := fmt.Fprintf(out, "%s 公布信息：%s\n", event.EffectiveDate, event.ReleaseNote); err != nil {
				return err
			}
		}
	}
	_, err := fmt.Fprintln(out, "来源：美联储官方调整表及FOMC声明；单位：%，1个百分点=100基点。按生效日期筛选实际调整，不补每日数据或未调整会议；基准可以早于查询区间。公布日期和时间仅取自可核验声明，未取得字段在JSON中为null。2008-12-16由单一目标转为区间，不计算跨机制变化；历史为当前可见版本。")
	return err
}
