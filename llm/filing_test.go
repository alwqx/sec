package llm

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/alwqx/sec/provider/edgar"
	"github.com/stretchr/testify/require"
)

const (
	filingURL   = "https://www.sec.gov/Archives/edgar/data/320193/000032019326000024/aapl-20261102.htm"
	exhibitURL  = "https://www.sec.gov/Archives/edgar/data/320193/000032019326000024/ex991.htm"
	filingIndex = "https://www.sec.gov/Archives/edgar/data/320193/000032019326000024/0000320193-26-000024-index.htm"
)

func filingReply(text string, refs ...string) FakeReply {
	data, _ := json.Marshal(map[string]any{"sections": []FilingSection{{Text: text, Refs: refs}}})
	return FakeText(string(data))
}

// filingReader 返回一份 8-K：texts 依次是 Item 2.02 和各附件的原文。
func filingReader(item string, exhibits ...string) func(context.Context, string) (*edgar.FilingDocument, error) {
	return func(ctx context.Context, raw string) (*edgar.FilingDocument, error) {
		ref, err := edgar.ParseFilingURL(raw)
		if err != nil {
			return nil, err
		}
		doc := &edgar.FilingDocument{FilingRef: *ref, Form: "8-K", FilingDate: "2026-11-02", ReportDate: "2026-11-02", URL: filingURL,
			Sections: []edgar.DocumentSection{{Label: "Item 2.02", URL: filingURL, Text: item}}, Notes: []string{"Exhibit 99.2 不是 HTML 或文本文件，未参与摘要。"}}
		for i, text := range exhibits {
			doc.Sections = append(doc.Sections, edgar.DocumentSection{Label: "Exhibit 99." + string(rune('1'+i)), URL: exhibitURL, Text: text})
		}
		return doc, ctx.Err()
	}
}

const (
	itemText    = "Item 2.02 Results of Operations. On November 2, 2026, Apple issued a press release. See Exhibit 99.1."
	exhibitText = "Apple reported quarterly revenue of $114.5 billion, up 12 percent. Net income $ 30,711 (In millions)."
)

func TestFilingSummary(t *testing.T) {
	fake := &Fake{Replies: []FakeReply{filingReply("Apple 于 2026 年 11 月 2 日公布季度营收 1,145 亿美元，同比增长 12%，净利润 30,711 million 美元。", "Exhibit 99.1", "Item 2.02", "Item 2.02")}}
	s := FilingSummarizer{Client: fake, Model: Model{Profile: "first", API: "openai", BaseURL: "http://localhost/v1", Name: "test"}, CacheDir: t.TempDir(), Read: filingReader(itemText, exhibitText)}
	result, err := s.Summarize(context.Background(), exhibitURL)
	require.NoError(t, err)
	require.Equal(t, "0000320193-26-000024", result.DocumentID)
	require.Equal(t, "8-K", result.Form)
	require.Equal(t, "2026-11-02", result.AsOf)
	require.Equal(t, filingURL, result.SourceURL)
	require.Equal(t, filingIndex, result.IndexURL)
	require.Equal(t, []FilingSource{{"Item 2.02", filingURL}, {"Exhibit 99.1", exhibitURL}}, result.Sources)
	require.Equal(t, []string{"Item 2.02", "Exhibit 99.1"}, result.Sections[0].Refs, "引用去重，并按原文顺序排列")
	require.True(t, strings.HasSuffix(result.Text, "（Item 2.02、Exhibit 99.1）"), result.Text)
	require.True(t, result.Check.OK(), "英文月份、billion 和千分位都能找到来源：%v", result.Check)
	require.Contains(t, strings.Join(result.Notes, ""), "Exhibit 99.2 不是 HTML")
	require.Contains(t, result.Disclaimer, "不构成投资建议")
	require.Len(t, fake.Requests, 1)
	req := fake.Requests[0]
	require.Empty(t, req.Tools)
	require.Equal(t, summaryMaxTokens, req.MaxTokens)
	require.Contains(t, req.System, "不可信")
	require.Contains(t, req.Messages[0].Text, `"label":"Exhibit 99.1"`)
	require.Contains(t, req.Messages[0].Text, `"form":"8-K"`)
	require.NotContains(t, req.Messages[0].Text, exhibitURL, "不把地址发给模型")

	// 命中缓存不读原文、不请求模型；配置改名也能复用，模型改变则失效。
	s.Read = func(context.Context, string) (*edgar.FilingDocument, error) {
		t.Fatal("缓存不应读取原文")
		return nil, nil
	}
	s.Model.Profile = "renamed"
	cached, err := s.Summarize(context.Background(), filingIndex)
	require.NoError(t, err, "同一申报目录下的其他链接也命中缓存")
	require.True(t, cached.Cached)
	require.Equal(t, "renamed", cached.Profile)
	ref, _ := edgar.ParseFilingURL(filingURL)
	key := s.summaryKey(ref)
	other := s
	other.Model.Name = "other"
	require.NotEqual(t, key, other.summaryKey(ref))
}

func TestFilingSummaryTrimsRefs(t *testing.T) {
	fake := &Fake{Replies: []FakeReply{FakeText(`{"sections":[{"text":"公司发布业绩新闻稿。","refs":["Item 2.02 "," Exhibit 99.1"]}]}`)}}
	s := FilingSummarizer{Client: fake, Read: filingReader(itemText, exhibitText)}
	result, err := s.Summarize(context.Background(), filingURL)
	require.NoError(t, err, "引用前后的空格去掉后再和输入比较")
	require.Equal(t, []string{"Item 2.02", "Exhibit 99.1"}, result.Sections[0].Refs)
}

func TestFilingSummaryCheckWarnings(t *testing.T) {
	// 每段只和引用的节核对：12% 只在附件里，只引用 Item 2.02 时找不到；December 不在原文里，12 月也找不到。
	fake := &Fake{Replies: []FakeReply{filingReply("营收同比增长 12%，将于 12 月公布。", "Item 2.02")}}
	s := FilingSummarizer{Client: fake, CacheDir: t.TempDir(), Read: filingReader(itemText, exhibitText)}
	result, err := s.Summarize(context.Background(), filingURL)
	require.NoError(t, err)
	require.False(t, result.Check.OK())
	require.Len(t, result.Check.Unsourced, 2)
	files, err := os.ReadDir(s.CacheDir)
	require.NoError(t, err)
	require.Empty(t, files, "有核对警告不缓存")
}

func TestFilingSummaryChunksAndMerge(t *testing.T) {
	long := exhibitText + strings.Repeat(" a", filingChunkRunes)
	client := &routeClient{route: func(text string) FakeReply {
		switch {
		case strings.Contains(text, "合并分块摘要"):
			return FakeText(`{"sections":[{"text":"季度营收 114.5 billion 美元。","refs":["Item 2.02","Exhibit 99.1"]}]}`)
		case strings.Contains(text, "Item 2.02"):
			return filingReply("公司发布业绩新闻稿。", "Item 2.02")
		default:
			return filingReply("季度营收 114.5 billion 美元。", "Exhibit 99.1")
		}
	}}
	s := FilingSummarizer{Client: client, Read: filingReader(itemText, long)}
	result, err := s.Summarize(context.Background(), filingURL)
	require.NoError(t, err)
	require.Len(t, client.Requests, 4, "正文和长附件分成三块，再合并一次")
	require.True(t, result.Check.OK(), result.Check)

	// 合并不能添加分块摘要没有引用的节，即使申报里真的有。
	client.route = func(text string) FakeReply {
		if strings.Contains(text, "合并分块摘要") {
			return filingReply("事项", "Exhibit 99.2")
		}
		return filingReply("事项", "Exhibit 99.1")
	}
	s.Read = filingReader("", long, "存在但未被分块摘要引用")
	_, err = s.Summarize(context.Background(), filingURL)
	require.ErrorIs(t, err, ErrFilingSummary)
	require.ErrorContains(t, err, "不存在的事项或附件")
}

func TestFilingSummaryFailures(t *testing.T) {
	read := filingReader(itemText)
	for reply, want := range map[*Response]string{
		{StopReason: StopMaxTokens, Message: Message{Text: `{"sections":`}}:                                                                               "申报可能太长",
		{StopReason: StopMaxTokens, Reasoned: true}:                                                                                                       "思考模型",
		{StopReason: StopToolUse, Message: Message{ToolCalls: []ToolCall{{Name: "x"}}}}:                                                                   "申报摘要不提供工具",
		{StopReason: StopEnd, Message: Message{Text: `{"sections":[{"text":"事项"}]}`}}:                                                                     "段落缺少有效引用",
		{StopReason: StopEnd, Message: Message{Text: `{"sections":[{"text":"` + strings.Repeat("长", filingSectionRunes+1) + `","refs":["Item 2.02"]}]}`}}: "超过 600 字的上限",
		{StopReason: StopEnd, Message: Message{Text: `{"sections":[{"text":"事项","refs":"Item 2.02"}]}`}}:                                                  "带引用的结构化摘要",
		{StopReason: StopEnd, Message: Message{Text: "不是 JSON"}}:                                                                                          "带引用的结构化摘要",
		{StopReason: StopEnd, Message: Message{Text: `{"sections":[{"text":"事项","refs":["Item 5.02"]}]}`}}:                                                "不存在的事项或附件",
	} {
		s := FilingSummarizer{Client: &Fake{Replies: []FakeReply{{Response: reply}}}, Read: read}
		_, err := s.Summarize(context.Background(), filingURL)
		require.ErrorIs(t, err, ErrFilingSummary, want)
		require.ErrorContains(t, err, want)
	}
	s := FilingSummarizer{Client: &Fake{}, Read: func(context.Context, string) (*edgar.FilingDocument, error) {
		t.Fatal("无效链接不读原文")
		return nil, nil
	}}
	_, err := s.Summarize(context.Background(), "https://evil.test/a.htm")
	require.ErrorIs(t, err, edgar.ErrDocument)
	s.Read = func(context.Context, string) (*edgar.FilingDocument, error) { return nil, edgar.ErrDocument }
	_, err = s.Summarize(context.Background(), filingURL)
	require.ErrorIs(t, err, edgar.ErrDocument)
	s.Read = nil
	_, err = s.Summarize(context.Background(), filingURL)
	require.ErrorIs(t, err, ErrFilingSummary)
}

func TestFilingNumberSource(t *testing.T) {
	source := filingNumberSource("Dated Sept. 5, 2026 and December 2026; Market 2 and may 3 are not months; $1,234.5 million")
	require.Contains(t, source, "1234.5 million")
	require.True(t, strings.HasSuffix(source, "\n12 9"), source)
}

const (
	tenKURL  = "https://www.sec.gov/Archives/edgar/data/320193/000032019326000080/aapl-20260926.htm"
	riskText = "Item 1A. Risk Factors. The Company depends on component suppliers in Asia. Tariffs could increase costs."
	mdnaText = "Item 7. Management’s Discussion and Analysis. Total net sales increased 6% to $416,161 (in millions) during 2026."
)

// tenKReader 返回一份 10-K：Item 1A 和 Item 7 的原文。
func tenKReader(risk, mdna string) func(context.Context, string) (*edgar.FilingDocument, error) {
	return func(ctx context.Context, raw string) (*edgar.FilingDocument, error) {
		ref, err := edgar.ParseFilingURL(raw)
		if err != nil {
			return nil, err
		}
		return &edgar.FilingDocument{FilingRef: *ref, Form: "10-K", FilingDate: "2026-10-30", ReportDate: "2026-09-26", URL: tenKURL,
			Sections: []edgar.DocumentSection{{Label: "Item 1A", URL: tenKURL, Text: risk}, {Label: "Item 7", URL: tenKURL, Text: mdna}}}, ctx.Err()
	}
}

func TestFilingSummaryTenK(t *testing.T) {
	data, _ := json.Marshal(map[string]any{"sections": []FilingSection{
		{Text: "公司依赖亚洲的零部件供应商，关税可能推高成本。", Refs: []string{"Item 1A"}},
		{Text: "2026 财年总净销售额增长 6%，达到 4,161.61 亿美元。", Refs: []string{"Item 7"}},
	}})
	fake := &Fake{Replies: []FakeReply{FakeText(string(data))}}
	s := FilingSummarizer{Client: fake, Model: Model{Name: "test"}, CacheDir: t.TempDir(), Read: tenKReader(riskText, mdnaText)}
	result, err := s.Summarize(context.Background(), tenKURL)
	require.NoError(t, err)
	require.Equal(t, "10-K", result.Form)
	require.Equal(t, "2026-09-26", result.ReportDate)
	require.Equal(t, TenKPromptVersion, result.PromptVersion)
	require.True(t, result.Check.OK(), "以百万为单位的金额换算成亿也能找到来源：%v", result.Check)
	require.Equal(t, []FilingSource{{"Item 1A", tenKURL}, {"Item 7", tenKURL}}, result.Sources)
	require.Contains(t, result.Notes[0], "只摘要 10-K 的 Item 1A")
	require.Len(t, fake.Requests, 1)
	req := fake.Requests[0]
	require.Equal(t, tenKPrompt, req.System, "10-K 用自己的提示词")
	require.Contains(t, req.Messages[0].Text, "财年截止日")

	// 缓存按结果的申报类型核对提示词：改了 8-K 的提示词，10-K 的缓存照常命中；改了 10-K 的就不命中。
	s.Read = nil
	old := eightKKind.hash
	eightKKind.hash = "changed"
	cached, err := s.Summarize(context.Background(), tenKURL)
	eightKKind.hash = old
	require.NoError(t, err)
	require.True(t, cached.Cached)
	require.Equal(t, tenKKind.hash, cached.PromptHash)
	old = tenKKind.hash
	tenKKind.hash = "changed"
	_, err = s.Summarize(context.Background(), tenKURL)
	tenKKind.hash = old
	require.ErrorContains(t, err, "没有设置读取申报原文的方法", "提示词变了，不用旧缓存")

	// 不存在的章节按 10-K 的叫法报错。
	s = FilingSummarizer{Client: &Fake{Replies: []FakeReply{filingReply("事项", "Item 8")}}, Read: tenKReader(riskText, mdnaText)}
	_, err = s.Summarize(context.Background(), tenKURL)
	require.ErrorContains(t, err, "不存在的章节")
}

func TestFilingSummaryTenKChunks(t *testing.T) {
	// 10-K 的块比 8-K 大：Item 1A 加 Item 7 超过 8-K 的块大小也只分一块。
	short := FilingSummarizer{Client: &Fake{Replies: []FakeReply{filingReply("公司依赖亚洲的零部件供应商。", "Item 1A")}},
		Read: tenKReader(riskText+strings.Repeat("a", filingChunkRunes), mdnaText)}
	_, err := short.Summarize(context.Background(), tenKURL)
	require.NoError(t, err)
	require.Len(t, short.Client.(*Fake).Requests, 1)

	client := &routeClient{route: func(text string) FakeReply {
		switch {
		case strings.Contains(text, "合并分块摘要"):
			return FakeText(`{"sections":[{"text":"公司依赖亚洲的零部件供应商。","refs":["Item 1A"]},{"text":"总净销售额增长 6%。","refs":["Item 7"]}]}`)
		case strings.Contains(text, `"label":"Item 7"`):
			return filingReply("总净销售额增长 6%。", "Item 7")
		default:
			return filingReply("公司依赖亚洲的零部件供应商。", "Item 1A")
		}
	}}
	s := FilingSummarizer{Client: client, Read: tenKReader(riskText+strings.Repeat("a", tenKChunkRunes), mdnaText)}
	result, err := s.Summarize(context.Background(), tenKURL)
	require.NoError(t, err)
	require.Len(t, client.Requests, 3, "长 Item 1A 分成两块，再合并一次")
	require.Contains(t, client.Requests[2].Messages[0].Text, "风险因素和管理层讨论与分析分开成段")
	require.Len(t, result.Sections, 2)
}

func TestFilingSummaryUnsupportedForm(t *testing.T) {
	// Read 返回其他类型时报错，不套用 8-K 的提示词。
	fake := &Fake{}
	s := FilingSummarizer{Client: fake, Read: func(ctx context.Context, raw string) (*edgar.FilingDocument, error) {
		doc, err := tenKReader(riskText, mdnaText)(ctx, raw)
		doc.Form = "10-Q"
		return doc, err
	}}
	_, err := s.Summarize(context.Background(), tenKURL)
	require.ErrorIs(t, err, ErrFilingSummary)
	require.ErrorContains(t, err, "不支持摘要 10-Q 申报")
	require.Empty(t, fake.Requests)
}
