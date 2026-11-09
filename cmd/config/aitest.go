package config

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/alwqx/sec/config"
	"github.com/alwqx/sec/llm"
	"github.com/alwqx/sec/utils"
	"github.com/spf13/cobra"
)

func newAITestCLI() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "test [name]",
		Short: "Send a minimal request to check a model profile's address, key and tool calling; defaults to the default profile",
		Long: "Send two small requests with a profile: one asking for \"OK\" to check the address, key and model,\n" +
			"and one asking the model to call a test tool, since sec ask and the Desktop chat need tool calling.\n" +
			"Only a fixed test sentence is sent, none of your data. Works before AI is enabled.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkAI(cmd); err != nil {
				return err
			}
			cfg, err := load(cmd)
			if err != nil {
				return err
			}
			ai := cfg.AI
			name := ai.Default
			if len(args) == 1 {
				name = args[0]
			}
			if name == "" {
				return errors.New("还没有模型配置，先用 sec config ai add 添加")
			}
			if !ai.Has(name) {
				return fmt.Errorf("%w：%s", config.ErrAIProfileNotFound, name)
			}
			secret, err := config.DefaultAISecret()
			if err != nil {
				return err
			}
			result := llm.ProbeProfile(cmd.Context(), &ai, name, secret, os.Getenv)
			asJSON, _ := cmd.Flags().GetBool("json")
			if asJSON {
				if err := utils.PrintJSON(cmd.OutOrStdout(), result); err != nil {
					return err
				}
			} else {
				printAITest(cmd.OutOrStdout(), result)
			}
			// 原因已经在上面的输出里，返回的错误只说结论（main 会再打印一次），退出码非 0。
			if !result.OK {
				return fmt.Errorf("模型配置 %s 测试失败", name)
			}
			return nil
		},
	}
	cmd.Flags().Bool("json", false, "Output JSON")
	return cmd
}

func printAITest(out io.Writer, r llm.ProbeResult) {
	fmt.Fprintf(out, "测试模型配置 %s（%s）\n", r.Name, r.Provider)
	fmt.Fprintf(out, "  协议：%s\n  地址：%s\n  模型：%s\n  Key：%s\n", r.API, r.BaseURL, r.Model, r.Key)
	switch {
	case !r.Sent:
		fmt.Fprintln(out, "  数据：没有发出请求")
	case r.Local:
		fmt.Fprintln(out, "  数据：本机模型，不出本机")
	default:
		fmt.Fprintf(out, "  数据：发送给 %s，只发了一句固定的测试文字，不含你的数据\n", r.Provider)
	}
	if !r.OK {
		fmt.Fprintf(out, "连接失败：%s\n", r.Error)
		if r.Hint != "" {
			fmt.Fprintf(out, "建议：%s\n", r.Hint)
		}
		return
	}
	model := ""
	if r.ReplyModel != "" && r.ReplyModel != r.Model {
		model = "，实际模型 " + r.ReplyModel
	}
	fmt.Fprintf(out, "连接正常：%.1f 秒%s，回复 %q，用量 输入 %d / 输出 %d tokens\n", float64(r.LatencyMS)/1000, model, r.Reply, r.Usage.InputTokens, r.Usage.OutputTokens)
	if r.Warning != "" {
		fmt.Fprintf(out, "注意：%s\n", r.Warning)
	}
	switch r.Tools {
	case llm.ToolsSupported:
		fmt.Fprintln(out, "工具调用：支持")
	case llm.ToolsNotCalled:
		fmt.Fprintf(out, "工具调用：模型没有调用测试工具（%s），这个模型可能不支持工具调用，sec ask 和 Desktop 对话会用不了\n", r.ToolsDetail)
	case llm.ToolsError:
		fmt.Fprintf(out, "工具调用：带工具的请求被拒绝（%s），这个模型可能不支持工具调用，sec ask 和 Desktop 对话会用不了\n", r.ToolsDetail)
	case llm.ToolsUnknown:
		fmt.Fprintf(out, "工具调用：没测出来，带工具的请求临时失败（%s），稍后重试\n", r.ToolsDetail)
	}
}
