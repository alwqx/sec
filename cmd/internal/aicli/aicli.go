// Package aicli 是调用模型的命令（估值解读、公告摘要、申报摘要）共用的部分：按 --ai 打开模型配置、
// 改写模型调用的错误、输出摘要的核对结果和说明。
package aicli

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/alwqx/sec/config"
	"github.com/alwqx/sec/llm"
	"github.com/spf13/cobra"
)

// SummaryTimeout 是一次摘要的整体时限，包括下载原文、分块请求和合并。
const SummaryTimeout = 10 * time.Minute

// Setup 是按 --ai 打开的模型和读到的配置。
type Setup struct {
	Client llm.Client
	Model  llm.Model
	// CacheDir 是 ~/.sec/cache/ai/<kind>，CLI 和 Desktop 共用；建不了时为空，本次不缓存。
	CacheDir string
	// Config 是读到的配置，命令可以复用其中的其他设置，比如 SEC 联系邮箱。
	Config *config.Config
}

// Open 读取配置并按 --ai 创建客户端，不写时用 default。在请求上游数据之前调用，配置有问题时不发请求。
func Open(cmd *cobra.Command, kind string) (*Setup, error) {
	cfg, err := config.Load(cmd.Context())
	switch {
	// 删除 ai 段不能让解读或摘要成功，只提示修正。
	case errors.Is(err, config.ErrInvalidAI):
		return nil, fmt.Errorf("%w；请先手动修正 ai 段", err)
	// 整个文件无效时 ai 段也换成了默认值，不能报「还没有模型配置」：照着 add 会覆盖文件、丢掉已存的 Key。
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
	cacheDir, err := llm.DefaultCacheDir(kind)
	if err != nil {
		slog.WarnContext(cmd.Context(), "无法创建 AI 缓存目录，本次不缓存", "kind", kind, "error", err)
	}
	return &Setup{Client: client, Model: model, CacheDir: cacheDir, Config: &cfg}, nil
}

// Error 把模型调用的失败改写成给用户看的错误，what 是功能名，如「公告摘要」。
// 模型接口和流内的错误正文可能回显 Key，只保留 llm.PublicError 给出的部分。
func Error(what string, err error) error {
	if message, ok := llm.PublicError(err); ok {
		return fmt.Errorf("%s失败：%s，请检查模型配置或稍后重试", what, message)
	}
	return fmt.Errorf("%s失败：%w", what, err)
}

// PrintSummaryFooter 输出摘要的核对结果、说明和免责声明。
func PrintSummaryFooter(out io.Writer, check llm.Check, notes []string, disclaimer string) {
	fmt.Fprintf(out, "核对：%s\n", check.String())
	for _, warning := range check.Warnings() {
		fmt.Fprintf(out, "核对：%s\n", warning)
	}
	for _, note := range notes {
		fmt.Fprintln(out, note)
	}
	fmt.Fprintln(out, disclaimer)
}
