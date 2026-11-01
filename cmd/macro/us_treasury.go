package macro

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/alwqx/sec/provider/bond"
	"github.com/alwqx/sec/provider/macrodata"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

// usTreasuryCoverageStart 是财政部每日收益率曲线的最早记录日。
const usTreasuryCoverageStart = "19900102"

func newUSTreasuryCLI() *cobra.Command {
	command := &cobra.Command{Use: "treasury", Short: "美国国债平价收益率曲线（1个月至30年期）", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		begin, end, err := usTreasuryDates(cmd, time.Now())
		if err != nil {
			return err
		}
		start, _ := time.Parse("20060102", begin)
		finish, _ := time.Parse("20060102", end)
		result, err := bond.QueryBond(cmd.Context(), &bond.QueryBondReq{Start: start.Format("2006-01-02"), End: finish.Format("2006-01-02"), Strict: true})
		if err != nil {
			return err
		}
		standard, _ := cmd.Flags().GetBool("standard-json")
		if standard {
			dataset, err := macrodata.USTreasury(result, time.Now(), begin, end)
			return writeDataset(cmd, dataset, err)
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(bond.CurveDays(result.Data))
		}
		return printUSTreasury(cmd.OutOrStdout(), result)
	}}
	command.Flags().StringP("begin", "b", "", "开始日期 YYYYMMDD，默认截至日前29天，最早19900102")
	command.Flags().StringP("end", "e", "", "截至日期 YYYYMMDD，默认UTC今天")
	command.Flags().Bool("json", false, "输出各期限收益率 JSON，未公布的期限为 null")
	addStandardJSONFlag(command, "按统一宏观序列格式输出JSON，每个期限各成一条序列，缺失期限不补值")
	command.Flags().BoolP("debug", "D", false, "显示调试日志")
	return command
}

func usTreasuryDates(cmd *cobra.Command, now time.Time) (string, string, error) {
	begin, _ := cmd.Flags().GetString("begin")
	end, _ := cmd.Flags().GetString("end")
	today := now.UTC().Format("20060102")
	if end == "" {
		end = today
	}
	date, err := time.Parse("20060102", end)
	if err != nil || date.Format("20060102") != end {
		return "", "", fmt.Errorf("无效截至日期 %q，请使用 YYYYMMDD", end)
	}
	if end < usTreasuryCoverageStart {
		return "", "", fmt.Errorf("美国国债收益率曲线查询支持自%s起", usTreasuryCoverageStart)
	}
	if begin == "" {
		begin = date.AddDate(0, 0, -29).Format("20060102")
		if begin < usTreasuryCoverageStart {
			begin = usTreasuryCoverageStart
		}
	}
	if parsed, err := time.Parse("20060102", begin); err != nil || parsed.Format("20060102") != begin {
		return "", "", fmt.Errorf("无效开始日期 %q，请使用 YYYYMMDD", begin)
	}
	if begin > end {
		return "", "", fmt.Errorf("开始日期不能晚于截至日期")
	}
	if begin < usTreasuryCoverageStart {
		return "", "", fmt.Errorf("美国国债收益率曲线查询支持自%s起", usTreasuryCoverageStart)
	}
	if end > today {
		return "", "", fmt.Errorf("截至日期不能晚于UTC今天")
	}
	return begin, end, nil
}

func printUSTreasury(out io.Writer, result *bond.QueryBondResp) error {
	if len(result.Data) == 0 {
		_, err := fmt.Fprintln(out, "所选日期区间未返回美国国债收益率数据，周末及节假日不补值")
		return err
	}
	table := tablewriter.NewWriter(out)
	table.SetBorder(false)
	table.SetHeader([]string{"日期", "1个月", "3个月", "6个月", "1年", "2年", "3年", "5年", "7年", "10年", "20年", "30年"})
	for _, item := range result.Data {
		row := []string{item.Date}
		for _, tenor := range []struct {
			id    string
			value float64
		}{{"1m", item.BC1Month}, {"3m", item.BC3Month}, {"6m", item.BC6Month}, {"1y", item.BC1Year}, {"2y", item.BC2Year}, {"3y", item.BC3Year}, {"5y", item.BC5Year}, {"7y", item.BC7Year}, {"10y", item.BC10Year}, {"20y", item.BC20Year}, {"30y", item.BC30Year}} {
			cell := "—"
			if !item.MissingTenors[tenor.id] {
				cell = strconv.FormatFloat(tenor.value, 'f', 2, 64)
			}
			row = append(row, cell)
		}
		table.Append(row)
	}
	table.Render()
	_, err := fmt.Fprintln(out, "来源：美国财政部每日国债平价收益率曲线；单位：%。空值为 —，不补值；周末、节假日无记录。")
	return err
}
