package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/alwqx/sec/fund"
	"github.com/alwqx/sec/provider/eastmoney"
	"github.com/alwqx/sec/utils"
	"github.com/spf13/cobra"
)

// fundInfoHandler 打印基金概况。--json 格式是 fund.Info，和 A 股、港股、美股都不同。
// 一个数据源失败时照常输出另一个的内容，并在 stderr 说明。基金没有分红送配表，-d 只提示。
func fundInfoHandler(cmd *cobra.Command, exCode string, asJSON, dividend bool) error {
	info, err := fund.Load(cmd.Context(), exCode)
	if err != nil {
		return err
	}
	if dividend {
		fmt.Fprintln(cmd.ErrOrStderr(), "基金暂不支持 -d，分红记录见基金档案的「成立来分红」")
	}
	for _, note := range info.Notes {
		fmt.Fprintln(cmd.ErrOrStderr(), note)
	}
	if asJSON {
		return utils.PrintJSON(cmd.OutOrStdout(), info)
	}
	printFundInfo(cmd.OutOrStdout(), info)
	return nil
}

func printFundInfo(out io.Writer, info *fund.Info) {
	rows := [][2]string{
		{"基金代码", info.ExCode}, {"基金简称", info.Name}, {"基金全称", info.FullName}, {"基金类型", info.Type},
		{"场内交易", yesNo(info.Listed)}, {"风险等级", info.RiskLevel},
		{"基金管理人", info.Company}, {"基金托管人", info.Custodian}, {"基金经理", info.Managers},
		{"成立日期", info.LaunchDate}, {"成立规模", info.LaunchSize}, {"净资产", netAssetsText(info)},
	}
	if info.MoneyMarket {
		rows = append(rows, [2]string{"万份收益", ratioText(info.Yield10K, "%.4f 元")}, [2]string{"七日年化", ratioText(info.Yield7D, "%.3f%%")})
	} else {
		rows = append(rows, [2]string{"单位净值", ratioText(info.NAV, "%.4f")}, [2]string{"累计净值", ratioText(info.AccNAV, "%.4f")},
			[2]string{"日涨幅", ratioText(info.DayChange, "%+.2f%%")})
	}
	rows = append(rows, [2]string{"净值日期", info.NAVDate})
	if info.Returns != nil {
		rows = append(rows, [2]string{"阶段收益", returnsText(info.Returns)})
	}
	rows = append(rows, [2]string{"管理费率", info.ManagementFee}, [2]string{"托管费率", info.CustodyFee},
		[2]string{"业绩基准", info.Benchmark}, [2]string{"跟踪标的", info.Tracking}, [2]string{"投资目标", info.Objective})
	for _, row := range rows {
		value := row[1]
		if value == "" {
			value = "-"
		}
		fmt.Fprintf(out, "%s\t%s\n", row[0], value)
	}
}

// netAssetsText 优先用网页上带截止日期的净资产规模，没有时用 App 接口的净资产。
func netAssetsText(info *fund.Info) string {
	if info.NetAssetsNote != "" {
		return info.NetAssetsNote
	}
	if info.NetAssets == nil {
		return ""
	}
	return utils.HumanNum(*info.NetAssets) + "元"
}

func ratioText(value *float64, format string) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf(format, *value)
}

// returnsText 把阶段收益率写成一行，没有的阶段不显示。
func returnsText(r *eastmoney.FundReturns) string {
	parts := []string{}
	for _, item := range []struct {
		label string
		value *float64
	}{{"近1周", r.Week}, {"近1月", r.Month}, {"近3月", r.Quarter}, {"近6月", r.HalfYear}, {"近1年", r.Year},
		{"近2年", r.TwoYears}, {"近3年", r.ThreeYears}, {"今年来", r.YearToDate}, {"成立来", r.SinceLaunch}} {
		if item.value != nil {
			parts = append(parts, fmt.Sprintf("%s %+.2f%%", item.label, *item.value))
		}
	}
	return strings.Join(parts, "  ")
}

func yesNo(value bool) string {
	if value {
		return "是"
	}
	return "否"
}
