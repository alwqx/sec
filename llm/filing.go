package llm

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/alwqx/sec/provider/edgar"
	"github.com/alwqx/sec/watchlist"
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
	if cached := s.readSummaryCache(key, ref); cached != nil {
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
	chunks := chunkText(parts, filingChunkRunes, func(p filingPart) string { return p.Text }, func(p filingPart, text string) filingPart {
		return filingPart{p.Label, text}
	})
	if len(chunks) == 0 || len(chunks) > 40 {
		return nil, fmt.Errorf("%w：申报为空或超过 40 个文本块", ErrFilingSummary)
	}
	summaries, model, err := summarizeChunks(ctx, chunks, s.Model.Name, func(ctx context.Context, chunk []filingPart) ([]FilingSection, string, error) {
		input := struct {
			Task       string       `json:"task"`
			Form       string       `json:"form"`
			FilingDate string       `json:"filing_date"`
			ReportDate string       `json:"report_date"`
			Sections   []filingPart `json:"sections"`
		}{"分块摘要；filing_date 是申报日，report_date 是最早事项的发生日", doc.Form, doc.FilingDate, doc.ReportDate, chunk}
		allowed := map[string]bool{}
		for _, part := range chunk {
			allowed[part.Label] = true
		}
		return s.summarizePart(ctx, input, allowed, order, false, 4, filingSectionRunes)
	})
	if err != nil {
		return nil, err
	}
	var sections []FilingSection
	for _, part := range summaries {
		sections = append(sections, part...)
	}
	if len(chunks) > 1 {
		allowed := map[string]bool{}
		for _, section := range sections {
			for _, ref := range section.Refs {
				allowed[ref] = true
			}
		}
		input := struct {
			Task       string          `json:"task"`
			Form       string          `json:"form"`
			FilingDate string          `json:"filing_date"`
			Sections   []FilingSection `json:"sections"`
		}{"合并分块摘要，按事项保留关键事实、条件、风险和引用", doc.Form, doc.FilingDate, sections}
		merged, mergeModel, err := s.summarizePart(ctx, input, allowed, order, true, 12, 800)
		if err != nil {
			return nil, err
		}
		sections = merged
		if mergeModel != "" {
			model = mergeModel
		}
	}
	result := &FilingSummary{
		DocumentID: doc.Accession, Form: doc.Form, SourceURL: doc.URL, IndexURL: doc.IndexURL, AsOf: doc.FilingDate, ReportDate: doc.ReportDate,
		Sources: []FilingSource{}, Sections: sections,
		Notes: []string{
			"Item 是 8-K 正文的事项，Exhibit 是附件；封面、签名和 Item 9.01 附件清单不参与摘要。8-K 的报告期是最早事项的发生日。",
			"摘要基于可提取的文字，不解析图片；请核对原文中的表格、口径（如 GAAP 与 non-GAAP）和前瞻性陈述的限定。",
		},
		Disclaimer: defaultDisclaimer, Model: model, Profile: s.Model.Profile, PromptVersion: FilingPromptVersion, CreatedAt: time.Now(),
	}
	for _, section := range doc.Sections {
		result.Sources = append(result.Sources, FilingSource{section.Label, section.URL})
	}
	result.Notes = append(result.Notes, doc.Notes...)
	result.Text, result.Check = checkFilingSummary(sections, doc.Sections)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// 有核对警告的完整结果仍可查看，但不缓存；失败、截断和取消都不会写入缓存。
	if result.Check.OK() && key != "" {
		data, err := json.MarshalIndent(result, "", "  ")
		if err == nil {
			_ = watchlist.AtomicWrite(filepath.Join(s.CacheDir, key+".json"), data)
		}
	}
	return result, nil
}

// summarizePart 请求一次分块摘要或合并（merge），校验段落和引用；引用按原文顺序排列。
func (s *FilingSummarizer) summarizePart(ctx context.Context, input any, allowed map[string]bool, order map[string]int, merge bool, maxSections, maxRunes int) ([]FilingSection, string, error) {
	text, model, err := summaryCall{client: s.Client, model: s.Model, system: filingPrompt, err: ErrFilingSummary, what: "申报"}.chat(ctx, input, merge)
	if err != nil {
		return nil, "", err
	}
	var output struct {
		Sections []FilingSection `json:"sections"`
	}
	if json.Unmarshal([]byte(text), &output) != nil || len(output.Sections) < 1 || len(output.Sections) > maxSections {
		return nil, "", fmt.Errorf("%w：模型没有返回带引用的结构化摘要，请重试或更换模型", ErrFilingSummary)
	}
	for i := range output.Sections {
		section := &output.Sections[i]
		section.Text = strings.TrimSpace(section.Text)
		for j := range section.Refs {
			section.Refs[j] = strings.TrimSpace(section.Refs[j])
		}
		slices.SortFunc(section.Refs, func(a, b string) int { return order[a] - order[b] })
		section.Refs = slices.Compact(section.Refs)
		switch n := utf8.RuneCountInString(section.Text); {
		case n == 0:
			return nil, "", fmt.Errorf("%w：模型返回了空段落，请重试或更换模型", ErrFilingSummary)
		case n > maxRunes:
			return nil, "", fmt.Errorf("%w：模型返回的段落有 %d 字，超过 %d 字的上限，请重试或更换模型", ErrFilingSummary, n, maxRunes)
		case len(section.Refs) == 0:
			return nil, "", fmt.Errorf("%w：段落缺少有效引用，请重试或更换模型", ErrFilingSummary)
		}
		for _, ref := range section.Refs {
			if !allowed[ref] {
				return nil, "", fmt.Errorf("%w：模型引用了输入中不存在的事项或附件", ErrFilingSummary)
			}
		}
	}
	return output.Sections, model, nil
}

// checkFilingSummary 每段只与其引用的原文节核对，不能用别处恰好相同的数字给错误引用背书。
func checkFilingSummary(sections []FilingSection, parts []edgar.DocumentSection) (string, Check) {
	check := Check{Unsourced: []NumberRef{}, AdviceTerms: []TermRef{}}
	var text strings.Builder
	for i, section := range sections {
		if i > 0 {
			text.WriteString("\n\n")
		}
		offset := utf8.RuneCountInString(text.String())
		var sources []string
		for _, ref := range section.Refs {
			for _, part := range parts {
				if part.Label == ref {
					sources = append(sources, filingNumberSource(part.Text))
				}
			}
		}
		c := Verify(section.Text, sources...)
		check.Numbers += c.Numbers
		for _, n := range c.Unsourced {
			n.Start, n.End = n.Start+offset, n.End+offset
			check.Unsourced = append(check.Unsourced, n)
		}
		for _, term := range c.AdviceTerms {
			term.Start, term.End = term.Start+offset, term.End+offset
			check.AdviceTerms = append(check.AdviceTerms, term)
		}
		fmt.Fprintf(&text, "%s（%s）", section.Text, strings.Join(section.Refs, "、"))
	}
	return text.String(), check
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
	if s.CacheDir == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{ref.Accession, ref.IndexURL, s.Model.ID(), FilingPromptVersion, filingPrompt}, "\x00")))
	return hex.EncodeToString(sum[:])
}

func (s *FilingSummarizer) readSummaryCache(key string, ref *edgar.FilingRef) *FilingSummary {
	if key == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(s.CacheDir, key+".json"))
	if err != nil {
		return nil
	}
	var result FilingSummary
	if json.Unmarshal(data, &result) != nil || result.Text == "" || len(result.Sections) == 0 || result.DocumentID != ref.Accession || result.IndexURL != ref.IndexURL || result.PromptVersion != FilingPromptVersion || !result.Check.OK() {
		return nil
	}
	result.Profile, result.Cached = s.Model.Profile, true
	return &result
}
