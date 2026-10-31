package macro

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/alwqx/sec/provider/bls"
	"github.com/alwqx/sec/provider/macrodata"
	"github.com/alwqx/sec/utils"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func newUSLaborForceCLI(indicator string) *cobra.Command {
	short := "美国总体失业率（CPS、季调）"
	if indicator == "participation" {
		short = "美国劳动参与率（CPS、季调）"
	}
	command := &cobra.Command{Use: indicator, Short: short, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		begin, end, err := queryUSMonths(cmd, time.Now(), "194801")
		if err != nil {
			return err
		}
		req := &bls.LaborForceRequest{Indicator: indicator, Begin: begin, End: end}
		if err := req.Validate(); err != nil {
			return err
		}
		cacheDir := ""
		noCache, _ := cmd.Flags().GetBool("no-cache")
		if !noCache {
			cacheDir, err = utils.SecDir("cache", "bls")
			if err != nil {
				return err
			}
		}
		client := bls.NewClient(cacheDir)
		client.Refresh, _ = cmd.Flags().GetBool("refresh")
		result, err := client.LaborForce(cmd.Context(), req)
		if err != nil {
			return err
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		standard, _ := cmd.Flags().GetBool("standard-json")
		if standard {
			dataset, err := macrodata.LaborForce(result)
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
		return printUSLaborForce(cmd.OutOrStdout(), result)
	}}
	addQueryFlags(command)
	command.Flags().Bool("standard-json", false, "按统一宏观序列格式输出JSON，保留调查总体、比率分母和百分点变化")
	command.MarkFlagsMutuallyExclusive("json", "standard-json")
	command.Flags().Lookup("end").Usage = "截至月份 YYYYMM，默认UTC上个月"
	return command
}

func printUSLaborForce(out io.Writer, result *bls.LaborForceResult) error {
	label := "失业率"
	if result.Indicator == "participation" {
		label = "劳动参与率"
	}
	table := tablewriter.NewWriter(out)
	table.SetBorder(false)
	table.SetHeader([]string{"月份", label + "（%）", "较上月（百分点）", "较上年同月（百分点）", "来源脚注", "说明"})
	format := func(value *float64) string {
		if value == nil {
			return "—"
		}
		return strconv.FormatFloat(*value, 'f', 1, 64)
	}
	codes := func(value bls.LaborForceValue) string {
		notes := []string{}
		for _, note := range value.Footnotes {
			if note.Code != "" {
				notes = append(notes, note.Code)
			} else if note.Text != "" {
				notes = append(notes, "文字脚注")
			}
		}
		return strings.Join(notes, ",")
	}
	for _, item := range result.Observations {
		date, _ := time.Parse("200601", item.Period)
		footnotes := []string{}
		for _, point := range []struct {
			label string
			value bls.LaborForceValue
		}{{"当月", item.LaborForceValue}, {"上月", item.MoMBase}, {"上年同月", item.YoYBase}} {
			if text := codes(point.value); text != "" {
				footnotes = append(footnotes, point.label+":"+text)
			}
		}
		table.Append([]string{date.Format("2006-01"), format(item.Rate), format(item.MoMChange), format(item.YoYChange), strings.Join(footnotes, "；"), item.Note})
	}
	table.Render()
	_, err := fmt.Fprintln(out, "来源：美国劳工统计局（BLS）CPS住户调查，16岁及以上非机构平民人口，季调。失业率的分母为平民劳动力，劳动参与率的分母为非机构平民人口。变化单位为百分点，按同一响应内的严格基期相减，缺口不补。人口控制更新可能影响跨期可比性，季调历史可能修订；来源脚注原文及基期见JSON。历史为当前可见版本，未提供官方历次发布快照。")
	return err
}
