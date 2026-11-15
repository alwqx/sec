package filings

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/alwqx/sec/cmd/internal/aicli"
	"github.com/alwqx/sec/config"
	"github.com/alwqx/sec/llm"
	"github.com/alwqx/sec/provider/edgar"
	"github.com/alwqx/sec/utils"
	"github.com/spf13/cobra"
)

// summarySelector 解析 --summary 或 --risks（name）：列表序号（从 1 开始），或申报目录下的文件链接。
func summarySelector(name, value string) (int, string, error) {
	if index, err := strconv.Atoi(value); err == nil && index > 0 {
		return index, "", nil
	}
	if _, err := edgar.ParseFilingURL(value); err != nil {
		return 0, "", fmt.Errorf("%s 需要从 1 开始的列表序号，或 SEC 申报文件链接", name)
	}
	return 0, value, nil
}

// openSummarizer 按 --ai 创建申报摘要用的客户端，同时返回读到的配置，查询申报列表复用。
// 在请求 EDGAR 之前调用，配置有问题时不发请求。测试时替换。
var openSummarizer = func(cmd *cobra.Command) (*llm.FilingSummarizer, *config.Config, error) {
	setup, err := aicli.Open(cmd, "filings")
	if err != nil {
		return nil, nil, err
	}
	sec := &edgar.Client{Email: setup.Config.SECEmail}
	return &llm.FilingSummarizer{Client: setup.Client, Model: setup.Model, CacheDir: setup.CacheDir, Read: sec.ReadFiling}, setup.Config, nil
}

// summaryTarget 返回列表第 index 条申报的地址；只支持 edgar.SummarizableForms（含修订版），其他类型不读原文直接报错。
func summaryTarget(filings []edgar.Filing, index int) (string, error) {
	return rowTarget(filings, index, edgar.SummarizableForms, "目前只支持摘要")
}

// rowTarget 返回列表第 index 条申报的地址。forms 是支持的基本类型（含修订版），其他类型报「第 n 条是 X，<what> <forms>」。
func rowTarget(filings []edgar.Filing, index int, forms []string, what string) (string, error) {
	if index > len(filings) {
		return "", fmt.Errorf("申报序号超出列表范围（共 %d 条），请先查看申报列表", len(filings))
	}
	f := filings[index-1]
	if !slices.Contains(forms, edgar.BaseForm(f.Form)) {
		return "", fmt.Errorf("第 %d 条是 %s，%s %s", index, f.Form, what, strings.Join(forms, "、"))
	}
	return f.URL, nil
}

func printSummary(cmd *cobra.Command, summarizer *llm.FilingSummarizer, url string, asJSON bool) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), aicli.SummaryTimeout)
	defer cancel()
	result, err := summarizer.Summarize(ctx, url)
	if err != nil {
		return aicli.Error("申报摘要", err)
	}
	if asJSON {
		return utils.PrintJSON(cmd.OutOrStdout(), result)
	}
	out := cmd.OutOrStdout()
	// 8-K 的报告期是最早事项的发生日，10-K 是财年截止日。
	reportDate := "事项日"
	if edgar.BaseForm(result.Form) != "8-K" {
		reportDate = "报告期"
	}
	fmt.Fprintf(out, "【AI 申报摘要】%s %s · 申报日 %s · %s %s · 模型 %s · 配置 %s\n", result.Form, result.DocumentID, result.AsOf, reportDate, result.ReportDate, result.Model, result.Profile)
	if result.Cached {
		fmt.Fprintf(out, "缓存生成于 %s\n", result.CreatedAt.Local().Format(utils.StandardTimeLayout))
	}
	fmt.Fprintf(out, "原文：%s\n\n%s\n\n", result.IndexURL, result.Text)
	for _, source := range result.Sources {
		fmt.Fprintf(out, "%s：%s\n", source.Label, source.URL)
	}
	aicli.PrintSummaryFooter(out, result.Check, result.Notes, result.Disclaimer)
	return nil
}
