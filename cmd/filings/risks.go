package filings

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/alwqx/sec/cmd/internal/aicli"
	"github.com/alwqx/sec/config"
	"github.com/alwqx/sec/llm"
	"github.com/alwqx/sec/provider/edgar"
	"github.com/alwqx/sec/utils"
	"github.com/spf13/cobra"
)

// risksTarget 返回列表第 index 条申报的地址；只有 10-K 和 10-KT（含修订版）有风险因素，其他类型不读原文直接报错。
func risksTarget(filings []edgar.Filing, index int) (string, error) {
	return rowTarget(filings, index, []string{"10-K", "10-KT"}, "风险因素对比只支持")
}

// openRiskExplainer 按 --ai 创建解读风险因素变化的客户端，同时返回读到的配置。在请求 EDGAR 之前调用，测试时替换。
// Read 为空，由 filingsHandler 设成列表用的 EDGAR 客户端。
var openRiskExplainer = func(cmd *cobra.Command) (*llm.RiskExplainer, *config.Config, error) {
	setup, err := aicli.Open(cmd, "risks")
	if err != nil {
		return nil, nil, err
	}
	return &llm.RiskExplainer{Client: setup.Client, Model: setup.Model, CacheDir: setup.CacheDir}, setup.Config, nil
}

// printRisks 对比 url 所在的 10-K 和上一年 10-K 的风险因素；explainer 不为空时再用模型解读变化。
func printRisks(cmd *cobra.Command, client *edgar.Client, explainer *llm.RiskExplainer, url string, asJSON bool) error {
	if explainer != nil {
		return printRiskExplanation(cmd, explainer, url, asJSON)
	}
	changes, err := client.RiskChanges(cmd.Context(), url)
	if err != nil {
		return err
	}
	if asJSON {
		return utils.PrintJSON(cmd.OutOrStdout(), changes)
	}
	printRiskChanges(cmd.OutOrStdout(), changes)
	for _, note := range changes.Notes {
		fmt.Fprintln(cmd.OutOrStdout(), note)
	}
	return nil
}

// printRiskExplanation 先列出确定性的对比，再输出解读：解读的引用（如 新增 1）对应列表里的序号。
func printRiskExplanation(cmd *cobra.Command, explainer *llm.RiskExplainer, url string, asJSON bool) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), aicli.SummaryTimeout)
	defer cancel()
	result, err := explainer.Explain(ctx, url)
	// 申报内容的问题（没有上一年的 10-K、识别不出小标题）和不加 --explain 时一样直接报错，不说成解读失败。
	if errors.Is(err, edgar.ErrDocument) {
		return err
	}
	if err != nil {
		return aicli.Error("风险因素变化解读", err)
	}
	if asJSON {
		return utils.PrintJSON(cmd.OutOrStdout(), result)
	}
	out := cmd.OutOrStdout()
	printRiskChanges(out, result.Changes)
	if len(result.Sections) > 0 {
		fmt.Fprintf(out, "\n【AI 解读】模型 %s · 配置 %s\n", result.Model, result.Profile)
		if result.Cached {
			fmt.Fprintf(out, "缓存生成于 %s\n", result.CreatedAt.Local().Format(utils.StandardTimeLayout))
		}
		fmt.Fprintf(out, "\n%s\n\n", result.Text)
	}
	aicli.PrintSummaryFooter(out, result.Check, append(slices.Clone(result.Changes.Notes), result.Notes...), result.Disclaimer)
	return nil
}

// printRiskChanges 输出确定性的对比：新增、删除和改写逐条列出，带序号，保留的只给条数。
func printRiskChanges(out io.Writer, changes *edgar.RiskChanges) {
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
