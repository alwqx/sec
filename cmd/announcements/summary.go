package announcements

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/alwqx/sec/config"
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
	cfg, err := config.Load(cmd.Context())
	switch {
	case errors.Is(err, config.ErrInvalidAI):
		return nil, fmt.Errorf("%w；请先手动修正 ai 段", err)
	case errors.Is(err, config.ErrInvalid) && len(cfg.AI.Profiles) == 0:
		return nil, fmt.Errorf("%w；读不到模型配置，请先修正配置文件", err)
	case errors.Is(err, config.ErrInvalid):
		fmt.Fprintln(cmd.ErrOrStderr(), err)
	case err != nil:
		return nil, err
	}
	secret, err := config.DefaultAISecret()
	if err != nil {
		return nil, err
	}
	name, _ := cmd.Flags().GetString("ai")
	client, model, err := llm.Open(&cfg.AI, name, secret, os.Getenv)
	if err != nil {
		return nil, err
	}
	cacheDir, err := llm.DefaultCacheDir("announcements")
	if err != nil {
		slog.WarnContext(cmd.Context(), "无法创建公告摘要缓存目录，本次不缓存", "error", err)
	}
	return &llm.AnnouncementSummarizer{Client: client, Model: model, CacheDir: cacheDir}, nil
}

func printSummary(cmd *cobra.Command, summarizer *llm.AnnouncementSummarizer, url string, asJSON bool) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Minute)
	defer cancel()
	result, err := summarizer.Summarize(ctx, url)
	if err != nil {
		// 模型接口可能回显 Key，错误正文不直接交给用户。
		var apiErr *llm.APIError
		if errors.As(err, &apiErr) {
			return fmt.Errorf("公告摘要失败：模型接口返回 HTTP %d，请检查模型配置或稍后重试", apiErr.Status)
		}
		return fmt.Errorf("公告摘要失败：%w", err)
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
	fmt.Fprintf(out, "核对：%s\n", result.Check.String())
	for _, warning := range result.Check.Warnings() {
		fmt.Fprintf(out, "核对：%s\n", warning)
	}
	for _, note := range result.Notes {
		fmt.Fprintln(out, note)
	}
	fmt.Fprintln(out, result.Disclaimer)
	return nil
}
