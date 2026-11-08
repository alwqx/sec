package valuation

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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
}

func TestPrintExplanation(t *testing.T) {
	var out bytes.Buffer
	printExplanation(&out, &llm.Explanation{Text: "解读", Model: "m", Profile: "p", Disclaimer: "免责", Cached: true, Truncated: true, Check: llm.Check{Numbers: 3}})
	for _, text := range []string{"生成的缓存", "输出达到上限", "3 个数字都能在数据中找到", "免责"} {
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
