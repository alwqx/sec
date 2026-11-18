package ask

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/alwqx/sec/aitool"
	"github.com/alwqx/sec/llm"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func runAskCmd(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	cmd := NewAskCLI()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	return out.String(), errOut.String(), err
}

// fakeAsker 替换 openAsker：模型先调用 get_quote，再给出回答。
func fakeAsker(t *testing.T, replies ...llm.FakeReply) *llm.Fake {
	t.Helper()
	quote := aitool.Tool{Name: "get_quote", Description: "行情", InputSchema: json.RawMessage(`{"type":"object"}`),
		Call: func(ctx context.Context, input json.RawMessage) (*aitool.Result, error) {
			result := aitool.NewResult("get_quote", "新浪财经行情", map[string]any{"price": 41.01})
			result.AsOf = "2026-11-17"
			return result, nil
		}}
	tools, err := aitool.NewRegistry(quote)
	require.NoError(t, err)
	fake := &llm.Fake{Replies: replies}
	old := openAsker
	openAsker = func(cmd *cobra.Command) (*llm.Asker, error) {
		return &llm.Asker{Client: fake, Model: llm.Model{Profile: "test", Name: "fake"}, Tools: tools}, nil
	}
	t.Cleanup(func() { openAsker = old })
	return fake
}

func TestAsk(t *testing.T) {
	quoteCall := llm.FakeToolCalls(llm.ToolCall{ID: "1", Name: "get_quote", Input: json.RawMessage(`{ "codes": ["600036"] }`)})
	fakeAsker(t, quoteCall, llm.FakeText("截至 2026-11-17，招商银行最新价 41.01 元。"))
	out, errOut, err := runAskCmd(t, "招商银行", "现在多少钱", "--code", "600036")
	require.NoError(t, err)
	require.Regexp(t, `^\[1\] get_quote \{"codes":\["600036"\]\} · 截止 2026-11-17 · \d+\.\ds\n$`, errOut, "工具调用显示在 stderr")
	for _, text := range []string{"【AI 问答】模型 fake · 配置 test · 请求 2 轮 · 调用工具 1 次", "问题：600036 · 招商银行 现在多少钱",
		"招商银行最新价 41.01 元。", "数据截止：get_quote 2026-11-17", "核对：", "不构成投资建议"} {
		require.Contains(t, out, text)
	}

	fakeAsker(t, quoteCall, llm.FakeText("最新价 41.01 元。"))
	out, _, err = runAskCmd(t, "现在多少钱", "--json")
	require.NoError(t, err)
	var answer llm.Answer
	require.NoError(t, json.Unmarshal([]byte(out), &answer), "JSON 模式只输出回答对象")
	require.Len(t, answer.Steps, 1)
	require.Equal(t, "2026-11-17", answer.Steps[0].AsOf)
	require.True(t, answer.Check.OK())
}

func TestAskErrors(t *testing.T) {
	old := openAsker
	openAsker = func(cmd *cobra.Command) (*llm.Asker, error) {
		t.Fatal("问题或证券不能用时不读模型配置")
		return nil, nil
	}
	_, _, err := runAskCmd(t, " ")
	require.ErrorContains(t, err, "问题不能为空")
	_, _, err = runAskCmd(t, "问题", "--code", strings.Repeat("长", 33))
	require.ErrorContains(t, err, "不能超过 32 字")
	openAsker = old

	fakeAsker(t, llm.FakeReply{Err: &llm.APIError{Provider: "Ollama", Status: 400, Message: "key sk-secret"}})
	_, _, err = runAskCmd(t, "问题", "--ai", "local")
	require.EqualError(t, err, "问答失败：Ollama 接口返回 HTTP 400，请检查模型配置或稍后重试；模型可能不支持工具调用，可以用 sec config ai test test 检查")

	// 调用过工具之后的 400 是别的原因（如超过上下文长度），不提示工具调用。
	quoteCall := llm.FakeToolCalls(llm.ToolCall{ID: "1", Name: "get_quote", Input: json.RawMessage(`{}`)})
	fakeAsker(t, quoteCall, llm.FakeReply{Err: &llm.APIError{Provider: "DeepSeek", Status: 400}})
	_, _, err = runAskCmd(t, "问题")
	require.EqualError(t, err, "问答失败：DeepSeek 接口返回 HTTP 400，请检查模型配置或稍后重试")

	fakeAsker(t, llm.FakeText(""))
	_, _, err = runAskCmd(t, "问题")
	require.ErrorContains(t, err, "模型没有给出回答")
}
