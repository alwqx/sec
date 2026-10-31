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

func newUSHousingCLI() *cobra.Command    { return housingCommand(false) }
func newUSHousePriceCLI() *cobra.Command { return housingCommand(true) }

func housingCommand(price bool) *cobra.Command {
	use, short := "housing", "美国月度新房和成屋销售（分别为千套、套，季调折年）"
	if price {
		use, short = "house-price", "美国季度FHFA全交易房价指数（1980Q1=100，未季调）"
	}
	command := &cobra.Command{Use: use, Short: short, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		var begin, end string
		var err error
		component := ""
		if price {
			begin, end, err = housingQuarters(cmd, time.Now())
		} else {
			begin, end, err = queryUSMonths(cmd, time.Now(), "196301")
			component, _ = cmd.Flags().GetString("component")
		}
		if err != nil {
			return err
		}
		if price {
			err = (&fred.HousePriceRequest{Begin: begin, End: end}).Validate()
		} else {
			err = (&fred.HousingRequest{Begin: begin, End: end, Component: component}).Validate()
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
		var result *fred.HousingResult
		if price {
			result, err = client.HousePrice(cmd.Context(), &fred.HousePriceRequest{Begin: begin, End: end})
		} else {
			result, err = client.HousingSales(cmd.Context(), &fred.HousingRequest{Begin: begin, End: end, Component: component})
		}
		if err != nil {
			return err
		}
		standard, _ := cmd.Flags().GetBool("standard-json")
		if standard {
			dataset, err := macrodata.Housing(result)
			return writeDataset(cmd, dataset, err)
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(result)
		}
		return housingPrint(cmd.OutOrStdout(), result)
	}}
	addQueryFlags(command)
	addStandardJSONFlag(command, "按统一宏观序列格式输出JSON，分开新房、成屋和房价指数的单位与季调口径")
	command.Flags().Lookup("begin").Usage = "开始月份 YYYYMM，默认截至月份前11个月；最早196301，成屋仅当前公开快照可见范围"
	command.Flags().Lookup("end").Usage = "截至月份 YYYYMM，默认UTC上个月"
	if price {
		command.Flags().Lookup("begin").Usage = "开始季度 YYYYQ1至YYYYQ4，默认截至季度前7季；最早1975Q1"
		command.Flags().Lookup("end").Usage = "截至季度 YYYYQ1至YYYYQ4，默认UTC上一完整季度"
	} else {
		command.Flags().String("component", "all", "住宅销售分类：all、new-sales（新房）、existing-sales（成屋）")
	}
	return command
}

func housingQuarters(cmd *cobra.Command, now time.Time) (string, string, error) {
	begin, _ := cmd.Flags().GetString("begin")
	end, _ := cmd.Flags().GetString("end")
	if end == "" {
		now = now.UTC()
		quarter := now.Year()*4 + (int(now.Month())-1)/3 - 1
		end = fmt.Sprintf("%04dQ%d", quarter/4, quarter%4+1)
	}
	if err := (&fred.HousePriceRequest{Begin: end, End: end}).Validate(); err != nil {
		return "", "", err
	}
	if begin == "" {
		year, _ := strconv.Atoi(end[:4])
		quarter := year*4 + int(end[5]-'1') - 7
		begin = fmt.Sprintf("%04dQ%d", quarter/4, quarter%4+1)
		if begin < "1975Q1" {
			begin = "1975Q1"
		}
	}
	if err := (&fred.HousePriceRequest{Begin: begin, End: end}).Validate(); err != nil {
		return "", "", err
	}
	return begin, end, nil
}

func housingPrint(out io.Writer, result *fred.HousingResult) error {
	format := func(value *float64) string {
		if value == nil {
			return "—"
		}
		return strconv.FormatFloat(*value, 'f', 2, 64)
	}
	period, growth := "月份", "月环比（%）"
	if result.Frequency == "quarterly" {
		period, growth = "季度", "未季调季环比（%）"
	}
	table := tablewriter.NewWriter(out)
	table.SetBorder(false)
	table.SetHeader([]string{period, "指标", "水平", "单位及口径", "同比（%）", growth, "说明"})
	for _, item := range result.Observations {
		date := item.Period
		if result.Frequency == "monthly" {
			if parsed, err := time.Parse("200601", date); err == nil {
				date = parsed.Format("2006-01")
			}
		}
		name := map[string]string{"new-sales": "新建单户住宅销售", "existing-sales": "成屋销售", "house-price": "FHFA全交易房价指数"}[item.Component]
		unit := item.Unit + "（季调折年）"
		if result.Frequency == "quarterly" {
			unit = "1980Q1=100，未季调"
		}
		table.Append([]string{date, name, format(item.Value), unit, format(item.YoY), format(item.PreviousPeriodGrowth), item.Note})
	}
	table.Render()
	if _, err := fmt.Fprintln(out, "来源：各官方机构，经FRED转接。"+result.Note+" 空值为 —；JSON保留原始水平、严格基期、版权归属和各系列当前有效覆盖。"); err != nil {
		return err
	}
	for _, series := range result.Series {
		if _, err := fmt.Fprintf(out, "%s：%s；当前快照有效覆盖 %s 至 %s。\n", series.Name, series.Producer, series.FirstPeriod, series.LastPeriod); err != nil {
			return err
		}
		if series.Component == "existing-sales" {
			if _, err := fmt.Fprintln(out, "成屋版权归 National Association of Realtors；引用：NAR Existing Home Sales [EXHOSLUSM495S]，经FRED转接。"); err != nil {
				return err
			}
		}
	}
	return nil
}
