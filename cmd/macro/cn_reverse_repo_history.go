package macro

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/alwqx/sec/provider/pbc"
	"github.com/alwqx/sec/utils"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func newCNReverseRepoHistoryCLI() *cobra.Command {
	command := &cobra.Command{
		Use:   "reverse-repo-history",
		Short: "中国人民银行固定利率机制切换前的七天逆回购中标利率历史",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			begin, _ := cmd.Flags().GetString("begin")
			end, _ := cmd.Flags().GetString("end")
			if end == "" {
				end = pbc.ReverseRepoHistoryVerifiedThrough
			}
			if begin == "" {
				endDate, err := time.Parse("20060102", end)
				if err != nil {
					return fmt.Errorf("无效截至日期: %s", end)
				}
				begin = endDate.AddDate(-1, 0, 0).Format("20060102")
				if begin < pbc.ReverseRepoHistoryCoverageStart {
					begin = pbc.ReverseRepoHistoryCoverageStart
				}
			}
			req := pbc.ReverseRepoHistoryRequest{Begin: begin, End: end}
			if err := req.Validate(); err != nil {
				return err
			}
			cacheDir := ""
			noCache, _ := cmd.Flags().GetBool("no-cache")
			if !noCache {
				var err error
				cacheDir, err = utils.SecDir("cache", "pbc")
				if err != nil {
					return err
				}
			}
			client := pbc.NewClient(cacheDir)
			client.Refresh, _ = cmd.Flags().GetBool("refresh")
			result, err := client.ReverseRepoHistory(cmd.Context(), req)
			if err != nil {
				return err
			}
			limit, _ := cmd.Flags().GetInt("limit")
			if limit < 0 {
				return fmt.Errorf("--limit 不能小于0")
			}
			if limit > 0 && len(result.Events) > limit {
				result.Events = result.Events[len(result.Events)-limit:]
			}
			asJSON, _ := cmd.Flags().GetBool("json")
			if asJSON {
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(result)
			}
			return printCNReverseRepoHistory(cmd.OutOrStdout(), result)
		},
	}
	addQueryFlags(command)
	command.Flags().Lookup("begin").Usage = "开始公告公布日期 YYYYMMDD，默认最近12个月可用历史"
	command.Flags().Lookup("end").Usage = "结束公告公布日期 YYYYMMDD，默认20240719"
	command.Flags().Int("limit", 20, "最多显示最近记录数；设为0显示区间内全部结果")
	return command
}

func printCNReverseRepoHistory(out io.Writer, result pbc.ReverseRepoHistoryResult) error {
	if len(result.Events) == 0 {
		if _, err := fmt.Fprintln(out, "所选公布日期区间内没有明确列出7天期中标利率的操作公告"); err != nil {
			return err
		}
	} else {
		table := tablewriter.NewWriter(out)
		table.SetBorder(false)
		table.SetHeader([]string{"公布日", "公告时间（北京时间）", "操作日（正文明确时）", "期限", "中标利率（%）"})
		for _, event := range result.Events {
			operationDate := event.OperationDate
			if operationDate == "" {
				operationDate = "未明示"
			}
			rate := strconv.FormatFloat(event.RatePercent, 'f', -1, 64)
			table.Append([]string{event.PublicationDate, event.PublishedAt, operationDate, event.Term, rate})
		}
		table.Render()
	}
	_, err := fmt.Fprintf(out, "共 %d 条；默认显示最近记录。来源：%s；发布机构为%s，承载站点为%s。覆盖2015-01-22至2024-07-19；2024-07-22起机制切换，后段固定利率政策调整见 reverse-repo 命令。无明确7天中标利率的公告不补值。更新状态：%s。\n", result.TotalEvents, "中国货币网公开市场操作公告目录", "中国人民银行公开市场业务操作室", "中国货币网", result.UpdateStatus)
	return err
}
