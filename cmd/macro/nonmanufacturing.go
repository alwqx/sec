package macro

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/alwqx/sec/provider/nbs"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func newNonManufacturingCLI() *cobra.Command {
	command := newPMIQueryCLI("nonmanufacturing-pmi", "非制造业商务活动指数、服务业、建筑业及相关分项", (*nbs.Client).NonManufacturingPMI, func(cmd *cobra.Command, items []nbs.PMIObservation) error {
		details, _ := cmd.Flags().GetBool("details")
		return printNonManufacturing(cmd.OutOrStdout(), items, details)
	})
	command.Aliases = []string{"npmi"}
	command.Flags().Bool("details", false, "展示订单、价格、就业等九个全国分项")
	return command
}

func printNonManufacturing(out io.Writer, items []nbs.PMIObservation, details bool) error {
	if len(items) == 0 {
		_, err := fmt.Fprintln(out, "所选月份暂无非制造业商务活动指数数据")
		return err
	}
	keys := []string{"business-activity", "services-business-activity", "construction-business-activity"}
	printPMITable(out, items, keys, []string{"月份", "非制造业商务活动", "服务业商务活动", "建筑业商务活动"})
	if details {
		table := tablewriter.NewWriter(out)
		table.SetBorder(false)
		table.SetHeader([]string{"月份", "全国分项", "指数（%）"})
		for _, item := range items {
			if item.Indicator == keys[0] || item.Indicator == keys[1] || item.Indicator == keys[2] {
				continue
			}
			value := "—"
			if item.Index != nil {
				value = strconv.FormatFloat(*item.Index, 'f', 1, 64)
			}
			label := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(item.SeriesName), "(%)"))
			table.Append([]string{item.Period, label, value})
		}
		table.Render()
	}
	_, err := fmt.Fprintln(out, "来源：国家统计局；季调扩散指数，单位 %，50 为临界点；指数水平不是产出增速。配送时间指数高于 50 表示交货加快。空值显示为 —，缺少的月份不补值。")
	return err
}
