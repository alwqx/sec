// Package ask 实现 sec ask：模型调用 aitool 的只读工具查询数据，回答一个问题。设计见 docs/ai.md 的「问答：sec ask 与 Desktop 对话面板」。
package ask

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"

	"github.com/alwqx/sec/aitool"
	"github.com/alwqx/sec/cmd/internal/aicli"
	"github.com/alwqx/sec/cmd/macro"
	"github.com/alwqx/sec/llm"
	"github.com/alwqx/sec/utils"
	"github.com/spf13/cobra"
)

func NewAskCLI() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ask <question>",
		Short: "Ask an AI model a question, answered with sec's read-only data tools",
		Long: "The AI model calls sec's read-only tools (search, quote, K-line summary, financials, valuation, " +
			"filings, macro data) to answer the question. Tool calls are shown on stderr; the answer, the data dates " +
			"and the number check go to stdout. Press Ctrl-C to cancel.",
		Example: `  sec ask "贵州茅台最近 5 年的营业收入和净利润"
  sec ask 现在的估值处于历史什么位置 --code 600519
  sec ask "美国最新的 CPI 同比是多少" --ai local --json`,
		Args: cobra.MinimumNArgs(1),
		RunE: runAsk,
	}
	cmd.Flags().String("code", "", "Security code or name the question is about")
	cmd.Flags().String("ai", "", "Model profile, default profile if empty")
	cmd.Flags().Bool("json", false, "Output JSON")
	return cmd
}

// openAsker 按 --ai 创建问答的客户端，在请求模型之前调用，配置有问题时不发请求。测试时替换。
var openAsker = func(cmd *cobra.Command) (*llm.Asker, error) {
	// 问答不缓存，kind 为空，不建单独的缓存目录。
	setup, err := aicli.Open(cmd, "")
	if err != nil {
		return nil, err
	}
	return &llm.Asker{Client: setup.Client, Model: setup.Model, Tools: aitool.Default(macro.AISource())}, nil
}

func runAsk(cmd *cobra.Command, args []string) error {
	question, err := llm.NormalizeAskQuestion(strings.Join(args, " "))
	if err != nil {
		return err
	}
	code, _ := cmd.Flags().GetString("code")
	if code, err = llm.NormalizeAskCode(code); err != nil {
		return err
	}
	asJSON, _ := cmd.Flags().GetBool("json")
	asker, err := openAsker(cmd)
	if err != nil {
		return err
	}
	// Ctrl-C 取消正在进行的模型请求和工具调用。
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, aicli.SummaryTimeout)
	defer cancel()
	errOut := cmd.ErrOrStderr()
	steps := 0
	asker.OnStep = func(step llm.ToolStep) {
		steps++
		printStep(errOut, step)
	}
	answer, err := asker.Ask(ctx, question, code)
	switch {
	case errors.Is(err, context.Canceled) && ctx.Err() != nil:
		return errors.New("问答已取消")
	case errors.Is(err, context.DeadlineExceeded) && ctx.Err() != nil:
		return fmt.Errorf("问答超过 %d 分钟没有完成，请把问题问得具体一些或稍后重试", int(aicli.SummaryTimeout.Minutes()))
	case errors.Is(err, llm.ErrAsk):
		return err
	case err != nil:
		return askError(err, asker.Model.Profile, steps == 0)
	}
	if asJSON {
		return utils.PrintJSON(cmd.OutOrStdout(), answer)
	}
	printAnswer(cmd.OutOrStdout(), answer)
	return nil
}

// askError 改写模型调用的失败。还没有调用过工具时请求就被厂商拒绝（400、422），常常是模型不支持工具调用，
// 提示用连接测试检查；调用过工具之后的拒绝是别的原因（如超过上下文长度），不给这个提示。
func askError(err error, profile string, firstRound bool) error {
	rewritten := aicli.Error("问答", err)
	var apiErr *llm.APIError
	if firstRound && errors.As(err, &apiErr) && (apiErr.Status == http.StatusBadRequest || apiErr.Status == http.StatusUnprocessableEntity) {
		if profile == "" {
			profile = "<配置名>"
		}
		return fmt.Errorf("%w；模型可能不支持工具调用，可以用 sec config ai test %s 检查", rewritten, profile)
	}
	return rewritten
}

// printStep 在 stderr 显示一次工具调用：轮次、工具、参数、截止日期或失败原因、耗时。搜索结果和日期各不相同的结果没有截止日期。
func printStep(out io.Writer, step llm.ToolStep) {
	input := string(step.Input)
	var compact bytes.Buffer
	if json.Compact(&compact, step.Input) == nil {
		input = compact.String()
	}
	line := fmt.Sprintf("[%d] %s %s", step.Round, step.Tool, utils.TruncateRunes(input, 200))
	switch {
	case step.Error != "":
		line += " · 失败：" + step.Error
	case step.AsOf != "":
		line += " · 截止 " + step.AsOf
	}
	fmt.Fprintf(out, "%s · %.1fs\n", line, float64(step.ElapsedMS)/1000)
}

func printAnswer(out io.Writer, a *llm.Answer) {
	fmt.Fprintf(out, "【AI 问答】模型 %s · 配置 %s · 请求 %d 轮 · 调用工具 %d 次\n", a.Model, a.Profile, a.Rounds, len(a.Steps))
	question := a.Question
	if a.Code != "" {
		question = a.Code + " · " + question
	}
	fmt.Fprintf(out, "问题：%s\n\n%s\n\n", question, a.Text)
	if len(a.DataDates) > 0 {
		fmt.Fprintf(out, "数据截止：%s\n", strings.Join(a.DataDates, "；"))
	}
	if a.Truncated {
		fmt.Fprintf(out, "注意：输出达到上限，回答不完整%s\n", a.TruncationHint)
	}
	aicli.PrintSummaryFooter(out, a.Check, a.Notes, a.Disclaimer)
}
