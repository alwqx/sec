package macro

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/alwqx/sec/provider/fred"
	"github.com/alwqx/sec/provider/macrodata"
	"github.com/alwqx/sec/utils"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func newUSGDPCLI() *cobra.Command {
	command := &cobra.Command{Use: "gdp", Short: "美国实际GDP季度水平、同比与官方环比年化增速", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		begin, end, err := queryUSGDPQuarters(cmd, time.Now())
		if err != nil {
			return err
		}
		vintages, _ := cmd.Flags().GetBool("vintages")
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
		result, err := client.GDP(cmd.Context(), &fred.GDPRequest{Begin: begin, End: end, Vintages: vintages})
		if err != nil {
			return err
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		standard, _ := cmd.Flags().GetBool("standard-json")
		if standard {
			dataset, err := macrodata.USGDP(result)
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
		return printUSGDP(cmd.OutOrStdout(), result)
	}}
	addQueryFlags(command)
	command.Flags().Bool("standard-json", false, "按统一宏观序列格式输出JSON，区分折年水平、年化及未年化增速")
	command.MarkFlagsMutuallyExclusive("json", "standard-json")
	command.Flags().Lookup("begin").Usage = "开始季度 YYYYQ1至YYYYQ4，默认截至季度前7个季度；最早1947Q1"
	command.Flags().Lookup("end").Usage = "截至季度 YYYYQ1至YYYYQ4，默认UTC上一个已结束季度"
	command.Flags().Bool("vintages", false, "附带BEA官方发布版本档案（2002Q1起的各版实际增速及发布日期）")
	return command
}

func queryUSGDPQuarters(cmd *cobra.Command, now time.Time) (string, string, error) {
	begin, _ := cmd.Flags().GetString("begin")
	end, _ := cmd.Flags().GetString("end")
	if end == "" {
		now = now.UTC()
		q := now.Year()*4 + (int(now.Month())-1)/3 - 1
		end = fmt.Sprintf("%04dQ%d", q/4, q%4+1)
	}
	if err := (&fred.GDPRequest{Begin: end, End: end}).Validate(); err != nil {
		return "", "", err
	}
	if begin == "" {
		year, _ := strconv.Atoi(end[:4])
		q := year*4 + int(end[5]-'1') - 7
		begin = fmt.Sprintf("%04dQ%d", q/4, q%4+1)
		if begin < "1947Q1" {
			begin = "1947Q1"
		}
	}
	if err := (&fred.GDPRequest{Begin: begin, End: end}).Validate(); err != nil {
		return "", "", err
	}
	return begin, end, nil
}

func printUSGDP(out io.Writer, result *fred.GDPResult) error {
	format := func(value *float64, precision int) string {
		if value == nil {
			return "—"
		}
		return strconv.FormatFloat(*value, 'f', precision, 64)
	}
	table := tablewriter.NewWriter(out)
	table.SetBorder(false)
	table.SetHeader([]string{"季度", "实际GDP（十亿美元）", "环比年化（%）", "同比（%）", "环比（%）", "说明"})
	for _, item := range result.Observations {
		table.Append([]string{item.Period, format(item.Level, 3), format(item.QoQAnnualized, 1), format(item.YoY, 2), format(item.QoQ, 2), item.Note})
	}
	table.Render()
	if _, err := fmt.Fprintln(out, "来源：BEA，经FRED转接；2017年链式美元，季调折年水平，并非季度实际金额。环比年化为官方发布值；同比与环比由当前可见修订水平计算，未年化、不跨缺口。空值为 —。"); err != nil {
		return err
	}
	if result.VintagesRequested {
		versions := tablewriter.NewWriter(out)
		versions.SetBorder(false)
		versions.SetHeader([]string{"季度", "官方版本名称", "发布日期", "实际环比年化（%）", "发布说明"})
		for _, item := range result.Observations {
			for _, version := range item.Vintages {
				versions.Append([]string{item.Period, gdpEstimateLabel(version.Estimate), version.ReleaseDate, format(version.QoQAnnualized, 1), gdpReleaseNote(version.Note)})
			}
		}
		versions.Render()
		_, err := fmt.Fprintf(out, "版本档案来源：BEA Vintage History，覆盖%s至%s。保留官方版本名称，未将当前水平当作初值；档案不含历史实际GDP水平，real_level=null。发布日期无具体时分；部分档案行注明GDP未修订。原始名称与说明可通过--json查看。\n", result.VintagesFirstPeriod, result.VintagesLastPeriod)
		return err
	}
	return nil
}

func gdpEstimateLabel(estimate string) string {
	labels := map[string]string{"Advance": "初值", "Second": "二次估计", "Third": "三次估计", "Revised": "修订值", "Initial": "首次估计", "Updated": "更新值", "Preliminary": "初步值", "Final": "终值"}
	if label, ok := labels[estimate]; ok {
		return label + "（" + estimate + "）"
	}
	return "其他版本（原名见JSON）"
}

func gdpReleaseNote(note string) string {
	if note == "" {
		return ""
	}
	var parts []string
	if strings.Contains(note, "GDI not published") {
		parts = append(parts, "未发布GDI")
	}
	if strings.Contains(note, "GDI percent change not published") {
		parts = append(parts, "未发布GDI增速")
	}
	if strings.Contains(note, "GDP not open for revision") || strings.Contains(note, "GDP not revised") {
		parts = append(parts, "GDP未修订")
	}
	if len(parts) == 0 {
		return "其他发布说明（原文见JSON）"
	}
	return strings.Join(parts, "；")
}
