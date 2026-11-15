package filings

import (
	"fmt"
	"io"

	"github.com/alwqx/sec/provider/edgar"
	"github.com/alwqx/sec/utils"
	"github.com/spf13/cobra"
)

// risksTarget 返回列表第 index 条申报的地址；只有 10-K 和 10-KT（含修订版）有风险因素，其他类型不读原文直接报错。
func risksTarget(filings []edgar.Filing, index int) (string, error) {
	return rowTarget(filings, index, []string{"10-K", "10-KT"}, "风险因素对比只支持")
}

// printRisks 对比 url 所在的 10-K 和上一年 10-K 的风险因素，不调用模型。
func printRisks(cmd *cobra.Command, client *edgar.Client, url string, asJSON bool) error {
	changes, err := client.RiskChanges(cmd.Context(), url)
	if err != nil {
		return err
	}
	if asJSON {
		return utils.PrintJSON(cmd.OutOrStdout(), changes)
	}
	out := cmd.OutOrStdout()
	cur, prev := changes.Current, changes.Previous
	fmt.Fprintf(out, "【风险因素变化】%s %s（报告期 %s）对比上一年 %s %s（报告期 %s）\n", cur.Form, cur.Accession, cur.ReportDate, prev.Form, prev.Accession, prev.ReportDate)
	fmt.Fprintf(out, "上一年 %d 条，今年 %d 条：新增 %d 条，删除 %d 条，改写 %d 条，保留 %d 条。\n",
		prev.Count, cur.Count, len(changes.Added), len(changes.Removed), len(changes.Revised), len(changes.Kept))
	printRiskList(out, "新增", changes.Added)
	printRiskList(out, "删除", changes.Removed)
	fmt.Fprintf(out, "\n改写（%d）：\n", len(changes.Revised))
	if len(changes.Revised) == 0 {
		fmt.Fprintln(out, "  无")
	}
	for i, r := range changes.Revised {
		fmt.Fprintf(out, "  %d. %s新：%s\n", i+1, riskCategory(r.New), r.New.Title)
		fmt.Fprintf(out, "     旧：%s（小标题相似度 %.2f，正文 %.2f）\n", r.Old.Title, r.TitleSimilarity, r.TextSimilarity)
	}
	if len(changes.Kept) > 0 {
		fmt.Fprintf(out, "\n保留的 %d 条小标题相同，--json 列出全部。\n", len(changes.Kept))
	}
	fmt.Fprintf(out, "\n原文：%s\n上一年：%s\n", cur.URL, prev.URL)
	for _, note := range changes.Notes {
		fmt.Fprintln(out, note)
	}
	return nil
}

func printRiskList(out io.Writer, label string, risks []edgar.RiskFactor) {
	fmt.Fprintf(out, "\n%s（%d）：\n", label, len(risks))
	if len(risks) == 0 {
		fmt.Fprintln(out, "  无")
	}
	for i, r := range risks {
		fmt.Fprintf(out, "  %d. %s%s\n", i+1, riskCategory(r), r.Title)
	}
}

// riskCategory 返回「[类别] 」，没有类别时为空。
func riskCategory(r edgar.RiskFactor) string {
	if r.Category == "" {
		return ""
	}
	return "[" + r.Category + "] "
}
