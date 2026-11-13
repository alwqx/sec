package filings

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/alwqx/sec/cmd/internal/aicli"
	"github.com/alwqx/sec/config"
	"github.com/alwqx/sec/llm"
	"github.com/alwqx/sec/provider/edgar"
	"github.com/alwqx/sec/utils"
	"github.com/spf13/cobra"
)

// summarySelector 解析 --summary：列表序号（从 1 开始），或申报目录下的文件链接。
func summarySelector(value string) (int, string, error) {
	if index, err := strconv.Atoi(value); err == nil && index > 0 {
		return index, "", nil
	}
	if _, err := edgar.ParseFilingURL(value); err != nil {
		return 0, "", fmt.Errorf("--summary 需要从 1 开始的列表序号，或 SEC 申报文件链接")
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
	return &llm.FilingSummarizer{Client: setup.Client, Model: setup.Model, CacheDir: setup.CacheDir, Read: sec.Read8K}, setup.Config, nil
}

// summaryTarget 返回列表第 index 条申报的地址；目前只支持 8-K，其他类型不读原文直接报错。
func summaryTarget(filings []edgar.Filing, index int) (string, error) {
	if index > len(filings) {
		return "", fmt.Errorf("申报序号超出列表范围（共 %d 条），请先查看申报列表", len(filings))
	}
	f := filings[index-1]
	if base, _ := strings.CutSuffix(f.Form, "/A"); base != "8-K" {
		return "", fmt.Errorf("第 %d 条是 %s，目前只支持摘要 8-K", index, f.Form)
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
	fmt.Fprintf(out, "【AI 申报摘要】%s %s · 申报日 %s · 事项日 %s · 模型 %s · 配置 %s\n", result.Form, result.DocumentID, result.AsOf, result.ReportDate, result.Model, result.Profile)
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
