package ipo

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/alwqx/sec/llm"
	"github.com/alwqx/sec/provider/cninfo"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

const askURL = "https://static.cninfo.com.cn/finalpage/2025-11-28/1224820000.PDF"

func runIPO(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := NewIPOCLI()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func fakeAnswerer(t *testing.T) *llm.Fake {
	t.Helper()
	fake := &llm.Fake{Replies: []llm.FakeReply{llm.FakeText(`{"sections":[{"text":"实际控制人是张建中。","pages":[2]}]}`)}}
	old := openAnswerer
	openAnswerer = func(cmd *cobra.Command) (*llm.ProspectusAnswerer, error) {
		return &llm.ProspectusAnswerer{Client: fake, Model: llm.Model{Profile: "test", Name: "fake"}, Read: func(ctx context.Context, raw string) (*cninfo.Document, error) {
			require.Equal(t, askURL, raw)
			return &cninfo.Document{ID: "1224820000", URL: raw, AsOf: "2025-11-28", Pages: []cninfo.DocumentPage{
				{Number: 1, Text: "保荐人"}, {Number: 2, Text: "实际控制人 张建中"},
			}}, ctx.Err()
		}}, nil
	}
	t.Cleanup(func() { openAnswerer = old })
	return fake
}

func TestAskLink(t *testing.T) {
	fakeAnswerer(t)
	out, err := runIPO(t, "ask", askURL, "实际控制人", "是谁？")
	require.NoError(t, err)
	for _, text := range []string{"【AI 招股书问答】招股书 1224820000 · 披露日期 2025-11-28 · 共 2 页 · 模型 fake · 配置 test", "问题：实际控制人 是谁？",
		"实际控制人是张建中。（PDF 第 2 页）", "按问题挑出的页：PDF 第 2 页", "核对：", "不构成投资建议"} {
		require.Contains(t, out, text)
	}

	fakeAnswerer(t)
	out, err = runIPO(t, "ask", askURL, "实际控制人", "--json")
	require.NoError(t, err)
	var result llm.ProspectusAnswer
	require.NoError(t, json.Unmarshal([]byte(out), &result), "JSON 模式只输出回答对象")
	require.Equal(t, []int{2}, result.Pages)
}

func TestAskRejectsBeforeRequests(t *testing.T) {
	defer gock.Off()
	gock.Intercept()
	gock.CleanUnmatchedRequest()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	_, err := runIPO(t, "ask", "688795", "实际控制人是谁")
	require.ErrorIs(t, err, llm.ErrNoProfile)
	_, err = runIPO(t, "ask", "688795", " ")
	require.ErrorContains(t, err, "请提供问题")
	_, err = runIPO(t, "ask", "688795", "请问是什么？")
	require.ErrorContains(t, err, "没有可以用来查找的词")
	_, err = runIPO(t, "ask", "688795")
	require.Error(t, err)
	require.Empty(t, gock.GetUnmatchedRequests(), "模型配置或问题有问题时不请求巨潮和新浪")

	fakeAnswerer(t)
	_, err = runIPO(t, "ask", "https://evil.test/a.pdf", "实际控制人")
	require.ErrorIs(t, err, cninfo.ErrDocument)

	fakeAnswerer(t)
	_, err = runIPO(t, "ask", askURL, "募集资金")
	require.ErrorIs(t, err, llm.ErrProspectusAsk)
	require.Equal(t, 1, strings.Count(err.Error(), "招股书问答失败"), "错误前缀不重复：%v", err)
}

func TestPickProspectus(t *testing.T) {
	a := func(title string, at int64) *cninfo.Announcement { return &cninfo.Announcement{Title: title, Time: at} }
	list := []*cninfo.Announcement{
		a("摩尔线程首次公开发行股票并在科创板上市招股说明书提示性公告", 5),
		a("摩尔线程首次公开发行股票并在科创板上市招股意向书", 1),
		a("摩尔线程首次公开发行股票并在科创板上市招股说明书 ", 4),
		a("摩尔线程首次公开发行股票并在科创板上市招股意向书附录", 2),
		a("摩尔线程首次公开发行股票并在科创板上市招股说明书摘要（修订稿）", 6),
	}
	require.Equal(t, int64(4), pickProspectus(list).Time, "招股说明书优先，提示性公告和附录不算")
	require.Equal(t, int64(1), pickProspectus(list[1:2]).Time, "没有招股说明书时用招股意向书")
	require.Nil(t, pickProspectus(list[3:]))
	require.Equal(t, int64(7), pickProspectus(append(list, a("摩尔线程首次公开发行股票并在科创板上市招股说明书（注册稿）", 7))).Time, "末尾括号里的说明不影响")
	require.Equal(t, int64(3), pickProspectus([]*cninfo.Announcement{a("某公司招股意向书(更新后)", 3)}).Time)
}
