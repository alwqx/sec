package llm

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
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

//go:embed prompts/tenk_summary.md
var tenKPrompt string

// 提示词版本也记下发给模型的原文怎么取：取法改了（如 v2 加上银行年报里的 MD&A 和 MD&A 的字数上限，v3 按标题找花旗的风险因素）
// 要改版本，缓存随之失效。
const (
	FilingPromptVersion = "edgar-8k-summary-v1"
	TenKPromptVersion   = "edgar-10k-summary-v3"
)

// filingChunkRunes 是 8-K 每块的字符数。申报原文是英文，同样字数的 token 比中文少，块可以比公告大。
const filingChunkRunes = 20000

// tenKChunkRunes 是 10-K 每块的字符数，约 1 万 token。Item 1A 和 Item 7 合计常有 10 万到 17 万字，
// 按 8-K 的块大小要分 6 到 9 块、超过并发数分两轮请求；块大一些，大多数 10-K 一轮请求加一次合并就能完成。
const tenKChunkRunes = 40000

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
	SourceURL     string          `json:"source_url"`  // 主文件
	IndexURL      string          `json:"index_url"`   // 申报目录，包含全部附件
	AsOf          string          `json:"as_of"`       // 申报日
	ReportDate    string          `json:"report_date"` // 8-K 是最早事项的发生日，10-K 是财年截止日
	Sources       []FilingSource  `json:"sources"`
	Sections      []FilingSection `json:"sections"`
	Text          string          `json:"text"`
	Notes         []string        `json:"notes"`
	Check         Check           `json:"check"`
	Disclaimer    string          `json:"disclaimer"`
	Model         string          `json:"model"`
	Profile       string          `json:"profile"`
	PromptVersion string          `json:"prompt_version"`
	PromptHash    string          `json:"prompt_hash"` // 提示词版本和内容的 SHA-256，读缓存时核对
	Cached        bool            `json:"cached"`
	CreatedAt     time.Time       `json:"created_at"`
}

// FilingSummarizer 生成 8-K 事项摘要和 10-K 风险因素、管理层讨论与分析摘要，CLI 和 Desktop 共用。
type FilingSummarizer struct {
	Client   Client
	Model    Model
	CacheDir string
	// Read 读取申报原文，一般是 (*edgar.Client).ReadFiling 或只读 8-K 的 Read8K。没有默认值：EDGAR 要求请求带联系邮箱。
	Read func(context.Context, string) (*edgar.FilingDocument, error)
}

// filingKind 是一类申报的摘要设置。
type filingKind struct {
	prompt, version      string
	hash                 string // 提示词版本和内容的摘要，存进结果，读缓存时核对
	chunkRunes           int
	chunkTask, mergeTask string
	chunkLimit           sectionLimit
	refWhat              string // 引用指向的东西，用于错误说明
	notes                []string
}

var (
	eightKKind = filingKind{
		prompt: filingPrompt, version: FilingPromptVersion, hash: promptHash(FilingPromptVersion, filingPrompt), chunkRunes: filingChunkRunes,
		chunkTask:  "分块摘要；filing_date 是申报日，report_date 是最早事项的发生日",
		mergeTask:  "合并分块摘要，按事项保留关键事实、条件、风险和引用",
		chunkLimit: sectionLimit{4, filingSectionRunes},
		refWhat:    "事项或附件",
		notes: []string{
			"Item 是 8-K 正文的事项，Exhibit 是附件；封面、签名和 Item 9.01 附件清单不参与摘要。8-K 的报告期是最早事项的发生日。",
			"摘要基于可提取的文字，不解析图片；请核对原文中的表格、口径（如 GAAP 与 non-GAAP）和前瞻性陈述的限定。",
		},
	}
	tenKKind = filingKind{
		prompt: tenKPrompt, version: TenKPromptVersion, hash: promptHash(TenKPromptVersion, tenKPrompt), chunkRunes: tenKChunkRunes,
		chunkTask: "分块摘要；一节可能分成多块，只概括本块的内容；filing_date 是申报日，report_date 是财年截止日",
		mergeTask: "合并分块摘要，风险因素和管理层讨论与分析分开成段，保留关键事实、原因和引用",
		// 一块 4 万字的风险因素或 MD&A 内容密集，提示词要求最多 6 段、每段 400 字，模型常略超，上限留出余量。
		chunkLimit: sectionLimit{6, 800},
		refWhat:    "章节",
		notes: []string{
			"只摘要 10-K 的 Item 1A 风险因素和 Item 7 管理层讨论与分析；业务介绍、财务报表、附注和其他 Item 不参与摘要。10-K 的报告期是财年截止日。",
			"风险因素按原文顺序概括，不代表风险大小；摘要基于可提取的文字，不解析图片，请核对原文中的表格、口径（如 GAAP 与 non-GAAP）和前瞻性陈述的限定。",
		},
	}
)

func promptHash(version, prompt string) string {
	sum := sha256.Sum256([]byte(version + "\x00" + prompt))
	return hex.EncodeToString(sum[:])
}

// kindOf 按申报类型（含修订版）选择摘要设置；edgar.Summarizable 以外的类型返回 false，不套用别的类型的提示词。
func kindOf(form string) (filingKind, bool) {
	switch edgar.BaseForm(form) {
	case "8-K":
		return eightKKind, true
	case "10-K", "10-KT":
		return tenKKind, true
	}
	return filingKind{}, false
}

// filingPart 是发给模型的一节原文，不带地址。
type filingPart struct {
	Label string `json:"label"`
	Text  string `json:"text"`
}

// Summarize 摘要一份 8-K 或 10-K（由 Read 支持的类型决定）。rawURL 是该申报目录下任意文件的地址。
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
		return r.Text != "" && len(r.Sections) > 0 && r.DocumentID == ref.Accession && r.IndexURL == ref.IndexURL && r.Check.OK() && currentPrompt(r)
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
	kind, ok := kindOf(doc.Form)
	if !ok {
		return nil, fmt.Errorf("%w：不支持摘要 %s 申报", ErrFilingSummary, doc.Form)
	}
	var parts []filingPart
	order := map[string]int{}
	sources := map[string][]string{}
	for i, section := range doc.Sections {
		parts = append(parts, filingPart{section.Label, section.Text})
		order[section.Label] = i
	}
	d := summaryDoc[filingPart, string]{
		call:    summaryCall{client: s.Client, model: s.Model, system: kind.prompt, err: ErrFilingSummary, what: "申报"},
		refName: "引用", refWhat: kind.refWhat,
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
			}{kind.chunkTask, doc.Form, doc.FilingDate, doc.ReportDate, chunk}, labels
		},
		mergeInput: func(sections []citedJSON[string]) any {
			return struct {
				Task       string              `json:"task"`
				Form       string              `json:"form"`
				FilingDate string              `json:"filing_date"`
				Sections   []citedJSON[string] `json:"sections"`
			}{kind.mergeTask, doc.Form, doc.FilingDate, sections}
		},
		chunkLimit: kind.chunkLimit, mergeLimit: sectionLimit{12, 800},
		// 一节可能被多段引用，长章节的数字来源只准备一次；check 逐段顺序调用，不需要加锁。
		source: func(label string) []string {
			if texts, ok := sources[label]; ok {
				return texts
			}
			var texts []string
			for _, section := range doc.Sections {
				if section.Label == label {
					texts = append(texts, filingNumberSource(section.Text))
				}
			}
			sources[label] = texts
			return texts
		},
		cite: func(labels []string) string { return strings.Join(labels, "、") },
	}
	chunks := chunkText(parts, kind.chunkRunes, func(p filingPart) string { return p.Text }, func(p filingPart, text string) filingPart {
		return filingPart{p.Label, text}
	})
	cited, model, err := d.summarize(ctx, chunks)
	if err != nil {
		return nil, err
	}
	result := &FilingSummary{
		DocumentID: doc.Accession, Form: doc.Form, SourceURL: doc.URL, IndexURL: doc.IndexURL, AsOf: doc.FilingDate, ReportDate: doc.ReportDate,
		Sources: []FilingSource{}, Sections: []FilingSection{},
		Notes:      slices.Clone(kind.notes),
		Disclaimer: defaultDisclaimer, Model: model, Profile: s.Model.Profile, PromptVersion: kind.version, PromptHash: kind.hash, CreatedAt: time.Now(),
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

// currentPrompt 判断缓存的结果是不是用这类申报当前的提示词生成的。
func currentPrompt(r *FilingSummary) bool {
	kind, ok := kindOf(r.Form)
	return ok && r.PromptHash == kind.hash
}

// summaryKey 是缓存的文件名。读到原文才知道申报类型，所以不含提示词：提示词记在结果的 PromptHash 里，
// 读缓存时按结果的类型核对，修改一类申报的提示词不会让另一类的缓存失效。
func (s *FilingSummarizer) summaryKey(ref *edgar.FilingRef) string {
	return summaryCacheKey(s.CacheDir, ref.Accession, ref.IndexURL, s.Model.ID(), "edgar-filing-summary")
}
