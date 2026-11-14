package announcements

import (
	"context"
	"fmt"
	"strconv"

	"github.com/alwqx/sec/cmd/internal/aicli"
	"github.com/alwqx/sec/llm"
	"github.com/alwqx/sec/provider/cninfo"
	"github.com/alwqx/sec/utils"
	"github.com/spf13/cobra"
)

func summarySelector(value string) (int, string, error) {
	if index, err := strconv.Atoi(value); err == nil && index > 0 {
		return index, "", nil
	}
	doc, err := cninfo.DocumentURL(value)
	if err != nil {
		return 0, "", fmt.Errorf("--summary 需要从 1 开始的当前页公告序号，或巨潮 PDF 原文链接")
	}
	return 0, doc.URL, nil
}

// 先检查配置，再查公告或下载 PDF；测试时可替换为假客户端。
var openSummarizer = func(cmd *cobra.Command) (*llm.AnnouncementSummarizer, error) {
	setup, err := aicli.Open(cmd, "announcements")
	if err != nil {
		return nil, err
	}
	return &llm.AnnouncementSummarizer{Client: setup.Client, Model: setup.Model, CacheDir: setup.CacheDir}, nil
}

func printSummary(cmd *cobra.Command, summarizer *llm.AnnouncementSummarizer, url string, asJSON bool) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), aicli.SummaryTimeout)
	defer cancel()
	result, err := summarizer.Summarize(ctx, url)
	if err != nil {
		return aicli.Error("公告摘要", err)
	}
	if asJSON {
		return utils.PrintJSON(cmd.OutOrStdout(), result)
	}
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "【AI 公告摘要】文档 %s · 披露日期 %s · 模型 %s · 配置 %s\n", result.DocumentID, result.AsOf, result.Model, result.Profile)
	if result.Cached {
		fmt.Fprintf(out, "缓存生成于 %s\n", result.CreatedAt.Local().Format(utils.StandardTimeLayout))
	}
	fmt.Fprintf(out, "原文：%s\n\n%s\n\n", result.SourceURL, result.Text)
	aicli.PrintSummaryFooter(out, result.Check, result.Notes, result.Disclaimer)
	return nil
}
