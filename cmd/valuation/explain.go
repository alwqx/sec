package valuation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/alwqx/sec/aitool"
	"github.com/alwqx/sec/config"
	"github.com/alwqx/sec/llm"
	"github.com/alwqx/sec/provider/sina"
	"github.com/alwqx/sec/utils"
	shared "github.com/alwqx/sec/valuation"
	"github.com/spf13/cobra"
)

// openExplainer 按 --ai 创建估值解读用的客户端，不写时用 default，同时返回读到的配置，美股估值复用。
// 在请求估值数据之前调用，配置有问题时不发请求。测试时替换。
var openExplainer = func(cmd *cobra.Command) (*llm.ValuationExplainer, *config.Config, error) {
	cfg, err := config.Load(cmd.Context())
	switch {
	// 删除 ai 段不能让解读成功，只提示修正。
	case errors.Is(err, config.ErrInvalidAI):
		return nil, nil, fmt.Errorf("%w；请先手动修正 ai 段", err)
	// 整个文件无效时 ai 段也换成了默认值，不能报「还没有模型配置」：照着 add 会覆盖文件、丢掉已存的 Key。
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
	// 缓存目录建不了时不缓存，不影响解读。
	cacheDir, err := llm.DefaultCacheDir("valuation")
	if err != nil {
		slog.WarnContext(cmd.Context(), "无法创建解读缓存目录，本次不缓存", "error", err)
	}
	return &llm.ValuationExplainer{Client: client, Model: model, CacheDir: cacheDir}, &cfg, nil
}

// explainInput 是 get_valuation 的参数：代码和用户改过的 DCF 参数。没改的参数不写，和默认参数共用缓存。
func explainInput(cmd *cobra.Command, code string) (json.RawMessage, error) {
	input := map[string]any{"code": code}
	for flag, key := range map[string]string{
		"growth-rate": "growth", "terminal-growth": "terminal_growth", "wacc": "wacc", "margin-of-safety": "margin_of_safety",
	} {
		if cmd.Flags().Changed(flag) {
			input[key], _ = cmd.Flags().GetFloat64(flag)
		}
	}
	return json.Marshal(input)
}

// explain 用已经算好的估值生成解读，不再请求一次估值数据。asJSON 时只输出解读。
// 在副本上设置 Fetch，不改调用方的 explainer。
func explain(ctx context.Context, out io.Writer, explainer *llm.ValuationExplainer, input json.RawMessage, sec *sina.BasicSecurity, r *shared.Result, asJSON bool) error {
	e := *explainer
	e.Fetch = func(context.Context, json.RawMessage) (*aitool.Result, error) {
		return aitool.ValuationResult(sec, r), nil
	}
	explanation, err := e.Explain(ctx, input)
	if err != nil {
		return fmt.Errorf("估值解读失败：%w", err)
	}
	if asJSON {
		return utils.PrintJSON(out, explanation)
	}
	printExplanation(out, explanation)
	return nil
}

func printExplanation(out io.Writer, x *llm.Explanation) {
	source := fmt.Sprintf("模型 %s，配置 %s", x.Model, x.Profile)
	if x.Cached {
		source += fmt.Sprintf("，%s 生成的缓存", x.CreatedAt.Local().Format(utils.StandardTimeLayout))
	}
	fmt.Fprintf(out, "【AI 解读】%s\n\n%s\n\n", source, x.Text)
	if x.Truncated {
		fmt.Fprintln(out, "注意: 输出达到上限，解读不完整")
	}
	warnings := x.Check.Warnings()
	switch {
	case len(warnings) > 0:
	case x.Check.Numbers == 0:
		fmt.Fprintln(out, "核对: 回答里没有数字，未核对")
	default:
		fmt.Fprintf(out, "核对: %d 个数字都能在数据中找到\n", x.Check.Numbers)
	}
	for _, warning := range warnings {
		fmt.Fprintf(out, "核对: %s\n", warning)
	}
	fmt.Fprintf(out, "\n%s\n\n", x.Disclaimer)
}
