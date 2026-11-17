package llm

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/alwqx/sec/provider/cninfo"
)

//go:embed prompts/prospectus_ask.md
var prospectusPrompt string

const ProspectusPromptVersion = "cninfo-prospectus-ask-v1"

const (
	// prospectusBudgetRunes 是发给模型的页面总字数，一次请求就能回答，不分块、不合并。招股书一页约 1000 字，大约 30 页。
	// 中文约 0.6 token 一字（实测 31141 字为 17905 token），加上提示词和回答约 2 万多 token，本地模型需要至少 32k 上下文。
	prospectusBudgetRunes = 36000
	prospectusMaxPages    = 30
	// maxQuestionRunes 是问题的字数上限。
	maxQuestionRunes = 200
	// unitCarryPages 是没有单位说明的页最多往前找几页单位说明，覆盖跨好几页的表格。
	unitCarryPages = 3
	// prospectusPagesVersion 随 PDF 文本提取的改动更新，旧的页面缓存不再使用。
	prospectusPagesVersion = "cninfo-prospectus-pages-v1"
)

var ErrProspectusAsk = errors.New("招股书问答失败")

// ErrNoRelatedPages 表示招股书里没有一页包含问题里的词，要换个问法，重试或换模型没有用。
var ErrNoRelatedPages = fmt.Errorf("%w：招股书里没有和问题相关的页", ErrProspectusAsk)

var prospectusPromptHash = promptHash(ProspectusPromptVersion, prospectusPrompt)

// ProspectusAnswer 是对一份招股说明书的一次问答。Pages 是按问题挑出来发给模型的页，回答每段引用其中的页码。
type ProspectusAnswer struct {
	DocumentID    string           `json:"document_id"`
	SourceURL     string           `json:"source_url"`
	AsOf          string           `json:"as_of"` // 披露日期，来自巨潮 PDF 路径
	Question      string           `json:"question"`
	PageCount     int              `json:"page_count"`
	Pages         []int            `json:"pages"`
	Sections      []SummarySection `json:"sections"`
	Text          string           `json:"text"`
	Notes         []string         `json:"notes"`
	Check         Check            `json:"check"`
	Disclaimer    string           `json:"disclaimer"`
	Model         string           `json:"model"`
	Profile       string           `json:"profile"`
	PromptVersion string           `json:"prompt_version"`
	PromptHash    string           `json:"prompt_hash"`
	Cached        bool             `json:"cached"`
	CreatedAt     time.Time        `json:"created_at"`
}

// ProspectusAnswerer 回答关于一份招股说明书的问题，CLI（sec ipo ask）使用。
type ProspectusAnswerer struct {
	Client   Client
	Model    Model
	CacheDir string
	// Read 读取 PDF，默认 cninfo.ReadProspectus。
	Read func(context.Context, string) (*cninfo.Document, error)
}

// unitDeclaration 匹配会放大数字的单位说明，如「单位：万元」「（亿元）」，和 sourceScales 认的单位一致；
// 「（元）」「单位：%」「（股）」不放大数字，不算。
var unitDeclaration = regexp.MustCompile(`(单位|金额单位)[:：](人民币|美元|港元|港币)?(百万元|万元|亿元|千元)|[（(](人民币)?(百万元|万元|亿元|千元)[)）]`)

// unitHints 返回一页里的单位说明，用空格分开；没有时返回空字符串。PDF 常把单位拆到两行（「（万\n元）」），先去掉空白。
func unitHints(text string) string {
	return strings.Join(unitDeclaration.FindAllString(strings.Join(strings.Fields(text), ""), -1), " ")
}

// NormalizeQuestion 合并问题里的空白，检查问题能不能用来挑页：为空、太长或没有可以查找的词（「请问是什么」）时报错。
// CLI 在查巨潮、下载 PDF 之前调用，问题不能用时不发请求。
func NormalizeQuestion(question string) (string, error) {
	question = strings.Join(strings.Fields(question), " ")
	switch n := utf8.RuneCountInString(question); {
	case n == 0:
		return "", fmt.Errorf("%w：问题不能为空", ErrProspectusAsk)
	case n > maxQuestionRunes:
		return "", fmt.Errorf("%w：问题超过 %d 字，请缩短", ErrProspectusAsk, maxQuestionRunes)
	case len(questionTerms(question)) == 0:
		return "", fmt.Errorf("%w：问题里没有可以用来查找的词，请写明要问的内容，比如营业收入、募集资金、实际控制人", ErrProspectusAsk)
	}
	return question, nil
}

// Ask 回答关于 rawURL（巨潮的招股说明书 PDF）的问题：按问题里的词挑出最相关的约 30 页，一次请求模型回答，
// 每段引用 PDF 页码，数字只和引用的页核对。
func (a *ProspectusAnswerer) Ask(ctx context.Context, rawURL, question string) (*ProspectusAnswer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	question, err := NormalizeQuestion(question)
	if err != nil {
		return nil, err
	}
	meta, err := cninfo.DocumentURL(rawURL)
	if err != nil {
		return nil, err
	}
	key := summaryCacheKey(a.CacheDir, meta.ID, meta.URL, a.Model.ID(), "cninfo-prospectus-ask", question)
	if cached := readSummaryCache(a.CacheDir, key, func(r *ProspectusAnswer) bool {
		return r.Text != "" && r.DocumentID == meta.ID && r.Question == question && r.PromptHash == prospectusPromptHash && r.Check.OK()
	}); cached != nil {
		cached.Profile, cached.Cached = a.Model.Profile, true
		return cached, nil
	}
	doc, err := a.readPages(ctx, meta)
	if err != nil {
		return nil, err
	}
	selected := selectPages(doc.Pages, rankPages(doc.Pages, question), prospectusBudgetRunes, prospectusMaxPages)
	if len(selected) == 0 {
		return nil, fmt.Errorf("%w，请换个说法，比如用招股书里的用语（营业收入、募集资金、实际控制人）", ErrNoRelatedPages)
	}
	sources := pageNumberSources(doc.Pages, selected)
	d := summaryDoc[cninfo.DocumentPage, int]{
		call:  summaryCall{client: a.Client, model: a.Model, system: prospectusPrompt, err: ErrProspectusAsk, what: "招股书"},
		pages: true, refName: "页码", refWhat: " PDF 页码",
		chunkInput: func(chunk []cninfo.DocumentPage) (any, []int) {
			var pages []int
			for _, page := range chunk {
				pages = append(pages, page.Number)
			}
			return struct {
				Question string                `json:"question"`
				AsOf     string                `json:"as_of"`
				Pages    []cninfo.DocumentPage `json:"pages"`
			}{question, meta.AsOf, chunk}, pages
		},
		chunkLimit: sectionLimit{6, 600},
		source:     func(n int) []string { return []string{sources[n]} },
		cite:       CitePDFPages,
	}
	// 选出的页不超过一块，只请求一次，不合并。
	cited, model, err := d.summarize(ctx, [][]cninfo.DocumentPage{selected})
	if err != nil {
		return nil, err
	}
	result := &ProspectusAnswer{
		DocumentID: meta.ID, SourceURL: meta.URL, AsOf: meta.AsOf, Question: question, PageCount: len(doc.Pages), Sections: []SummarySection{},
		Notes: []string{
			fmt.Sprintf("回答只基于按问题里的词从 %d 页中挑出的 %d 页（总字数上限 %d），没有挑到的页不在回答范围内；问得宽泛（如「有哪些风险」）时可能只覆盖一部分，可以问得具体一些。", len(doc.Pages), len(selected), prospectusBudgetRunes),
			"页码从 PDF 首页起计，与正文印刷页码可能不同；披露日期来自巨潮 PDF 路径。回答基于可提取的文本，不解析图片，请核对原文中的表格和口径说明。",
		},
		Disclaimer: defaultDisclaimer, Model: model, Profile: a.Model.Profile, PromptVersion: ProspectusPromptVersion, PromptHash: prospectusPromptHash, CreatedAt: time.Now(),
	}
	for _, page := range selected {
		result.Pages = append(result.Pages, page.Number)
	}
	for _, section := range cited {
		result.Sections = append(result.Sections, SummarySection{Text: section.Text, Pages: section.Refs})
	}
	result.Text, result.Check = d.check(cited)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// 有核对警告的完整结果仍可查看，但不缓存。
	if result.Check.OK() {
		writeSummaryCache(a.CacheDir, key, result)
	}
	return result, nil
}

// pageNumberSources 返回选中的每页用来核对数字的来源：页的正文加上单位说明。单位说明去掉了空白，拆到两行的
// 「万\n元」也能用来换算。跨页的表格（「续表」）常只在第一页写单位，这一页没有单位说明时，带上前几页里最近的
// 单位说明（只带单位，不带那几页的数字）。
func pageNumberSources(pages, selected []cninfo.DocumentPage) map[int]string {
	byNumber := map[int]string{}
	for _, page := range pages {
		byNumber[page.Number] = page.Text
	}
	sources := map[int]string{}
	for _, page := range selected {
		hints := unitHints(page.Text)
		for back := 1; hints == "" && back <= unitCarryPages; back++ {
			hints = unitHints(byNumber[page.Number-back])
		}
		sources[page.Number] = summaryNumberSource(page.Text) + "\n" + hints
	}
	return sources
}

// readPages 读取招股书的页。巨潮 finalpage 下的 PDF 不会改动，提取出的页按文档缓存，同一份招股书再问别的问题时
// 不再下载和解析几十 MB 的 PDF。
func (a *ProspectusAnswerer) readPages(ctx context.Context, meta *cninfo.Document) (*cninfo.Document, error) {
	key := summaryCacheKey(a.CacheDir, meta.ID, meta.URL, prospectusPagesVersion)
	if doc := readSummaryCache(a.CacheDir, key, func(d *cninfo.Document) bool {
		return d.ID == meta.ID && d.URL == meta.URL && len(d.Pages) > 0
	}); doc != nil {
		return doc, nil
	}
	read := a.Read
	if read == nil {
		read = cninfo.ReadProspectus
	}
	doc, err := read(ctx, meta.URL)
	if err != nil {
		return nil, err
	}
	if doc == nil || len(doc.Pages) == 0 {
		return nil, fmt.Errorf("%w：招股书没有可用页", ErrProspectusAsk)
	}
	writeSummaryCache(a.CacheDir, key, doc)
	return doc, nil
}
