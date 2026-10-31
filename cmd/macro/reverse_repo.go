package macro

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/alwqx/sec/provider/macrodata"
	"github.com/alwqx/sec/provider/pbc"
	"github.com/alwqx/sec/utils"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func newReverseRepoCLI() *cobra.Command {
	command := &cobra.Command{Use: "reverse-repo", Short: "人民银行七天逆回购政策利率调整公告（自20240722起）", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		begin, _ := cmd.Flags().GetString("begin")
		end, _ := cmd.Flags().GetString("end")
		if begin == "" {
			begin = pbc.ReverseRepoCoverageStart
		}
		if end == "" {
			end = time.Now().In(time.FixedZone("CST", 8*60*60)).Format("20060102")
		}
		req := pbc.ReverseRepoRequest{Begin: begin, End: end}
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
		result, err := client.ReverseRepo(cmd.Context(), req)
		if err != nil {
			return err
		}
		standard, _ := cmd.Flags().GetBool("standard-json")
		if standard {
			dataset, err := macrodata.ReverseRepo(&result)
			return writeDataset(cmd, dataset, err)
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(result)
		}
		return printReverseRepo(cmd.OutOrStdout(), result)
	}}
	addQueryFlags(command)
	addStandardJSONFlag(command, "按统一宏观序列格式输出JSON，只输出窗口内调整公告，生效日与公布时间分开")
	command.Flags().Lookup("begin").Usage = "开始生效日期 YYYYMMDD，默认20240722"
	command.Flags().Lookup("end").Usage = "截至生效日期 YYYYMMDD，默认北京时间今天"
	return command
}

func printReverseRepo(out io.Writer, result pbc.ReverseRepoResult) error {
	format := func(n float64) string { return strconv.FormatFloat(n, 'f', -1, 64) }
	if previous := result.PreviousEvent; previous != nil {
		if _, err := fmt.Fprintf(out, "区间前最近公告：%s 生效，利率 %s%%（未列入区间内调整）\n", previous.EffectiveDate, format(previous.Rate)); err != nil {
			return err
		}
	}
	if len(result.Events) == 0 {
		if _, err := fmt.Fprintln(out, "所选生效日期区间内未检索到七天逆回购政策利率调整公告"); err != nil {
			return err
		}
	} else {
		table := tablewriter.NewWriter(out)
		table.SetBorder(false)
		table.SetHeader([]string{"生效日期", "公布时间（北京时间）", "调整前（%）", "调整后（%）", "变化（基点，计算值）"})
		for _, event := range result.Events {
			table.Append([]string{event.EffectiveDate, event.PublishedAt, format(event.PreviousRate), format(event.Rate), format(event.ChangeBasisPoints)})
		}
		table.Render()
	}
	_, err := fmt.Fprintln(out, "来源：人民银行公开市场业务公告；生效日和前后利率直接取自公告。覆盖自20240722起的固定利率机制，网页公布时间可能受修订影响；无调整记录不代表利率为零。")
	return err
}
