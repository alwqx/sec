// Package macro 提供按经济体组织的宏观数据查询。
package macro

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/alwqx/sec/provider/nbs"
	"github.com/alwqx/sec/utils"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

// NewMacroCLI 创建按经济体组织的宏观数据查询命令。
func NewMacroCLI() *cobra.Command {
	root := &cobra.Command{Use: "macro", Short: "查询宏观经济数据"}
	cn := &cobra.Command{Use: "cn", Short: "中国宏观经济数据"}
	for _, indicator := range []string{"cpi", "core-cpi", "ppi"} {
		name := indicator
		command := &cobra.Command{
			Use: name, Short: map[string]string{"cpi": "居民消费价格指数", "core-cpi": "核心居民消费价格指数（不包括食品和能源）", "ppi": "工业生产者出厂价格指数"}[name],
			Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error { return prices(cmd, name) },
		}
		addQueryFlags(command)
		cn.AddCommand(command)
	}
	cn.AddCommand(newPMICLI())
	cn.AddCommand(newNonManufacturingCLI())
	cn.AddCommand(newIndustrialCLI())
	cn.AddCommand(newRetailCLI())
	cn.AddCommand(newUnemploymentCLI())
	cn.AddCommand(newFinancingCLI())
	cn.AddCommand(newLoansCLI())
	cn.AddCommand(newMoneySupplyCLI())
	root.AddCommand(cn)
	return root
}

func prices(cmd *cobra.Command, indicator string) error {
	begin, end, err := queryMonths(cmd)
	if err != nil {
		return err
	}
	req := nbs.PriceRequest{Indicator: indicator, Begin: begin, End: end}
	if err := req.Validate(); err != nil {
		return err
	}
	client, err := queryClient(cmd)
	if err != nil {
		return err
	}
	items, err := client.Prices(cmd.Context(), req)
	if err != nil {
		return err
	}
	asJSON, _ := cmd.Flags().GetBool("json")
	if asJSON {
		if items == nil {
			items = []nbs.Observation{}
		}
		encoder := json.NewEncoder(cmd.OutOrStdout())
		encoder.SetIndent("", "  ")
		return encoder.Encode(items)
	}
	if len(items) == 0 {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "所选月份暂无数据")
		return err
	}
	table := tablewriter.NewWriter(cmd.OutOrStdout())
	table.SetHeader([]string{"月份", "指标", "比较口径", "原始指数", "涨跌幅（%）", "说明"})
	table.SetBorder(false)
	for _, item := range items {
		index, change := "—", "—"
		if item.Index != nil {
			index = strconv.FormatFloat(*item.Index, 'f', 1, 64)
		}
		if item.Change != nil {
			change = strconv.FormatFloat(*item.Change, 'f', 1, 64)
		}
		label := map[string]string{"cpi": "CPI", "core-cpi": "核心 CPI", "ppi": "PPI"}[indicator]
		table.Append([]string{item.Period, label, item.Basis, index, change, item.Note})
	}
	table.Render()
	_, err = fmt.Fprintln(cmd.OutOrStdout(), "来源：国家统计局；涨跌幅 = 原始指数 − 100；未季调。空值显示为 —，缺少的月份不补值。")
	return err
}

func addQueryFlags(command *cobra.Command) {
	command.Flags().StringP("begin", "b", "", "开始月份 YYYYMM，默认截至月份前 11 个月")
	command.Flags().StringP("end", "e", "", "截至月份 YYYYMM，默认上个月")
	command.Flags().Bool("json", false, "输出含指标数值和来源的 JSON")
	command.Flags().Bool("refresh", false, "重新采集并更新缓存")
	command.Flags().Bool("no-cache", false, "不读写磁盘缓存")
	command.Flags().BoolP("debug", "D", false, "显示调试日志")
}

func queryMonths(cmd *cobra.Command) (string, string, error) {
	begin, _ := cmd.Flags().GetString("begin")
	end, _ := cmd.Flags().GetString("end")
	if end == "" {
		end = time.Now().AddDate(0, 0, -time.Now().Day()).Format("200601")
	}
	if begin == "" {
		date, err := time.Parse("200601", end)
		if err != nil {
			return "", "", fmt.Errorf("无效截至月份: %s", end)
		}
		begin = date.AddDate(0, -11, 0).Format("200601")
	}
	return begin, end, nil
}

func queryClient(cmd *cobra.Command) (*nbs.Client, error) {
	noCache, _ := cmd.Flags().GetBool("no-cache")
	cacheDir := ""
	if !noCache {
		var err error
		cacheDir, err = utils.SecDir("cache", "nbs")
		if err != nil {
			return nil, err
		}
	}
	client := nbs.NewClient(cacheDir)
	client.Refresh, _ = cmd.Flags().GetBool("refresh")
	return client, nil
}
