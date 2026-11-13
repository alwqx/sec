package valuation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alwqx/sec/config"
	"github.com/alwqx/sec/llm"
	"github.com/alwqx/sec/provider/sina"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestExplain(t *testing.T) {
	r := sampleResult(10)
	sec := &sina.BasicSecurity{ExCode: "SH600036", Name: "招商银行"}
	fake := &llm.Fake{Replies: []llm.FakeReply{llm.FakeText("估值水平：当前 PE 10 倍，股息率 3.8%。")}}
	e := &llm.ValuationExplainer{Client: fake, Model: llm.Model{Profile: "ds", Name: "deepseek-chat"}}

	var out bytes.Buffer
	require.NoError(t, explain(context.Background(), &out, e, json.RawMessage(`{"code":"600036"}`), sec, r, false))
	require.Nil(t, e.Fetch, "不改调用方的 explainer")
	require.Contains(t, fake.Requests[0].Messages[0].Text, `"ex_code":"SH600036"`, "用算好的估值，不再请求")
	for _, text := range []string{"【AI 解读】模型 deepseek-chat，配置 ds", "估值水平：当前 PE 10 倍", "以下数字未在数据中找到：3.8%", "不构成投资建议"} {
		require.Contains(t, out.String(), text)
	}

	fake.Replies = []llm.FakeReply{llm.FakeText("估值水平：当前 PE 10 倍。")}
	out.Reset()
	require.NoError(t, explain(context.Background(), &out, e, json.RawMessage(`{"code":"600036"}`), sec, r, true))
	var got llm.Explanation
	require.NoError(t, json.Unmarshal(out.Bytes(), &got), "--json 只输出解读")
	require.Equal(t, "SH600036", got.ExCode)
	require.Equal(t, "ds", got.Profile)

	fake.Replies = []llm.FakeReply{llm.FakeText("")}
	require.ErrorContains(t, explain(context.Background(), &out, e, nil, sec, r, false), "估值解读失败")

	// 模型接口的错误正文可能回显 Key，只显示状态码。
	fake.Replies = []llm.FakeReply{{Err: &llm.APIError{Status: 401, Message: "invalid key sk-secret"}}}
	err := explain(context.Background(), &out, e, json.RawMessage(`{"code":"600036"}`), sec, r, false)
	require.ErrorContains(t, err, "HTTP 401")
	require.NotContains(t, err.Error(), "sk-secret")
}

func TestPrintExplanation(t *testing.T) {
	var out bytes.Buffer
	printExplanation(&out, &llm.Explanation{Text: "解读", Model: "m", Profile: "p", Disclaimer: "免责", Cached: true, Truncated: true, TruncationHint: "（思考模型的推理也计入输出上限）。可以关闭思考后重试", Check: llm.Check{Numbers: 3}})
	for _, text := range []string{"生成的缓存", "输出达到上限，解读不完整（思考模型的推理也计入输出上限）。可以关闭思考后重试", "3 个数字都能在数据中找到", "免责"} {
		require.Contains(t, out.String(), text)
	}
	out.Reset()
	printExplanation(&out, &llm.Explanation{Text: "解读"})
	require.Contains(t, out.String(), "回答里没有数字，未核对")
	require.NotContains(t, out.String(), "都能在数据中找到")
}

func TestExplainInput(t *testing.T) {
	cmd := NewValuationCLI()
	require.NoError(t, cmd.ParseFlags([]string{"--wacc", "10", "--growth-rate", "5"}))
	input, err := explainInput(cmd, "600036")
	require.NoError(t, err)
	require.JSONEq(t, `{"code":"600036","wacc":10,"growth":5}`, string(input), "只写改过的参数")
}

func TestExplainFlagsFailBeforeRequests(t *testing.T) {
	defer gock.Off()
	gock.CleanUnmatchedRequest()
	gock.Intercept()

	cmd := NewValuationCLI()
	cmd.SetArgs([]string{"600036", "--ai", "ds"})
	require.ErrorContains(t, cmd.Execute(), "--ai 只能和 --explain 一起用")

	// 没有模型配置时直接报错。Windows 读 USERPROFILE。
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	cmd = NewValuationCLI()
	cmd.SetArgs([]string{"600036", "--explain"})
	require.ErrorIs(t, cmd.Execute(), llm.ErrNoProfile)

	// 配置文件不是有效 JSON：说明文件无效，不提示 add（照着 add 会覆盖文件）。
	path := filepath.Join(home, ".sec", "config.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(`{"ai":{},}`), 0o600))
	cmd = NewValuationCLI()
	cmd.SetArgs([]string{"600036", "--explain"})
	err := cmd.Execute()
	require.ErrorIs(t, err, config.ErrInvalid)
	require.NotErrorIs(t, err, llm.ErrNoProfile)
	require.ErrorContains(t, err, "请先修正配置文件")

	// ai 段无效：只提示修正，不提示删除。
	require.NoError(t, os.WriteFile(path, []byte(`{"ai":{"enabled":"yes"}}`), 0o600))
	cmd = NewValuationCLI()
	cmd.SetArgs([]string{"600036", "--explain"})
	err = cmd.Execute()
	require.ErrorIs(t, err, config.ErrInvalidAI)
	require.NotContains(t, err.Error(), "删除")

	original := openExplainer
	t.Cleanup(func() { openExplainer = original })
	var name string
	openExplainer = func(cmd *cobra.Command) (*llm.ValuationExplainer, *config.Config, error) {
		name, _ = cmd.Flags().GetString("ai")
		return nil, nil, llm.ErrAIDisabled
	}
	cmd = NewValuationCLI()
	cmd.SetArgs([]string{"600036", "--explain", "--ai", "ds"})
	require.ErrorIs(t, cmd.Execute(), llm.ErrAIDisabled)
	require.Equal(t, "ds", name)
	require.False(t, gock.HasUnmatchedRequest(), "配置有问题时不发请求")
}

type cliStreamClient struct {
	llm.Fake
	stream func(func(llm.StreamEvent) error) (*llm.Response, error)
}

func (c *cliStreamClient) ChatStream(_ context.Context, _ llm.Request, emit func(llm.StreamEvent) error) (*llm.Response, error) {
	return c.stream(emit)
}

func TestExplainStreaming(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "完成", true: "断流"}[fail], func(t *testing.T) {
			var out bytes.Buffer
			client := &cliStreamClient{}
			client.stream = func(emit func(llm.StreamEvent) error) (*llm.Response, error) {
				for _, text := range []string{"当前 PE ", "10 倍。"} {
					if err := emit(llm.StreamEvent{Type: llm.StreamText, Text: text}); err != nil {
						return nil, err
					}
					require.Contains(t, out.String(), text, "返回完整结果前正文已写到终端")
				}
				if fail {
					return nil, errors.New("上游断流")
				}
				return llm.FakeText("当前 PE 10 倍。").Response, nil
			}
			e := &llm.ValuationExplainer{Client: client, Model: llm.Model{Name: "m", Profile: "p"}, CacheDir: t.TempDir()}
			sec := &sina.BasicSecurity{ExCode: "SH600036", Name: "招商银行"}
			err := explainStreaming(context.Background(), &out, e, nil, sec, sampleResult(10))
			require.Nil(t, e.Fetch)
			if fail {
				require.ErrorContains(t, err, "上游断流")
				require.Contains(t, out.String(), "未完成核对的草稿")
				require.NotContains(t, out.String(), "核对:")
			} else {
				require.NoError(t, err)
				require.Equal(t, 1, strings.Count(out.String(), "当前 PE 10 倍。"), "最终结果不重复打印正文")
				require.Contains(t, out.String(), "核对:")
				out.Reset()
				client.stream = func(func(llm.StreamEvent) error) (*llm.Response, error) {
					t.Fatal("缓存命中不请求模型")
					return nil, nil
				}
				require.NoError(t, explainStreaming(context.Background(), &out, e, nil, sec, sampleResult(10)))
				require.Contains(t, out.String(), "生成的缓存")
				require.Contains(t, out.String(), "当前 PE 10 倍。")
			}
		})
	}
	require.False(t, terminalOutput(&bytes.Buffer{}), "管道和捕获输出保留完整响应")
}
