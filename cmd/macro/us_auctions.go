package macro

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/alwqx/sec/provider/fiscaldata"
	"github.com/alwqx/sec/utils"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func newUSAuctionsCLI() *cobra.Command {
	cmd := &cobra.Command{Use: "auctions", Short: "美国国债逐券发行日期、拍卖结果与再发行", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		begin, _ := cmd.Flags().GetString("begin")
		end, _ := cmd.Flags().GetString("end")
		basis, _ := cmd.Flags().GetString("date-basis")
		if end == "" {
			end = time.Now().UTC().Format("20060102")
		}
		if begin == "" {
			d, e := time.Parse("20060102", end)
			if e != nil {
				return fmt.Errorf("无效截至日期：%s", end)
			}
			begin = d.AddDate(0, 0, -29).Format("20060102")
		}
		req := &fiscaldata.AuctionsRequest{Begin: begin, End: end, DateBasis: basis}
		if e := req.Validate(); e != nil {
			return e
		}
		disabled, _ := cmd.Flags().GetBool("no-cache")
		dir := ""
		var e error
		if !disabled {
			dir, e = utils.SecDir("cache", "fiscaldata")
			if e != nil {
				return e
			}
		}
		client := fiscaldata.NewClient(dir)
		client.Refresh, _ = cmd.Flags().GetBool("refresh")
		result, e := client.Auctions(cmd.Context(), req)
		if e != nil {
			return e
		}
		asJSON, _ := cmd.Flags().GetBool("json")
		if asJSON {
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(result)
		}
		return printUSAuctions(cmd.OutOrStdout(), result)
	}}
	addQueryFlags(cmd)
	cmd.Flags().String("date-basis", "issue", "日期筛选口径 issue（官方发行日，默认）或 auction（拍卖日）")
	cmd.Flags().Lookup("begin").Usage = "开始日期YYYYMMDD，最早19980727；默认截至日前29天"
	cmd.Flags().Lookup("end").Usage = "截至日期YYYYMMDD，默认UTC今天"
	return cmd
}
func printUSAuctions(out io.Writer, result *fiscaldata.AuctionsResult) error {
	basis := "官方发行日"
	if result.DateBasis == "auction_date" {
		basis = "拍卖日"
	}
	if _, e := fmt.Fprintf(out, "按%s筛选；金额单位：十亿美元面值，JSON保留美元原值。\n", basis); e != nil {
		return e
	}
	table := tablewriter.NewWriter(out)
	table.SetBorder(false)
	table.SetHeader([]string{"发行日", "拍卖日", "CUSIP", "类型", "期限", "再发行", "总中标", "SOMA"})
	amount := func(v *float64) string {
		if v == nil {
			return "—"
		}
		return strconv.FormatFloat(*v/1e9, 'f', 3, 64)
	}
	for _, r := range result.Observations {
		kind := map[string]string{"Bill": "国库券", "Note": "中期国债", "Bond": "长期国债"}[r.SecurityType]
		if r.FloatingRate != nil && *r.FloatingRate {
			kind = "浮息国债"
		}
		if r.InflationIndexed != nil && *r.InflationIndexed {
			kind = "通胀保值"
		}
		reopening := "—"
		if r.Reopening != nil {
			reopening = "否"
			if *r.Reopening {
				reopening = "是"
			}
		}
		table.Append([]string{r.IssueDate, r.AuctionDate, r.CUSIP, kind, r.Term, reopening, amount(r.Amounts.TotalAccepted), amount(r.Amounts.SOMA)})
	}
	table.Render()
	if len(result.Observations) == 0 {
		if _, e := fmt.Fprintln(out, "所选口径区间没有拍卖记录，不代表发行金额为零。"); e != nil {
			return e
		}
	}
	_, e := fmt.Fprintln(out, "来源：美国财政部Fiscal Data。SOMA是官方单列分项，可能已含于总中标，不相加；分配及纳入发行额标记见 --json。面值不是现金募集额或净融资，发行日不是单笔到账确认，按拍卖日查询可能包含尚未发行的结果。")
	return e
}
