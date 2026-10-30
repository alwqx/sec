package macro

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/alwqx/sec/provider/mof"
	"github.com/alwqx/sec/utils"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func newCNFiscalCLI() *cobra.Command {
	command := &cobra.Command{Use: "fiscal", Short: "全国一般公共预算、政府性基金累计收支与官方同比", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		begin, end, err := cnFiscalMonths(cmd, time.Now())
		if err != nil {
			return err
		}
		budget, _ := cmd.Flags().GetString("budget")
		req := &mof.FiscalRequest{Begin: begin, End: end, Budget: budget}
		if err := req.Validate(); err != nil {
			return err
		}
		cache := ""
		noCache, _ := cmd.Flags().GetBool("no-cache")
		if !noCache {
			cache, err = utils.SecDir("cache", "mof")
			if err != nil {
				return err
			}
		}
		client := mof.NewClient(cache)
		client.Refresh, _ = cmd.Flags().GetBool("refresh")
		result, err := client.Fiscal(cmd.Context(), req)
		if err != nil {
			return err
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(result)
		}
		return printCNFiscal(cmd.OutOrStdout(), result)
	}}
	addQueryFlags(command)
	command.Flags().String("budget", "all", "预算类型：all、general（一般公共预算）、fund（政府性基金）")
	command.Flags().Lookup("begin").Usage = "开始月份 YYYYMM，最早201501；默认截至月份前11个月"
	return command
}

func printCNFiscal(out io.Writer, result *mof.FiscalResult) error {
	if _, err := fmt.Fprintln(out, "全国财政收支：当年累计，名义金额；未季调、未年化。"); err != nil {
		return err
	}
	format := func(n *float64, digits int) string {
		if n == nil {
			return "—"
		}
		return strconv.FormatFloat(*n, 'f', digits, 64)
	}
	table := tablewriter.NewWriter(out)
	table.SetBorder(false)
	table.SetHeader([]string{"累计所属期", "指标", "累计（亿元）", "官方同比（%）"})
	adjusted := false
	for _, item := range result.Observations {
		period := item.Period[:4] + "年1—" + strings.TrimLeft(item.Period[4:], "0") + "月"
		for _, m := range item.Metrics {
			growth := format(m.YTDYoY, 1)
			if m.ComparableYTDYoY != nil || m.TaxRefundAdjustedYTDYoY != nil {
				growth += "*"
				adjusted = true
			}
			table.Append([]string{period, m.Name, format(m.YTDAmount, 0), growth})
		}
	}
	table.Render()
	note := "来源：财政部；1—2月不拆分，空值为 —；收支差额不等于官方赤字。"
	if adjusted {
		note += "* 另有同口径或扣除留抵退税因素后的同比，见 --json。"
	}
	_, err := fmt.Fprintln(out, note+"详细来源、公布时间及口径见 --json 或 docs/cn-fiscal.md。")
	return err
}

func cnFiscalMonths(cmd *cobra.Command, now time.Time) (string, string, error) {
	begin, _ := cmd.Flags().GetString("begin")
	end, _ := cmd.Flags().GetString("end")
	if end == "" {
		now = now.In(time.FixedZone("CST", 8*3600))
		end = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).AddDate(0, -1, 0).Format("200601")
	}
	if begin == "" {
		date, err := time.Parse("200601", end)
		if err != nil {
			return "", "", fmt.Errorf("无效截至月份: %s", end)
		}
		begin = date.AddDate(0, -11, 0).Format("200601")
		if begin < "201501" {
			begin = "201501"
		}
	}
	return begin, end, nil
}
