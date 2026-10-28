package macro

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/alwqx/sec/provider/fred"
	"github.com/alwqx/sec/utils"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func newUSProductionCLI(indicator string) *cobra.Command {
	short := "美国工业生产指数及同比、月环比（季调）"
	if indicator == "capacity-utilization" {
		short = "美国产能利用率及上月、上年同月百分点差（季调）"
	}
	command := &cobra.Command{Use: indicator, Short: short, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		sector, _ := cmd.Flags().GetString("sector")
		req := &fred.ProductionRequest{Indicator: indicator, Sector: sector}
		start, err := req.EarliestMonth()
		if err != nil {
			return err
		}
		req.Begin, req.End, err = queryUSMonths(cmd, time.Now(), start)
		if err != nil {
			return err
		}
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
		result, err := client.Production(cmd.Context(), req)
		if err != nil {
			return err
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(result)
		}
		return printUSProduction(cmd.OutOrStdout(), result)
	}}
	addQueryFlags(command)
	command.Flags().Lookup("begin").Usage = "开始月份 YYYYMM，默认截至月份前11个月，并按选中系列起点截断"
	command.Flags().Lookup("end").Usage = "截至月份 YYYYMM，默认UTC上个月"
	command.Flags().String("sector", "total", "部门：total（工业总量）、manufacturing（制造业，NAICS 31–33）")
	return command
}

func printUSProduction(out io.Writer, result *fred.ProductionResult) error {
	table := tablewriter.NewWriter(out)
	table.SetBorder(false)
	headers := []string{"月份", "生产指数（2017=100）", "同比（%）", "月环比（%）", "说明"}
	if result.Indicator == "capacity-utilization" {
		headers = []string{"月份", "产能利用率（%）", "较上年同月（百分点）", "较上月（百分点）", "说明"}
	}
	table.SetHeader(headers)
	format := func(value *float64, digits int) string {
		if value == nil {
			return "—"
		}
		return strconv.FormatFloat(*value, 'f', digits, 64)
	}
	for _, item := range result.Observations {
		date, _ := time.Parse("200601", item.Period)
		yoy, mom := item.YoY, item.MoM
		if result.Indicator == "capacity-utilization" {
			yoy, mom = item.YoYPercentagePoints, item.MoMPercentagePoints
		}
		table.Append([]string{date.Format("2006-01"), format(item.Level, 4), format(yoy, 2), format(mom, 2), item.Note})
	}
	table.Render()
	method := "工业生产指数以2017=100，衡量实际产出；同比和月环比为计算增速，未年化。"
	if result.Indicator == "capacity-utilization" {
		method = "产能利用率水平单位为百分比；变化是较上月、上年同月的百分点差。"
	}
	_, err := fmt.Fprintf(out, "来源：美国联邦储备委员会G.17，经FRED转接；%s（%s），月度季调。%s官方总量与制造业系列分别使用，不相加；制造业分项采用NAICS 31–33，工业总量另包含伐木及部分出版业。当前可见修订版本，缺口不补，JSON保留原值和严格基期；采集时间不等于实际公布时间。\n", result.Coverage, result.SeriesID, method)
	return err
}
