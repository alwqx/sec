package macro

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	"github.com/alwqx/sec/provider/nbs"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func newPMICLI() *cobra.Command {
	return newPMIQueryCLI("pmi", "制造业采购经理指数及生产、订单、出口订单、就业分项", (*nbs.Client).ManufacturingPMI, func(cmd *cobra.Command, items []nbs.PMIObservation) error {
		return printPMI(cmd.OutOrStdout(), items)
	})
}

func newPMIQueryCLI(use, short string, query func(*nbs.Client, context.Context, nbs.PMIRequest) ([]nbs.PMIObservation, error), render func(*cobra.Command, []nbs.PMIObservation) error) *cobra.Command {
	command := &cobra.Command{Use: use, Short: short, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		begin, end, err := queryMonths(cmd)
		if err != nil {
			return err
		}
		req := nbs.PMIRequest{Begin: begin, End: end}
		if err := req.Validate(); err != nil {
			return err
		}
		client, err := queryClient(cmd)
		if err != nil {
			return err
		}
		items, err := query(client, cmd.Context(), req)
		if err != nil {
			return err
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			if items == nil {
				items = []nbs.PMIObservation{}
			}
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(items)
		}
		return render(cmd, items)
	}}
	addQueryFlags(command)
	return command
}

func printPMI(out io.Writer, items []nbs.PMIObservation) error {
	if len(items) == 0 {
		_, err := fmt.Fprintln(out, "所选月份暂无制造业 PMI 数据")
		return err
	}
	keys := []string{"manufacturing-pmi", "production", "new-orders", "new-export-orders", "employment"}
	printPMITable(out, items, keys, []string{"月份", "制造业 PMI", "生产", "新订单", "新出口订单", "从业人员"})
	_, err := fmt.Fprintln(out, "来源：国家统计局；季调扩散指数，单位 %，50 为临界点；指数水平不是产出增速，不减 100。空值显示为 —，缺少的月份不补值。")
	return err
}

func printPMITable(out io.Writer, items []nbs.PMIObservation, keys, headers []string) {
	rows := map[string]map[string]*float64{}
	var months []string
	for _, item := range items {
		if rows[item.Period] == nil {
			months = append(months, item.Period)
			rows[item.Period] = map[string]*float64{}
		}
		rows[item.Period][item.Indicator] = item.Index
	}
	table := tablewriter.NewWriter(out)
	table.SetBorder(false)
	table.SetHeader(headers)
	for _, month := range months {
		row := []string{month}
		for _, key := range keys {
			value := "—"
			if n := rows[month][key]; n != nil {
				value = strconv.FormatFloat(*n, 'f', 1, 64)
			}
			row = append(row, value)
		}
		table.Append(row)
	}
	table.Render()
}
