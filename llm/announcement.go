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
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/alwqx/sec/provider/cninfo"
	"github.com/alwqx/sec/watchlist"
)

//go:embed prompts/announcement_summary.md
var announcementPrompt string

const AnnouncementPromptVersion = "cninfo-summary-v1"

const announcementChunkRunes = 12000

// announcementConcurrency 是同时请求模型的分块数上限。
const announcementConcurrency = 4

// announcementMaxTokens 是分块和合并请求的输出上限。思考模型的推理也计入上限，8000 不够长公告的一块。
// 按实测 deepseek-v4-pro 约 90 token/s，16000 约需 3 分钟，在 5 分钟的请求超时以内；更慢的模型可能先超时。
const announcementMaxTokens = 16000

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
	if cached := s.readSummaryCache(key, meta); cached != nil {
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
	chunks := chunkAnnouncement(doc.Pages)
	if len(chunks) == 0 || len(chunks) > 40 {
		return nil, fmt.Errorf("%w：公告为空或超过 40 个文本块", ErrSummary)
	}
	parts, model, err := s.summarizeChunks(ctx, meta.AsOf, chunks)
	if err != nil {
		return nil, err
	}
	var sections []SummarySection
	for _, part := range parts {
		sections = append(sections, part...)
	}
	if len(chunks) > 1 {
		allowed := map[int]bool{}
		for _, part := range sections {
			for _, page := range part.Pages {
				allowed[page] = true
			}
		}
		input := struct {
			Task     string           `json:"task"`
			AsOf     string           `json:"as_of"`
			Sections []SummarySection `json:"sections"`
		}{"合并分块摘要，保留关键事实、条件、风险和页码引用", meta.AsOf, sections}
		merged, mergeModel, err := s.summarizePart(ctx, input, allowed, true, 12, 800)
		if err != nil {
			return nil, err
		}
		sections = merged
		if mergeModel != "" {
			model = mergeModel
		}
	}
	result := &AnnouncementSummary{
		DocumentID: meta.ID, SourceURL: meta.URL, AsOf: meta.AsOf, PageCount: len(doc.Pages), Sections: sections,
		Notes:      []string{"页码从 PDF 首页起计，与正文印刷页码可能不同；披露日期来自巨潮 PDF 路径，不是报告期。", "摘要基于可提取的文本，不解析图片；请核对原文中的表格、图示和口径说明。"},
		Disclaimer: defaultDisclaimer, Model: model, Profile: s.Model.Profile, PromptVersion: AnnouncementPromptVersion, CreatedAt: time.Now(),
	}
	for _, page := range doc.Pages {
		if strings.TrimSpace(page.Text) == "" {
			result.Notes = append(result.Notes, fmt.Sprintf("PDF 第 %d 页未提取到文本，未参与摘要。", page.Number))
		}
	}
	result.Text, result.Check = checkSummary(sections, doc.Pages)
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

// summarizeChunks 并发摘要各块，结果按块的顺序返回，避免长文档串行调用超过整体时限。
// 任一块失败就取消其余请求。模型名取第一个返回了模型名的块，没有则用配置的模型名。
func (s *AnnouncementSummarizer) summarizeChunks(ctx context.Context, asOf string, chunks [][]cninfo.DocumentPage) ([][]SummarySection, string, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	parts := make([][]SummarySection, len(chunks))
	models := make([]string, len(chunks))
	sem := make(chan struct{}, announcementConcurrency)
	var wg sync.WaitGroup
	var once sync.Once
	var firstErr error
	for i, chunk := range chunks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				once.Do(func() { firstErr = ctx.Err() })
				return
			}
			input := struct {
				Task  string                `json:"task"`
				AsOf  string                `json:"as_of"`
				Pages []cninfo.DocumentPage `json:"pages"`
			}{"分块摘要；披露日期不是报告期", asOf, chunk}
			allowed := map[int]bool{}
			for _, page := range chunk {
				allowed[page.Number] = true
			}
			part, model, err := s.summarizePart(ctx, input, allowed, false, 4, 400)
			if err != nil {
				once.Do(func() { firstErr = err; cancel() })
				return
			}
			parts[i], models[i] = part, model
		}()
	}
	wg.Wait()
	if firstErr != nil {
		return nil, "", firstErr
	}
	model := s.Model.Name
	for _, m := range models {
		if m != "" {
			model = m
			break
		}
	}
	return parts, model, nil
}

// chunkAnnouncement 保留物理页码；单页太长时按 rune 分开，避免切坏中文字符。
func chunkAnnouncement(pages []cninfo.DocumentPage) [][]cninfo.DocumentPage {
	var chunks [][]cninfo.DocumentPage
	var chunk []cninfo.DocumentPage
	size := 0
	for _, page := range pages {
		runes := []rune(strings.TrimSpace(page.Text))
		for len(runes) > 0 {
			if size == announcementChunkRunes {
				chunks, chunk, size = append(chunks, chunk), nil, 0
			}
			n := min(len(runes), announcementChunkRunes-size)
			chunk = append(chunk, cninfo.DocumentPage{Number: page.Number, Text: string(runes[:n])})
			runes, size = runes[n:], size+n
		}
	}
	if len(chunk) > 0 {
		chunks = append(chunks, chunk)
	}
	return chunks
}

// summarizePart 请求一次分块摘要或合并（merge），按停止原因给出可操作的错误。
func (s *AnnouncementSummarizer) summarizePart(ctx context.Context, input any, allowed map[int]bool, merge bool, maxSections, maxRunes int) ([]SummarySection, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	data, err := json.Marshal(input)
	if err != nil {
		return nil, "", err
	}
	resp, err := s.Client.Chat(ctx, Request{System: announcementPrompt, Messages: []Message{{Role: RoleUser, Text: string(data)}}, MaxTokens: announcementMaxTokens})
	if err != nil {
		return nil, "", err
	}
	switch {
	case resp == nil:
		return nil, "", fmt.Errorf("%w：模型没有返回回复，请重试或更换模型", ErrSummary)
	case resp.StopReason == StopMaxTokens && merge:
		// 合并只输入分块摘要，被截断通常是分块太多，不是推理太长。
		return nil, "", fmt.Errorf("%w：合并分块摘要时回复超过 %d token 的输出上限被截断，公告可能太长，请重试或更换模型", ErrSummary, announcementMaxTokens)
	case resp.StopReason == StopMaxTokens:
		return nil, "", fmt.Errorf("%w：分块摘要时回复超过 %d token 的输出上限被截断（思考模型的推理也计入上限），请重试，或换用非思考模型或其他配置", ErrSummary, announcementMaxTokens)
	case len(resp.Message.ToolCalls) > 0:
		return nil, "", fmt.Errorf("%w：模型调用了工具，公告摘要不提供工具，请重试或更换模型", ErrSummary)
	case resp.StopReason != StopEnd:
		reason := ""
		if resp.RawStopReason != "" {
			reason = "（停止原因 " + resp.RawStopReason + "）"
		}
		return nil, "", fmt.Errorf("%w：模型拒绝或未正常结束%s，请重试或更换模型", ErrSummary, reason)
	}
	text := stripJSONFence(resp.Message.Text)
	var output struct {
		Sections []SummarySection `json:"sections"`
	}
	if json.Unmarshal([]byte(text), &output) != nil || len(output.Sections) < 1 || len(output.Sections) > maxSections {
		return nil, "", fmt.Errorf("%w：模型没有返回带页码的结构化摘要，请重试或更换模型", ErrSummary)
	}
	for i := range output.Sections {
		section := &output.Sections[i]
		section.Text = strings.TrimSpace(section.Text)
		slices.Sort(section.Pages)
		section.Pages = slices.Compact(section.Pages)
		if section.Text == "" || utf8.RuneCountInString(section.Text) > maxRunes || len(section.Pages) == 0 || len(section.Pages) > len(allowed) {
			return nil, "", fmt.Errorf("%w：段落为空、过长或缺少有效页码", ErrSummary)
		}
		for _, page := range section.Pages {
			if !allowed[page] {
				return nil, "", fmt.Errorf("%w：模型引用了输入中不存在的 PDF 页码", ErrSummary)
			}
		}
	}
	return output.Sections, resp.Model, nil
}

// checkSummary 每段只与其引用的原文页核对，不能用别处恰好相同的数字给错误引用背书。
func checkSummary(sections []SummarySection, pages []cninfo.DocumentPage) (string, Check) {
	check := Check{Unsourced: []NumberRef{}, AdviceTerms: []TermRef{}}
	var text strings.Builder
	for i, section := range sections {
		if i > 0 {
			text.WriteString("\n\n")
		}
		offset := utf8.RuneCountInString(text.String())
		var sources []string
		var refs []string
		for _, n := range section.Pages {
			refs = append(refs, strconv.Itoa(n))
			for _, page := range pages {
				if page.Number == n {
					sources = append(sources, summaryNumberSource(page.Text))
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
		fmt.Fprintf(&text, "%s（PDF 第 %s 页）", section.Text, strings.Join(refs, "、"))
	}
	return text.String(), check
}

// PDF 原文是自然语言，逗号可能是千分位；仅合并完整的三位分组，不改变共用 JSON 数字核对的规则。
func summaryNumberSource(text string) string {
	matches := mergeThousands(text, numberPattern.FindAllStringIndex(text, -1))
	var out strings.Builder
	last := 0
	for _, loc := range matches {
		out.WriteString(text[last:loc[0]])
		out.WriteString(strings.ReplaceAll(text[loc[0]:loc[1]], ",", ""))
		last = loc[1]
	}
	out.WriteString(text[last:])
	return out.String()
}

func (s *AnnouncementSummarizer) summaryKey(doc *cninfo.Document) string {
	if s.CacheDir == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{doc.ID, doc.URL, s.Model.ID(), AnnouncementPromptVersion, announcementPrompt}, "\x00")))
	return hex.EncodeToString(sum[:])
}

func (s *AnnouncementSummarizer) readSummaryCache(key string, doc *cninfo.Document) *AnnouncementSummary {
	if key == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(s.CacheDir, key+".json"))
	if err != nil {
		return nil
	}
	var result AnnouncementSummary
	if json.Unmarshal(data, &result) != nil || result.Text == "" || len(result.Sections) == 0 || result.DocumentID != doc.ID || result.SourceURL != doc.URL || result.PromptVersion != AnnouncementPromptVersion || !result.Check.OK() {
		return nil
	}
	result.Profile, result.Cached = s.Model.Profile, true
	return &result
}

// stripJSONFence 去掉部分兼容模型加的 Markdown 围栏（``` 或 ```json，大小写和 CRLF 均可），
// 不允许围栏以外的文字。
func stripJSONFence(text string) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	if !strings.HasPrefix(text, "```") || !strings.HasSuffix(text, "```") || len(text) < 6 {
		return text
	}
	body := strings.TrimSuffix(text[3:], "```")
	newline := strings.IndexByte(body, '\n')
	if newline < 0 {
		return text
	}
	if lang := strings.TrimSpace(body[:newline]); lang != "" && !strings.EqualFold(lang, "json") {
		return text
	}
	return strings.TrimSpace(body[newline+1:])
}
