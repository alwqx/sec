package llm

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	"github.com/alwqx/sec/provider/cninfo"
	"github.com/stretchr/testify/require"
)

const summaryURL = "https://static.cninfo.com.cn/finalpage/2026-11-01/123456.pdf"

func summaryReply(text string, pages ...int) FakeReply {
	data, _ := json.Marshal(map[string]any{"sections": []SummarySection{{Text: text, Pages: pages}}})
	return FakeText(string(data))
}

// routeClient 按请求正文选择回复，用于并发的分块请求。
type routeClient struct {
	Fake
	mu       sync.Mutex
	route    func(string) FakeReply
	Requests []Request
}

func (c *routeClient) Chat(ctx context.Context, req Request) (*Response, error) {
	c.mu.Lock()
	c.Requests = append(c.Requests, req)
	c.mu.Unlock()
	reply := c.route(req.Messages[0].Text)
	return reply.Response, reply.Err
}

func summaryReader(pages ...string) func(context.Context, string) (*cninfo.Document, error) {
	return func(ctx context.Context, url string) (*cninfo.Document, error) {
		doc, _ := cninfo.DocumentURL(url)
		for i, text := range pages {
			doc.Pages = append(doc.Pages, cninfo.DocumentPage{Number: i + 1, Text: text})
		}
		return doc, ctx.Err()
	}
}

func TestAnnouncementSummary(t *testing.T) {
	fake := &Fake{Replies: []FakeReply{summaryReply("收入为 1,200 万元，报告期 2025 年。", 1)}}
	s := AnnouncementSummarizer{Client: fake, Model: Model{Profile: "first", API: "openai", BaseURL: "http://localhost/v1", Name: "test"}, CacheDir: t.TempDir(), Read: summaryReader("收入为 1,200 万元，报告期 2025 年。", "")}
	result, err := s.Summarize(context.Background(), summaryURL)
	require.NoError(t, err)
	require.Equal(t, "2026-11-01", result.AsOf)
	require.Equal(t, "123456", result.DocumentID)
	require.Equal(t, 2, result.PageCount)
	require.Contains(t, result.Text, "（PDF 第 1 页）")
	require.True(t, result.Check.OK(), result.Check)
	require.Contains(t, strings.Join(result.Notes, ""), "第 2 页未提取到文本")
	require.Contains(t, result.Disclaimer, "不构成投资建议")
	require.Equal(t, "test", result.Model)
	require.Len(t, fake.Requests, 1)
	require.Empty(t, fake.Requests[0].Tools)
	require.Equal(t, announcementMaxTokens, fake.Requests[0].MaxTokens, "思考模型的推理也计入输出上限，不能用太小的上限")
	require.Contains(t, fake.Requests[0].System, "不可信")
	require.Contains(t, fake.Requests[0].Messages[0].Text, `"page":1`)
	require.NotContains(t, fake.Requests[0].Messages[0].Text, `"page":2`, "空白页不发送模型")

	// 命中缓存不下载 PDF、不请求模型；配置改名也能复用，API、地址和模型改变则失效。
	s.Read = func(context.Context, string) (*cninfo.Document, error) {
		t.Fatal("缓存不应下载 PDF")
		return nil, nil
	}
	s.Model.Profile = "renamed"
	cached, err := s.Summarize(context.Background(), summaryURL)
	require.NoError(t, err)
	require.True(t, cached.Cached)
	require.Equal(t, "renamed", cached.Profile)
	require.Equal(t, result.CreatedAt.UnixNano(), cached.CreatedAt.UnixNano())
	doc, _ := cninfo.DocumentURL(summaryURL)
	key := s.summaryKey(doc)
	for _, model := range []Model{{API: "anthropic"}, {API: "openai", BaseURL: "https://different"}, {API: "openai", BaseURL: "http://localhost/v1", Name: "other"}} {
		copy := s
		copy.Model = model
		require.NotEqual(t, key, copy.summaryKey(doc))
	}
}

func TestAnnouncementSummaryChunksAndMerge(t *testing.T) {
	first := "收入 1200 万元。" + strings.Repeat("文", announcementChunkRunes)
	read := summaryReader(first, "风险：合同尚需审批。")
	// 分块并发请求，按请求内容回复，不依赖到达顺序。
	fake := &routeClient{route: func(text string) FakeReply {
		switch {
		case strings.Contains(text, "合并分块摘要"):
			return FakeText(`{"sections":[{"text":"收入 1200 万元。","pages":[1]},{"text":"合同尚需审批。","pages":[2]}]}`)
		case strings.Contains(text, "风险"):
			return summaryReply("合同尚需审批。", 2)
		default:
			return summaryReply("收入 1200 万元。", 1)
		}
	}}
	s := AnnouncementSummarizer{Client: fake, Read: read}
	result, err := s.Summarize(context.Background(), summaryURL)
	require.NoError(t, err)
	require.Len(t, fake.Requests, 3)
	require.Contains(t, fake.Requests[2].Messages[0].Text, "合并分块摘要")
	require.NotContains(t, fake.Requests[2].Messages[0].Text, strings.Repeat("文", 100), "合并只发分块摘要")
	require.Len(t, result.Sections, 2)
	require.True(t, result.Check.OK())
	doc, err := read(context.Background(), summaryURL)
	require.NoError(t, err)
	chunks := chunkAnnouncement(doc.Pages)
	var joined strings.Builder
	for _, chunk := range chunks {
		n := 0
		for _, page := range chunk {
			require.True(t, utf8.ValidString(page.Text))
			n += utf8.RuneCountInString(page.Text)
			joined.WriteString(page.Text)
		}
		require.LessOrEqual(t, n, announcementChunkRunes)
	}
	require.Equal(t, first+"风险：合同尚需审批。", joined.String())
}

func TestAnnouncementSummaryChecksCitedPages(t *testing.T) {
	fake := &Fake{Replies: []FakeReply{summaryReply("收入 9900 万元，建议买入。", 1)}}
	s := AnnouncementSummarizer{Client: fake, CacheDir: t.TempDir(), Read: summaryReader("收入 1200 万元。", "其他收入 9900 万元。")}
	result, err := s.Summarize(context.Background(), summaryURL)
	require.NoError(t, err)
	require.Len(t, result.Check.Unsourced, 1, "不能用没有引用的页中的数字通过核对")
	require.Equal(t, "9900 万", result.Check.Unsourced[0].Text)
	require.NotEmpty(t, result.Check.AdviceTerms)
	files, err := os.ReadDir(s.CacheDir)
	require.NoError(t, err)
	require.Empty(t, files, "有警告的摘要不缓存")
	_, check := checkSummary([]SummarySection{{Text: "摘要。", Pages: []int{1}}, {Text: "金额 9999 元。", Pages: []int{1}}}, []cninfo.DocumentPage{{Number: 1, Text: "金额 1200 元。"}})
	require.Equal(t, utf8.RuneCountInString("摘要。（PDF 第 1 页）\n\n金额 "), check.Unsourced[0].Start)
}

func TestAnnouncementSummaryRejectsInvalidOutput(t *testing.T) {
	for _, reply := range []FakeReply{
		FakeText("没有引用"), FakeText(`{"sections":[]}`), summaryReply("摘要"), summaryReply("摘要", 0), summaryReply("摘要", 2), summaryReply("", 1), summaryReply(strings.Repeat("文", 401), 1),
		{Response: &Response{StopReason: StopMaxTokens, Message: Message{Text: `{"sections":[`}}},
		{Response: &Response{StopReason: StopRefusal}}, FakeToolCalls(ToolCall{Name: "anything"}),
	} {
		s := AnnouncementSummarizer{Client: &Fake{Replies: []FakeReply{reply}}, Read: summaryReader("原文"), CacheDir: t.TempDir()}
		_, err := s.Summarize(context.Background(), summaryURL)
		require.ErrorIs(t, err, ErrSummary)
		files, err := os.ReadDir(s.CacheDir)
		require.NoError(t, err)
		require.Empty(t, files)
	}
	// 截断单独说明原因，不和拒绝、未正常结束混在一起。
	s := AnnouncementSummarizer{Client: &Fake{Replies: []FakeReply{{Response: &Response{StopReason: StopMaxTokens, RawStopReason: "length"}}}}, Read: summaryReader("原文")}
	_, err := s.Summarize(context.Background(), summaryURL)
	require.ErrorIs(t, err, ErrSummary)
	require.ErrorContains(t, err, "分块摘要时回复超过 16000 token 的输出上限被截断")
	require.ErrorContains(t, err, "思考模型")
	// 合并被截断时不归咎于推理，换非思考模型帮不上忙。两块都只有第 1 页，分块回复相同，合并在最后。
	truncated := FakeReply{Response: &Response{StopReason: StopMaxTokens, RawStopReason: "length"}}
	s = AnnouncementSummarizer{Client: &Fake{Replies: []FakeReply{summaryReply("事项", 1), summaryReply("事项", 1), truncated}}, Read: summaryReader(strings.Repeat("文", announcementChunkRunes+1))}
	_, err = s.Summarize(context.Background(), summaryURL)
	require.ErrorContains(t, err, "合并分块摘要时回复超过 16000 token 的输出上限被截断")
	require.NotContains(t, err.Error(), "思考模型")
	for reply, want := range map[*Response]string{
		{StopReason: StopRefusal, RawStopReason: "content_filter"}: "模型拒绝或未正常结束（停止原因 content_filter）",
		{StopReason: StopOther}: "模型拒绝或未正常结束，请重试",
		{StopReason: StopToolUse, Message: Message{ToolCalls: []ToolCall{{Name: "x"}}}}: "模型调用了工具",
		{StopReason: StopEnd, Message: Message{ToolCalls: []ToolCall{{Name: "x"}}}}:     "模型调用了工具",
	} {
		s = AnnouncementSummarizer{Client: &Fake{Replies: []FakeReply{{Response: reply}}}, Read: summaryReader("原文")}
		_, err = s.Summarize(context.Background(), summaryURL)
		require.ErrorContains(t, err, want)
	}
	// 合并不能添加分块摘要里没有的页码，即使那页在 PDF 里真实存在。
	fake := &Fake{Replies: []FakeReply{summaryReply("事项", 1), summaryReply("事项", 1), summaryReply("事项", 2)}}
	s = AnnouncementSummarizer{Client: fake, Read: summaryReader(strings.Repeat("文", announcementChunkRunes+1), "存在但未被分块摘要引用")}
	_, err = s.Summarize(context.Background(), summaryURL)
	require.ErrorIs(t, err, ErrSummary)
}

type canceledSummaryClient struct{ cancel context.CancelFunc }

func (c canceledSummaryClient) Chat(context.Context, Request) (*Response, error) {
	c.cancel()
	return summaryReply("摘要", 1).Response, nil
}

func TestAnnouncementSummaryFailuresAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := AnnouncementSummarizer{Client: canceledSummaryClient{cancel}, Read: summaryReader("原文"), CacheDir: t.TempDir()}
	_, err := s.Summarize(ctx, summaryURL)
	require.ErrorIs(t, err, context.Canceled)
	files, err := os.ReadDir(s.CacheDir)
	require.NoError(t, err)
	require.Empty(t, files)
	s.Client = &Fake{Replies: []FakeReply{{Err: errors.New("offline")}}}
	_, err = s.Summarize(context.Background(), summaryURL)
	require.ErrorContains(t, err, "offline")
	s.Read = func(context.Context, string) (*cninfo.Document, error) { return nil, cninfo.ErrDocument }
	_, err = s.Summarize(context.Background(), summaryURL)
	require.ErrorIs(t, err, cninfo.ErrDocument)
	_, err = s.Summarize(context.Background(), "https://evil.test/file.pdf")
	require.ErrorIs(t, err, cninfo.ErrDocument)
	_, err = s.Summarize(ctx, summaryURL)
	require.ErrorIs(t, err, context.Canceled)

	// 损坏缓存重新生成，不能当作一次成功。
	s.Read = summaryReader("原文")
	s.Client = &Fake{Replies: []FakeReply{summaryReply("摘要", 1)}}
	doc, _ := cninfo.DocumentURL(summaryURL)
	require.NoError(t, os.WriteFile(filepath.Join(s.CacheDir, s.summaryKey(doc)+".json"), []byte("{"), 0o600))
	result, err := s.Summarize(context.Background(), summaryURL)
	require.NoError(t, err)
	require.False(t, result.Cached)
}

func TestAnnouncementSummaryTolerantOutput(t *testing.T) {
	body := `{"sections":[{"text":"摘要","pages":[1,1]}]}`
	for _, text := range []string{body, "```json\n" + body + "\n```", "```\n" + body + "\n```", "```JSON\r\n" + body + "\r\n```", "  ```json\n" + body + "\n```  "} {
		s := AnnouncementSummarizer{Client: &Fake{Replies: []FakeReply{FakeText(text)}}, Read: summaryReader("原文")}
		result, err := s.Summarize(context.Background(), summaryURL)
		require.NoError(t, err, text)
		require.Equal(t, []int{1}, result.Sections[0].Pages, "重复页码先去重再校验")
	}
	for _, text := range []string{"```python\n" + body + "\n```", "说明：\n```json\n" + body + "\n```"} {
		s := AnnouncementSummarizer{Client: &Fake{Replies: []FakeReply{FakeText(text)}}, Read: summaryReader("原文")}
		_, err := s.Summarize(context.Background(), summaryURL)
		require.ErrorIs(t, err, ErrSummary, text)
	}
}

func TestAnnouncementSummaryKeepsServedModel(t *testing.T) {
	served := func(r FakeReply, model string) FakeReply {
		copy := *r.Response
		copy.Model = model
		return FakeReply{Response: &copy}
	}
	client := &routeClient{route: func(text string) FakeReply {
		if strings.Contains(text, "合并分块摘要") {
			return FakeText(`{"sections":[{"text":"事项","pages":[1,2]}]}`)
		}
		if strings.Contains(text, "第二页") {
			return served(summaryReply("事项", 2), "served-v2")
		}
		return served(summaryReply("事项", 1), "served-v2")
	}}
	s := AnnouncementSummarizer{Client: client, Model: Model{Name: "alias"}, Read: summaryReader(strings.Repeat("文", announcementChunkRunes), "第二页")}
	result, err := s.Summarize(context.Background(), summaryURL)
	require.NoError(t, err)
	require.Equal(t, "served-v2", result.Model, "合并回复没有模型名时保留分块返回的实际模型")
}

func TestAnnouncementSummaryChunkFailure(t *testing.T) {
	client := &routeClient{route: func(text string) FakeReply {
		if strings.Contains(text, "坏块") {
			return FakeReply{Err: errors.New("offline")}
		}
		return summaryReply("事项", 1)
	}}
	s := AnnouncementSummarizer{Client: client, Read: summaryReader(strings.Repeat("文", announcementChunkRunes), "坏块")}
	_, err := s.Summarize(context.Background(), summaryURL)
	require.EqualError(t, err, "offline")
}
