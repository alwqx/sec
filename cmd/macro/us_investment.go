package macro

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/alwqx/sec/provider/fred"
	"github.com/alwqx/sec/provider/macrodata"
	"github.com/alwqx/sec/utils"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func newUSInvestmentCLI() *cobra.Command          { return investmentCommand(false) }
func newUSEquipmentInvestmentCLI() *cobra.Command { return investmentCommand(true) }

func investmentCommand(quarterly bool) *cobra.Command {
	use, short := "investment", "美国住宅开工和建筑许可住房套数（月度、季调折年）"
	if quarterly {
		use, short = "equipment-investment", "美国私人非住宅设备投资（季度、季调折年，默认实际口径）"
	}
	command := &cobra.Command{Use: use, Short: short, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		var begin, end, component, basis string
		var err error
		if quarterly {
			basis, _ = cmd.Flags().GetString("price-basis")
			begin, end, err = investmentQueryQuarters(cmd, time.Now(), basis)
		} else {
			component, _ = cmd.Flags().GetString("component")
			earliest := "195901"
			if component == "building-permits" {
				earliest = "196001"
			}
			begin, end, err = queryUSMonths(cmd, time.Now(), earliest)
		}
		if err != nil {
			return err
		}
		if quarterly {
			err = (&fred.EquipmentInvestmentRequest{Begin: begin, End: end, PriceBasis: basis}).Validate()
		} else {
			err = (&fred.HousingInvestmentRequest{Begin: begin, End: end, Component: component}).Validate()
		}
		if err != nil {
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
		var result *fred.InvestmentResult
		if quarterly {
			result, err = client.EquipmentInvestment(cmd.Context(), &fred.EquipmentInvestmentRequest{Begin: begin, End: end, PriceBasis: basis})
		} else {
			result, err = client.HousingInvestment(cmd.Context(), &fred.HousingInvestmentRequest{Begin: begin, End: end, Component: component})
		}
		if err != nil {
			return err
		}
		standard, _ := cmd.Flags().GetBool("standard-json")
		if standard {
			dataset, err := macrodata.Investment(result)
			return writeDataset(cmd, dataset, err)
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(result)
		}
		return investmentPrint(cmd.OutOrStdout(), result)
	}}
	addQueryFlags(command)
	addStandardJSONFlag(command, "按统一宏观序列格式输出JSON，区分住宅建设套数与名义／实际设备投资")
	if quarterly {
		command.Flags().String("price-basis", "real", "价格口径：real（实际，2017年链式美元）或nominal（名义，当期美元）")
		command.Flags().Lookup("begin").Usage = "开始季度 YYYYQ1至YYYYQ4，默认截至季度前7季；实际最早2007Q1，名义最早1947Q1"
		command.Flags().Lookup("end").Usage = "截至季度 YYYYQ1至YYYYQ4，默认UTC上一完整季度"
	} else {
		command.Flags().String("component", "housing-starts", "分类：housing-starts（住宅开工）或building-permits（建筑许可住房套数）")
		command.Flags().Lookup("begin").Usage = "开始月份 YYYYMM，默认截至月份前11个月；开工最早195901，许可最早196001"
		command.Flags().Lookup("end").Usage = "截至月份 YYYYMM，默认UTC上个月"
	}
	return command
}

func investmentQueryQuarters(cmd *cobra.Command, now time.Time, basis string) (string, string, error) {
	begin, _ := cmd.Flags().GetString("begin")
	end, _ := cmd.Flags().GetString("end")
	if end == "" {
		now = now.UTC()
		q := now.Year()*4 + (int(now.Month())-1)/3 - 1
		end = fmt.Sprintf("%04dQ%d", q/4, q%4+1)
	}
	if err := (&fred.EquipmentInvestmentRequest{Begin: end, End: end, PriceBasis: basis}).Validate(); err != nil {
		return "", "", err
	}
	if begin == "" {
		earliest := "2007Q1"
		if basis == "nominal" {
			earliest = "1947Q1"
		}
		year, _ := strconv.Atoi(end[:4])
		q := year*4 + int(end[5]-'1') - 7
		begin = fmt.Sprintf("%04dQ%d", q/4, q%4+1)
		if begin < earliest {
			begin = earliest
		}
	}
	if err := (&fred.EquipmentInvestmentRequest{Begin: begin, End: end, PriceBasis: basis}).Validate(); err != nil {
		return "", "", err
	}
	return begin, end, nil
}

func investmentPrint(out io.Writer, result *fred.InvestmentResult) error {
	label := map[string]string{"housing-starts": "住宅开工", "building-permits": "建筑许可住房套数", "equipment": "私人非住宅设备投资"}[result.Component]
	if _, err := fmt.Fprintf(out, "美国%s（%s）：单位%s，覆盖%s至%s。\n", label, result.SeriesID, result.Unit, result.FirstPeriod, result.LastPeriod); err != nil {
		return err
	}
	period, growth := "月份", "月环比（%）"
	if result.Frequency == "quarterly" {
		period, growth = "季度", "季环比（%）"
	}
	format := func(value *float64, digits int) string {
		if value == nil {
			return "—"
		}
		return strconv.FormatFloat(*value, 'f', digits, 64)
	}
	digits := 0
	if result.Component == "equipment" {
		digits = 3
	}
	table := tablewriter.NewWriter(out)
	table.SetBorder(false)
	table.SetHeader([]string{period, "季调折年水平", "同比（%）", growth, "说明"})
	for _, item := range result.Observations {
		date := item.Period
		if result.Frequency == "monthly" {
			if parsed, err := time.Parse("200601", date); err == nil {
				date = parsed.Format("2006-01")
			}
		}
		table.Append([]string{date, format(item.Level, digits), format(item.YoY, 2), format(item.PreviousGrowth, 2), item.Note})
	}
	table.Render()
	if _, err := fmt.Fprintln(out, "来源："+result.Producer+"，经FRED转接。"+result.Note+" 空值为 —；JSON保留原值、严格基期与溯源信息。"); err != nil {
		return err
	}
	for _, note := range result.MethodologyNotes {
		if _, err := fmt.Fprintln(out, note); err != nil {
			return err
		}
	}
	return nil
}
