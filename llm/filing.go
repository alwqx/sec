package llm

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/alwqx/sec/provider/edgar"
)

//go:embed prompts/filing_summary.md
var filingPrompt string

const FilingPromptVersion = "edgar-8k-summary-v1"

// filingChunkRunes 是每块的字符数。申报原文是英文，同样字数的 token 比中文少，块可以比公告大。
const filingChunkRunes = 20000

// filingSectionRunes 是分块摘要每段的字数上限。提示词要求 400 字，模型常把一个事项和它的附件写成一段、略超一点，
// 留出余量，不因为多写几个字让整次摘要失败。合并后每段上限 800 字。
const filingSectionRunes = 600

var ErrFilingSummary = errors.New("申报摘要生成失败")

// FilingSection 的引用是输入里的节标签（如 Item 2.02、Exhibit 99.1），由代码验证后渲染。
type FilingSection struct {
	Text string   `json:"text"`
	Refs []string `json:"refs"`
}

// FilingSource 是参与摘要的一节原文和它的地址。
type FilingSource struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

type FilingSummary struct {
	DocumentID    string          `json:"document_id"` // 申报编号，如 0000320193-26-000024
	Form          string          `json:"form"`
	SourceURL     string          `json:"source_url"` // 主文件
	IndexURL      string          `json:"index_url"`  // 申报目录，包含全部附件
	AsOf          string          `json:"as_of"`      // 申报日
	ReportDate    string          `json:"report_date"`
	Sources       []FilingSource  `json:"sources"`
	Sections      []FilingSection `json:"sections"`
	Text          string          `json:"text"`
	Notes         []string        `json:"notes"`
	Check         Check           `json:"check"`
	Disclaimer    string          `json:"disclaimer"`
	Model         string          `json:"model"`
	Profile       string          `json:"profile"`
	PromptVersion string          `json:"prompt_version"`
	Cached        bool            `json:"cached"`
	CreatedAt     time.Time       `json:"created_at"`
}

// FilingSummarizer 生成 8-K 事项摘要，CLI 和 Desktop 共用。
type FilingSummarizer struct {
	Client   Client
	Model    Model
	CacheDir string
	// Read 读取申报原文，一般是 (*edgar.Client).Read8K。没有默认值：EDGAR 要求请求带联系邮箱。
	Read func(context.Context, string) (*edgar.FilingDocument, error)
}

// filingPart 是发给模型的一节原文，不带地址。
type filingPart struct {
	Label string `json:"label"`
	Text  string `json:"text"`
}

// Summarize 摘要一份 8-K。rawURL 是该申报目录下任意文件的地址。
func (s *FilingSummarizer) Summarize(ctx context.Context, rawURL string) (*FilingSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ref, err := edgar.ParseFilingURL(rawURL)
	if err != nil {
		return nil, err
	}
	key := s.summaryKey(ref)
	if cached := readSummaryCache(s.CacheDir, key, func(r *FilingSummary) bool {
		return r.Text != "" && len(r.Sections) > 0 && r.DocumentID == ref.Accession && r.IndexURL == ref.IndexURL && r.PromptVersion == FilingPromptVersion && r.Check.OK()
	}); cached != nil {
		cached.Profile, cached.Cached = s.Model.Profile, true
		return cached, nil
	}
	if s.Read == nil {
		return nil, fmt.Errorf("%w：没有设置读取申报原文的方法", ErrFilingSummary)
	}
	doc, err := s.Read(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	if doc == nil || len(doc.Sections) == 0 {
		return nil, fmt.Errorf("%w：申报没有可摘要的正文", ErrFilingSummary)
	}
	var parts []filingPart
	order := map[string]int{}
	for i, section := range doc.Sections {
		parts = append(parts, filingPart{section.Label, section.Text})
		order[section.Label] = i
	}
	d := summaryDoc[filingPart, string]{
		call:    summaryCall{client: s.Client, model: s.Model, system: filingPrompt, err: ErrFilingSummary, what: "申报"},
		refName: "引用", refWhat: "事项或附件",
		order: func(a, b string) int { return order[a] - order[b] },
		chunkInput: func(chunk []filingPart) (any, []string) {
			var labels []string
			for _, part := range chunk {
				labels = append(labels, part.Label)
			}
			return struct {
				Task       string       `json:"task"`
				Form       string       `json:"form"`
				FilingDate string       `json:"filing_date"`
				ReportDate string       `json:"report_date"`
				Sections   []filingPart `json:"sections"`
			}{"分块摘要；filing_date 是申报日，report_date 是最早事项的发生日", doc.Form, doc.FilingDate, doc.ReportDate, chunk}, labels
		},
		mergeInput: func(sections []citedJSON[string]) any {
			return struct {
				Task       string              `json:"task"`
				Form       string              `json:"form"`
				FilingDate string              `json:"filing_date"`
				Sections   []citedJSON[string] `json:"sections"`
			}{"合并分块摘要，按事项保留关键事实、条件、风险和引用", doc.Form, doc.FilingDate, sections}
		},
		chunkLimit: sectionLimit{4, filingSectionRunes}, mergeLimit: sectionLimit{12, 800},
		source: func(label string) []string {
			var texts []string
			for _, section := range doc.Sections {
				if section.Label == label {
					texts = append(texts, filingNumberSource(section.Text))
				}
			}
			return texts
		},
		cite: func(labels []string) string { return strings.Join(labels, "、") },
	}
	chunks := chunkText(parts, filingChunkRunes, func(p filingPart) string { return p.Text }, func(p filingPart, text string) filingPart {
		return filingPart{p.Label, text}
	})
	cited, model, err := d.summarize(ctx, chunks)
	if err != nil {
		return nil, err
	}
	result := &FilingSummary{
		DocumentID: doc.Accession, Form: doc.Form, SourceURL: doc.URL, IndexURL: doc.IndexURL, AsOf: doc.FilingDate, ReportDate: doc.ReportDate,
		Sources: []FilingSource{}, Sections: []FilingSection{},
		Notes: []string{
			"Item 是 8-K 正文的事项，Exhibit 是附件；封面、签名和 Item 9.01 附件清单不参与摘要。8-K 的报告期是最早事项的发生日。",
			"摘要基于可提取的文字，不解析图片；请核对原文中的表格、口径（如 GAAP 与 non-GAAP）和前瞻性陈述的限定。",
		},
		Disclaimer: defaultDisclaimer, Model: model, Profile: s.Model.Profile, PromptVersion: FilingPromptVersion, CreatedAt: time.Now(),
	}
	for _, section := range cited {
		result.Sections = append(result.Sections, FilingSection{Text: section.Text, Refs: section.Refs})
	}
	for _, section := range doc.Sections {
		result.Sources = append(result.Sources, FilingSource{section.Label, section.URL})
	}
	result.Notes = append(result.Notes, doc.Notes...)
	result.Text, result.Check = d.check(cited)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// 有核对警告的完整结果仍可查看，但不缓存。
	if result.Check.OK() {
		writeSummaryCache(s.CacheDir, key, result)
	}
	return result, nil
}

// monthName 匹配后面跟着日或年的英文月份（如 November 2, 2026、Sept. 2026），小写的 may 是情态动词，不算。
var monthName = regexp.MustCompile(`\b(January|February|March|April|May|June|July|August|September|October|November|December|Jan|Feb|Mar|Apr|Jun|Jul|Aug|Sept|Sep|Oct|Nov|Dec)\b\.?\s+\d`)

var monthAbbr = []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}

// filingNumberSource 在 summaryNumberSource 之外补上英文月份对应的数字：原文写 November 2, 2026，
// 中文摘要写 2026 年 11 月 2 日，11 要能找到来源。
func filingNumberSource(text string) string {
	source := summaryNumberSource(text)
	var months []string
	for _, m := range monthName.FindAllStringSubmatch(text, -1) {
		months = append(months, strconv.Itoa(slices.Index(monthAbbr, m[1][:3])+1))
	}
	if len(months) == 0 {
		return source
	}
	slices.Sort(months)
	return source + "\n" + strings.Join(slices.Compact(months), " ")
}

func (s *FilingSummarizer) summaryKey(ref *edgar.FilingRef) string {
	return summaryCacheKey(s.CacheDir, ref.Accession, ref.IndexURL, s.Model.ID(), FilingPromptVersion, filingPrompt)
}
