package ipo

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/alwqx/sec/cmd/internal/aicli"
	"github.com/alwqx/sec/llm"
	"github.com/alwqx/sec/provider/cninfo"
	"github.com/alwqx/sec/utils"
	"github.com/spf13/cobra"
)

/* ----------------------------- ipo ask ----------------------------- */

func newAskCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ask <code|prospectus PDF link> <question>",
		Short: "Ask an AI model about a stock's IPO prospectus, answered with PDF page citations",
		Long: "Pick the latest 招股说明书 (or 招股意向书) of the stock from CNINFO, or use the given CNINFO PDF link " +
			"(see sec ipo prospectus), select the pages related to the question and ask the AI model. " +
			"Each paragraph of the answer cites the PDF pages it is based on.",
		Example: `  sec ipo ask 688795 "最近三年的营业收入和净利润是多少？"
  sec ipo ask 688795 募集资金投向哪些项目 --ai local
  sec ipo ask https://static.cninfo.com.cn/finalpage/2025-11-28/1224820000.PDF 实际控制人是谁 --json`,
		Args: cobra.MinimumNArgs(2),
		RunE: runAsk,
	}
	cmd.Flags().String("ai", "", "Model profile, default profile if empty")
	cmd.Flags().Bool("json", false, "Output JSON")
	return cmd
}

// openAnswerer 按 --ai 创建招股书问答的客户端。在请求巨潮之前调用，配置有问题时不发请求。测试时替换。
var openAnswerer = func(cmd *cobra.Command) (*llm.ProspectusAnswerer, error) {
	setup, err := aicli.Open(cmd, "prospectus")
	if err != nil {
		return nil, err
	}
	return &llm.ProspectusAnswerer{Client: setup.Client, Model: setup.Model, CacheDir: setup.CacheDir}, nil
}

func runAsk(cmd *cobra.Command, args []string) error {
	question := strings.Join(args[1:], " ")
	if strings.TrimSpace(question) == "" {
		return fmt.Errorf("请提供问题，比如 sec ipo ask %s 最近三年的营业收入是多少", args[0])
	}
	// 问题太长或没有可以查找的词时，在查股票、下载 PDF 之前报错。
	question, err := llm.NormalizeQuestion(question)
	if err != nil {
		return err
	}
	asJSON, _ := cmd.Flags().GetBool("json")
	answerer, err := openAnswerer(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), aicli.SummaryTimeout)
	defer cancel()
	link, title, err := prospectusTarget(ctx, args[0])
	if err != nil {
		return err
	}
	result, err := answerer.Ask(ctx, link, question)
	// PDF 的问题（下载失败、扫描件、超过大小）和问答自己的错误（已带「招股书问答失败」）原样返回，只改写模型接口的错误。
	if errors.Is(err, cninfo.ErrDocument) || errors.Is(err, llm.ErrProspectusAsk) {
		return err
	}
	if err != nil {
		return aicli.Error("招股书问答", err)
	}
	if asJSON {
		return utils.PrintJSON(cmd.OutOrStdout(), result)
	}
	out := cmd.OutOrStdout()
	if title == "" {
		title = "招股书 " + result.DocumentID
	}
	fmt.Fprintf(out, "【AI 招股书问答】%s · 披露日期 %s · 共 %d 页 · 模型 %s · 配置 %s\n", title, result.AsOf, result.PageCount, result.Model, result.Profile)
	if result.Cached {
		fmt.Fprintf(out, "缓存生成于 %s\n", result.CreatedAt.Local().Format(utils.StandardTimeLayout))
	}
	fmt.Fprintf(out, "问题：%s\n\n%s\n\n原文：%s\n", result.Question, result.Text, result.SourceURL)
	fmt.Fprintf(out, "按问题挑出的页：%s\n", llm.CitePDFPages(result.Pages))
	aicli.PrintSummaryFooter(out, result.Check, result.Notes, result.Disclaimer)
	return nil
}

// prospectusTarget 返回要问的招股书 PDF 地址和用于显示的标题：input 是巨潮 PDF 链接时直接用（标题为空），
// 否则按股票代码或名称查 IPO 公告，取最新的招股说明书，没有时取招股意向书。
func prospectusTarget(ctx context.Context, input string) (string, string, error) {
	if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
		if _, err := cninfo.DocumentURL(input); err != nil {
			return "", "", err
		}
		return input, "", nil
	}
	code, orgID, name, err := resolveStock(ctx, input)
	if err != nil {
		return "", "", err
	}
	announcements, err := cninfo.QueryIPOs(ctx, code+","+orgID, 100)
	if err != nil {
		return "", "", fmt.Errorf("查询招股书失败: %w", err)
	}
	a := pickProspectus(filterValidPDFs(announcements))
	if a == nil {
		return "", "", fmt.Errorf("没有找到 %s (%s) 的招股说明书或招股意向书 PDF；可以用 sec ipo prospectus %s 查看公告，再把 PDF 链接传给 sec ipo ask", name, code, code)
	}
	return a.PDFURL, fmt.Sprintf("%s %s", name, strings.TrimSpace(a.Title)), nil
}

// pickProspectus 返回最新的招股说明书，没有时返回最新的招股意向书；摘要、附录、提示性公告不是全文，见 cninfo.ProspectusKind。
func pickProspectus(announcements []*cninfo.Announcement) *cninfo.Announcement {
	for _, kind := range []string{cninfo.KindProspectus, cninfo.KindPreliminary} {
		var best *cninfo.Announcement
		for _, a := range announcements {
			if cninfo.ProspectusKind(a.Title) == kind && (best == nil || a.Time > best.Time) {
				best = a
			}
		}
		if best != nil {
			return best
		}
	}
	return nil
}
