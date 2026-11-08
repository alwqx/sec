package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/alwqx/sec/config"
	"github.com/alwqx/sec/llm"
	"github.com/alwqx/sec/utils"
	"github.com/spf13/cobra"
)

// aiTestTimeout 是测试时每个请求的超时。思考模型即使只回 OK 也可能想一会儿。
const aiTestTimeout = 2 * time.Minute

// aiTestMaxTokens 是测试请求的输出上限：要给思考模型留出推理的余量，否则可能在回答前就被截断。
const aiTestMaxTokens = 1024

// pingTool 是测试工具调用用的工具，不会真的执行。
var pingTool = llm.Tool{
	Name:        "ping",
	Description: "连接测试用的工具，原样返回 text。",
	InputSchema: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}},"required":["text"]}`),
}

// 工具调用测试的结果。
const (
	aiToolsSupported = "supported"  // 调用了 ping
	aiToolsNotCalled = "not_called" // 请求成功但没有调用工具
	aiToolsError     = "error"      // 带工具的请求被拒绝，比如模型不支持工具调用
	aiToolsUnknown   = "unknown"    // 带工具的请求临时失败（限流、5xx、超时、网络中断），没测出来
)

// aiTestUsage 是测试请求的 token 用量。
type aiTestUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// aiTestResult 是一次测试的结果，--json 原样输出。
type aiTestResult struct {
	Name        string      `json:"name"`
	Provider    string      `json:"provider"`
	API         string      `json:"api"`
	BaseURL     string      `json:"base_url"`
	Model       string      `json:"model"`
	Key         string      `json:"key"`
	Local       bool        `json:"local"`
	OK          bool        `json:"ok"`
	Error       string      `json:"error,omitempty"`
	Hint        string      `json:"hint,omitempty"`
	Sent        bool        `json:"sent"` // 是否真的发出了请求；读取 Key、创建客户端时就失败的不会发出
	LatencyMS   int64       `json:"latency_ms"`
	Warning     string      `json:"warning,omitempty"`
	ReplyModel  string      `json:"reply_model,omitempty"`
	Reply       string      `json:"reply,omitempty"`
	Usage       aiTestUsage `json:"usage"`
	Tools       string      `json:"tools,omitempty"`
	ToolsDetail string      `json:"tools_detail,omitempty"`
}

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
			result := testAIProfile(cmd.Context(), ai, name, secret)
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

// testAIProfile 用一个配置发两个测试请求，结果和失败原因都写在返回值里。
func testAIProfile(ctx context.Context, ai config.AIConfig, name string, secret *config.AISecret) aiTestResult {
	p := ai.Profiles[name]
	provider, _ := config.LookupAIProvider(p.Provider)
	// 只读取一次 Key：显示文字和创建客户端用同一个结果。
	key, keyErr := ai.ResolveKey(secret, name, os.Getenv)
	result := aiTestResult{
		Name: name, Provider: provider.Name, API: p.EffectiveAPI(), BaseURL: p.EffectiveBaseURL(), Model: p.Model,
		Key: ai.DescribeKey(name, key, keyErr), Local: p.IsLocal(),
	}
	fail := func(err error) aiTestResult {
		result.Error, result.Hint = err.Error(), aiTestHint(err, p)
		return result
	}
	if keyErr != nil {
		return fail(keyErr)
	}
	client, err := llm.New(p, key.Value)
	if err != nil {
		return fail(err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, aiTestTimeout)
	defer cancel()
	start := time.Now()
	result.Sent = true
	resp, err := client.Chat(reqCtx, llm.Request{
		System:    "这是 sec 的连接测试。",
		Messages:  []llm.Message{{Role: llm.RoleUser, Text: "请只回复 OK。"}},
		MaxTokens: aiTestMaxTokens,
	})
	if err != nil {
		return fail(err)
	}
	result.OK, result.LatencyMS = true, time.Since(start).Milliseconds()
	result.ReplyModel, result.Reply = resp.Model, utils.TruncateRunes(resp.Message.Text, 80)
	result.Usage = aiTestUsage{InputTokens: resp.Usage.InputTokens, OutputTokens: resp.Usage.OutputTokens}
	// 连接是通的，但思考模型可能把输出上限都用在推理上，回复被截断或为空。
	if resp.StopReason == llm.StopMaxTokens || strings.TrimSpace(resp.Message.Text) == "" {
		result.Warning = fmt.Sprintf("回复被截断或为空（停止原因 %s），模型可能需要更大的输出上限，或者模型本身有问题", resp.RawStopReason)
	}

	toolCtx, cancelTool := context.WithTimeout(ctx, aiTestTimeout)
	defer cancelTool()
	resp, err = client.Chat(toolCtx, llm.Request{
		System:    "这是 sec 的连接测试。需要时调用工具。",
		Messages:  []llm.Message{{Role: llm.RoleUser, Text: "请调用 ping 工具，参数 text 填 sec。"}},
		Tools:     []llm.Tool{pingTool},
		MaxTokens: aiTestMaxTokens,
	})
	var apiErr *llm.APIError
	switch {
	// 只有厂商明确拒绝（如 400、422）才可能是不支持工具调用；网络错误、被取消、可重试的状态码都算没测出来。
	case err != nil && (toolCtx.Err() != nil || !errors.As(err, &apiErr) || apiErr.Retryable()):
		result.Tools, result.ToolsDetail = aiToolsUnknown, err.Error()
	case err != nil:
		result.Tools, result.ToolsDetail = aiToolsError, err.Error()
	case slices.ContainsFunc(resp.Message.ToolCalls, func(c llm.ToolCall) bool { return c.Name == pingTool.Name }):
		result.Tools = aiToolsSupported
	default:
		result.Tools, result.ToolsDetail = aiToolsNotCalled, "停止原因 "+resp.RawStopReason
	}
	return result
}

// aiTestHint 根据失败原因给出下一步怎么做。
func aiTestHint(err error, p config.AIProfile) string {
	var apiErr *llm.APIError
	switch {
	case errors.Is(err, llm.ErrMissingKey):
		return "" // 错误信息里已经说了怎么设置
	case errors.Is(err, config.ErrSecretMissing), errors.Is(err, config.ErrKeyUndecryptable), errors.Is(err, config.ErrSecretCorrupt):
		return "保存的 Key 无法解密，用 sec config ai set-key 重新设置"
	case errors.Is(err, config.ErrSecretPermissions):
		return "secret.key 权限过宽，按上面的提示运行 chmod 600"
	case errors.As(err, &apiErr):
		switch {
		case apiErr.Status == 401 || apiErr.Status == 403:
			return "Key 不对或没有权限，用 sec config ai set-key 重新设置；国际区域和中国区域的 Key 不通用"
		case apiErr.Status == 404:
			return "地址或模型名可能不对，用 sec config ai edit 修改 --base-url 或 --model"
		case apiErr.Status == 400:
			return "请求被拒绝，检查模型名和协议（--api）是否和厂商一致"
		case apiErr.Status == 402 || apiErr.Status == 429:
			return "被限流或余额不足，稍后再试或检查账户"
		case apiErr.Status >= 500:
			return "厂商服务暂时不可用，稍后再试"
		}
	case connRefused(err) && p.IsLocal():
		return "本机模型服务可能没有启动，比如先运行 ollama serve"
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Sprintf("%v 内没有回复，模型可能太慢或地址不通", aiTestTimeout)
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return "连不上接口地址，检查网络、代理或 --base-url"
	}
	return ""
}

// connRefused 判断是不是连接被拒绝。Windows 上是 WSAECONNREFUSED（10061），和 syscall.ECONNREFUSED 不是同一个值。
func connRefused(err error) bool {
	var errno syscall.Errno
	return errors.As(err, &errno) && (errno == syscall.ECONNREFUSED || errno == wsaeConnRefused)
}

// wsaeConnRefused 是 Windows 的 WSAECONNREFUSED。
const wsaeConnRefused = syscall.Errno(10061)

func printAITest(out io.Writer, r aiTestResult) {
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
	case aiToolsSupported:
		fmt.Fprintln(out, "工具调用：支持")
	case aiToolsNotCalled:
		fmt.Fprintf(out, "工具调用：模型没有调用测试工具（%s），这个模型可能不支持工具调用，sec ask 和 Desktop 对话会用不了\n", r.ToolsDetail)
	case aiToolsError:
		fmt.Fprintf(out, "工具调用：带工具的请求被拒绝（%s），这个模型可能不支持工具调用，sec ask 和 Desktop 对话会用不了\n", r.ToolsDetail)
	case aiToolsUnknown:
		fmt.Fprintf(out, "工具调用：没测出来，带工具的请求临时失败（%s），稍后重试\n", r.ToolsDetail)
	}
}
