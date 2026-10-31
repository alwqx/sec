package macro

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"time"

	"github.com/alwqx/sec/provider/macrodata"
	"github.com/alwqx/sec/provider/pbc"
	"github.com/alwqx/sec/utils"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func newRRRCLI() *cobra.Command {
	command := &cobra.Command{Use: "rrr", Short: "人民币存款准备金率调整、适用机构与分批生效日期（自2018年起）", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		begin, _ := cmd.Flags().GetString("begin")
		end, _ := cmd.Flags().GetString("end")
		if begin == "" {
			begin = pbc.RRRCoverageStart
		}
		if end == "" {
			end = time.Now().In(time.FixedZone("CST", 8*60*60)).Format("20060102")
		}
		req := pbc.RRRRequest{Begin: begin, End: end}
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
		result, err := client.RRR(cmd.Context(), req)
		if err != nil {
			return err
		}
		standard, _ := cmd.Flags().GetBool("standard-json")
		if standard {
			dataset, err := macrodata.RRR(&result)
			return writeDataset(cmd, dataset, err)
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(result)
		}
		return printRRR(cmd.OutOrStdout(), result)
	}}
	addQueryFlags(command)
	addStandardJSONFlag(command, "按统一宏观序列格式输出JSON，保留调整幅度区间和机构范围，不推算准备金率水平")
	command.Flags().Lookup("begin").Usage = "开始生效日期 YYYYMMDD，默认20180101"
	command.Flags().Lookup("end").Usage = "截至生效日期 YYYYMMDD，默认北京时间今天"
	return command
}

func printRRR(out io.Writer, result pbc.RRRResult) error {
	if len(result.Events) == 0 {
		_, err := fmt.Fprintln(out, "所选生效日期区间内未检索到人民币存款准备金率调整公告（不代表准备金率为零）")
		return err
	}
	format := func(n float64) string { return strconv.FormatFloat(math.Abs(n), 'f', -1, 64) }
	table := tablewriter.NewWriter(out)
	table.SetBorder(false)
	table.SetHeader([]string{"生效日期", "公布时间（北京时间）", "类别", "调整（百分点）", "适用机构范围", "豁免条件"})
	for _, event := range result.Events {
		date := event.EffectiveDate
		if parsed, err := time.Parse("20060102", date); err == nil {
			date = parsed.Format("2006-01-02")
		}
		published := event.PublishedAt
		if parsed, err := time.Parse(time.RFC3339, published); err == nil {
			published = parsed.In(time.FixedZone("CST", 8*60*60)).Format("2006-01-02 15:04:05")
		}
		category := "定向"
		if event.ScopeType == "broad" {
			category = "全面"
		}
		if event.Additional {
			category += "（额外）"
		}
		if event.TrancheCount > 1 {
			category += fmt.Sprintf("（第%d/%d批）", event.TrancheIndex, event.TrancheCount)
		}
		change := "上调" + format(event.ChangePPLower)
		if event.ChangePPUpper < 0 {
			change = "下调" + format(event.ChangePPUpper)
			if event.ChangePPLower != event.ChangePPUpper {
				change += "～" + format(event.ChangePPLower)
			}
		} else if event.ChangePPLower != event.ChangePPUpper {
			change += "～" + format(event.ChangePPUpper)
		}
		table.Append([]string{date, published, category, change, event.Scope, event.Exclusions})
	}
	table.Render()
	_, err := fmt.Fprintln(out, "来源：人民银行人民币准备金率决策公告。按生效日和适用范围拆分；额外降幅、豁免条件与分批安排依原文，不将不同机构的变动相加或推算统一准备金率。公布时间为网页当前记录，JSON保留来源与原文。")
	return err
}
