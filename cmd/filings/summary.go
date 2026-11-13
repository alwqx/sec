package filings

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

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
	cfg, err := config.Load(cmd.Context())
	switch {
	case errors.Is(err, config.ErrInvalidAI):
		return nil, nil, fmt.Errorf("%w；请先手动修正 ai 段", err)
	case errors.Is(err, config.ErrInvalid) && len(cfg.AI.Profiles) == 0:
		return nil, nil, fmt.Errorf("%w；读不到模型配置，请先修正配置文件", err)
	case errors.Is(err, config.ErrInvalid):
		fmt.Fprintln(cmd.ErrOrStderr(), err)
	case err != nil:
		return nil, nil, err
	}
	secret, err := config.DefaultAISecret()
	if err != nil {
		return nil, nil, err
	}
	name, _ := cmd.Flags().GetString("ai")
	client, model, err := llm.Open(&cfg.AI, name, secret, os.Getenv)
	if err != nil {
		return nil, nil, err
	}
	cacheDir, err := llm.DefaultCacheDir("filings")
	if err != nil {
		slog.WarnContext(cmd.Context(), "无法创建申报摘要缓存目录，本次不缓存", "error", err)
	}
	sec := &edgar.Client{Email: cfg.SECEmail}
	return &llm.FilingSummarizer{Client: client, Model: model, CacheDir: cacheDir, Read: sec.Read8K}, &cfg, nil
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
	ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Minute)
	defer cancel()
	result, err := summarizer.Summarize(ctx, url)
	if err != nil {
		// 模型接口可能回显 Key，错误正文不直接交给用户。
		var apiErr *llm.APIError
		if errors.As(err, &apiErr) {
			return fmt.Errorf("申报摘要失败：模型接口返回 HTTP %d，请检查模型配置或稍后重试", apiErr.Status)
		}
		return fmt.Errorf("申报摘要失败：%w", err)
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
