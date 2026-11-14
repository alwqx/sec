package llm

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/alwqx/sec/provider/cninfo"
)

//go:embed prompts/announcement_summary.md
var announcementPrompt string

const AnnouncementPromptVersion = "cninfo-summary-v1"

const announcementChunkRunes = 12000

var ErrSummary = errors.New("公告摘要生成失败")

// SummarySection 的引用由代码验证后渲染，不相信模型在正文中生成的页码或链接。
type SummarySection struct {
	Text  string `json:"text"`
	Pages []int  `json:"pages"`
}

type AnnouncementSummary struct {
	DocumentID    string           `json:"document_id"`
	SourceURL     string           `json:"source_url"`
	AsOf          string           `json:"as_of"`
	PageCount     int              `json:"page_count"`
	Sections      []SummarySection `json:"sections"`
	Text          string           `json:"text"`
	Notes         []string         `json:"notes"`
	Check         Check            `json:"check"`
	Disclaimer    string           `json:"disclaimer"`
	Model         string           `json:"model"`
	Profile       string           `json:"profile"`
	PromptVersion string           `json:"prompt_version"`
	Cached        bool             `json:"cached"`
	CreatedAt     time.Time        `json:"created_at"`
}

// AnnouncementSummarizer 为 CLI 和 Desktop 共用。Read 仅用于测试注入，不接受前端提交正文。
type AnnouncementSummarizer struct {
	Client   Client
	Model    Model
	CacheDir string
	Read     func(context.Context, string) (*cninfo.Document, error)
}

func (s *AnnouncementSummarizer) Summarize(ctx context.Context, rawURL string) (*AnnouncementSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	meta, err := cninfo.DocumentURL(rawURL)
	if err != nil {
		return nil, err
	}
	key := s.summaryKey(meta)
	if cached := readSummaryCache(s.CacheDir, key, func(r *AnnouncementSummary) bool {
		return r.Text != "" && len(r.Sections) > 0 && r.DocumentID == meta.ID && r.SourceURL == meta.URL && r.PromptVersion == AnnouncementPromptVersion && r.Check.OK()
	}); cached != nil {
		cached.Profile, cached.Cached = s.Model.Profile, true
		return cached, nil
	}
	read := s.Read
	if read == nil {
		read = cninfo.ReadDocument
	}
	doc, err := read(ctx, meta.URL)
	if err != nil {
		return nil, err
	}
	if doc == nil || len(doc.Pages) == 0 || len(doc.Pages) > cninfo.MaxDocumentPages {
		return nil, fmt.Errorf("%w：公告没有可用页", ErrSummary)
	}
	d := summaryDoc[cninfo.DocumentPage, int]{
		call:  summaryCall{client: s.Client, model: s.Model, system: announcementPrompt, err: ErrSummary, what: "公告"},
		pages: true, refName: "页码", refWhat: " PDF 页码",
		chunkInput: func(chunk []cninfo.DocumentPage) (any, []int) {
			var pages []int
			for _, page := range chunk {
				pages = append(pages, page.Number)
			}
			return struct {
				Task  string                `json:"task"`
				AsOf  string                `json:"as_of"`
				Pages []cninfo.DocumentPage `json:"pages"`
			}{"分块摘要；披露日期不是报告期", meta.AsOf, chunk}, pages
		},
		mergeInput: func(sections []citedJSON[int]) any {
			return struct {
				Task     string           `json:"task"`
				AsOf     string           `json:"as_of"`
				Sections []citedJSON[int] `json:"sections"`
			}{"合并分块摘要，保留关键事实、条件、风险和页码引用", meta.AsOf, sections}
		},
		chunkLimit: sectionLimit{4, 400}, mergeLimit: sectionLimit{12, 800},
		source: func(n int) []string {
			var texts []string
			for _, page := range doc.Pages {
				if page.Number == n {
					texts = append(texts, summaryNumberSource(page.Text))
				}
			}
			return texts
		},
		cite: func(pages []int) string {
			refs := make([]string, len(pages))
			for i, n := range pages {
				refs[i] = strconv.Itoa(n)
			}
			return "PDF 第 " + strings.Join(refs, "、") + " 页"
		},
	}
	cited, model, err := d.summarize(ctx, chunkAnnouncement(doc.Pages))
	if err != nil {
		return nil, err
	}
	result := &AnnouncementSummary{
		DocumentID: meta.ID, SourceURL: meta.URL, AsOf: meta.AsOf, PageCount: len(doc.Pages), Sections: []SummarySection{},
		Notes:      []string{"页码从 PDF 首页起计，与正文印刷页码可能不同；披露日期来自巨潮 PDF 路径，不是报告期。", "摘要基于可提取的文本，不解析图片；请核对原文中的表格、图示和口径说明。"},
		Disclaimer: defaultDisclaimer, Model: model, Profile: s.Model.Profile, PromptVersion: AnnouncementPromptVersion, CreatedAt: time.Now(),
	}
	for _, section := range cited {
		result.Sections = append(result.Sections, SummarySection{Text: section.Text, Pages: section.Refs})
	}
	for _, page := range doc.Pages {
		if strings.TrimSpace(page.Text) == "" {
			result.Notes = append(result.Notes, fmt.Sprintf("PDF 第 %d 页未提取到文本，未参与摘要。", page.Number))
		}
	}
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

// chunkAnnouncement 保留物理页码。
func chunkAnnouncement(pages []cninfo.DocumentPage) [][]cninfo.DocumentPage {
	return chunkText(pages, announcementChunkRunes, func(p cninfo.DocumentPage) string { return p.Text }, func(p cninfo.DocumentPage, text string) cninfo.DocumentPage {
		return cninfo.DocumentPage{Number: p.Number, Text: text}
	})
}

func (s *AnnouncementSummarizer) summaryKey(doc *cninfo.Document) string {
	return summaryCacheKey(s.CacheDir, doc.ID, doc.URL, s.Model.ID(), AnnouncementPromptVersion, announcementPrompt)
}
