package macro

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/alwqx/sec/provider/macrodata"
	"github.com/alwqx/sec/provider/nyfed"
	"github.com/alwqx/sec/utils"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func newUSEFFRCLI() *cobra.Command {
	command := &cobra.Command{Use: "effr", Short: "美国有效联邦基金利率（纽约联储日频EFFR）", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		begin, end, err := queryEFFRDates(cmd, time.Now())
		if err != nil {
			return err
		}
		req := &nyfed.EFFRRequest{Begin: begin, End: end}
		if err := req.Validate(); err != nil {
			return err
		}
		cacheDir := ""
		noCache, _ := cmd.Flags().GetBool("no-cache")
		if !noCache {
			cacheDir, err = utils.SecDir("cache", "nyfed")
			if err != nil {
				return err
			}
		}
		client := nyfed.NewClient(cacheDir)
		client.Refresh, _ = cmd.Flags().GetBool("refresh")
		result, err := client.EFFR(cmd.Context(), req)
		if err != nil {
			return err
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		standard, _ := cmd.Flags().GetBool("standard-json")
		if standard {
			dataset, err := macrodata.EFFR(result)
			return writeDataset(cmd, dataset, err)
		}
		if asJSON {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(result)
		}
		return printUSEFFR(cmd.OutOrStdout(), result)
	}}
	addQueryFlags(command)
	addStandardJSONFlag(command, "按统一宏观序列格式输出JSON，保留交易所属日、方法变化与修订标记")
	command.Flags().Lookup("begin").Usage = "开始所属日期 YYYYMMDD，默认截至日期前29天；最早20000703"
	command.Flags().Lookup("end").Usage = "截至所属日期 YYYYMMDD，默认UTC今天；仅返回已公布观测"
	return command
}

func queryEFFRDates(cmd *cobra.Command, now time.Time) (string, string, error) {
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
		if begin < "20000703" {
			begin = "20000703"
		}
	}
	return begin, end, nil
}

func printUSEFFR(out io.Writer, result *nyfed.EFFRResult) error {
	if len(result.Observations) == 0 {
		_, err := fmt.Fprintln(out, "所选日期区间内没有已公布的EFFR观测，周末及节假日不补值")
		return err
	}
	table := tablewriter.NewWriter(out)
	table.SetBorder(false)
	table.SetHeader([]string{"所属日期", "EFFR（%）", "较前次观测（基点）", "前次所属日期", "修订标记"})
	for _, item := range result.Observations {
		change := "—"
		if item.ChangeBasisPoints != nil {
			change = strconv.FormatFloat(*item.ChangeBasisPoints, 'f', 2, 64)
		}
		previous := item.PreviousDate
		if previous == "" {
			previous = "—"
		}
		revision := item.RevisionIndicator
		if revision == "" {
			revision = "—"
		}
		table.Append([]string{item.EffectiveDate, strconv.FormatFloat(item.RatePercent, 'f', 2, 64), change, previous, revision})
	}
	table.Render()
	_, err := fmt.Fprintln(out, "来源：纽约联邦储备银行；单位：%，1个百分点=100基点。变化比较上一可用观测，前值可早于查询区间；周末、节假日及未公布日期不补值。所属日期为隔夜交易日，API未提供逐条实际公布时间。2016-03-01起改用FR2420交易量加权中位数，此前为经纪商数据加权均值，跨界变化不计算；历史为当前可见修订值。JSON保留官方原字段、脚注和修订标记。")
	return err
}
