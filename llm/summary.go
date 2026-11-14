package llm

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/alwqx/sec/watchlist"
)

// 公告和申报摘要共用的部分：分块、并发请求、按停止原因说明错误、去掉 Markdown 围栏。

// summaryConcurrency 是同时请求模型的分块数上限。
const summaryConcurrency = 4

// summaryMaxTokens 是分块和合并请求的输出上限。思考模型的推理也计入上限，8000 不够长公告的一块。
// 按实测 deepseek-v4-pro 约 90 token/s，16000 约需 3 分钟，在 5 分钟的请求超时以内；更慢的模型可能先超时。
const summaryMaxTokens = 16000

// summaryCall 是摘要的一次模型请求。
type summaryCall struct {
	client Client
	model  Model
	system string
	err    error  // 错误前缀，如 ErrSummary
	what   string // 文档名称，用于错误说明，如「公告」
}

// chat 请求一次分块摘要或合并（merge），按停止原因给出可操作的错误；返回去掉围栏的回复正文和实际模型名。
func (c summaryCall) chat(ctx context.Context, input any, merge bool) (string, string, error) {
	if err := ctx.Err(); err != nil {
		return "", "", err
	}
	data, err := json.Marshal(input)
	if err != nil {
		return "", "", err
	}
	resp, err := c.client.Chat(ctx, Request{System: c.system, Messages: []Message{{Role: RoleUser, Text: string(data)}}, MaxTokens: summaryMaxTokens})
	if err != nil {
		return "", "", err
	}
	switch {
	case resp == nil:
		return "", "", fmt.Errorf("%w：模型没有返回回复，请重试或更换模型", c.err)
	case resp.StopReason == StopMaxTokens:
		stage := "分块摘要"
		if merge {
			stage = "合并分块摘要"
		}
		return "", "", fmt.Errorf("%w：%s时回复超过 %d token 的输出上限被截断%s", c.err, stage, summaryMaxTokens, c.model.TruncationAdvice(resp, "摘要正文超出上限，"+c.what+"可能太长"))
	case len(resp.Message.ToolCalls) > 0:
		return "", "", fmt.Errorf("%w：模型调用了工具，%s摘要不提供工具，请重试或更换模型", c.err, c.what)
	case resp.StopReason != StopEnd:
		reason := ""
		if resp.RawStopReason != "" {
			reason = "（停止原因 " + resp.RawStopReason + "）"
		}
		return "", "", fmt.Errorf("%w：模型拒绝或未正常结束%s，请重试或更换模型", c.err, reason)
	}
	return stripJSONFence(resp.Message.Text), resp.Model, nil
}

// summarizeChunks 并发处理各块，结果按块的顺序返回，避免长文档串行调用超过整体时限。
// 任一块失败就取消其余请求。模型名取第一个返回了模型名的块，没有则用 fallback。
func summarizeChunks[C, R any](ctx context.Context, chunks []C, fallback string, run func(context.Context, C) (R, string, error)) ([]R, string, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	parts := make([]R, len(chunks))
	models := make([]string, len(chunks))
	sem := make(chan struct{}, summaryConcurrency)
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
			part, model, err := run(ctx, chunk)
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
	model := fallback
	for _, m := range models {
		if m != "" {
			model = m
			break
		}
	}
	return parts, model, nil
}

// chunkText 把带位置（页码、章节）的文本按 size 个字符分块，保留位置；单个位置太长时按 rune 分开，
// 避免切坏中文字符。text 取出文字，with 用一段文字生成同一位置的新元素。
func chunkText[T any](items []T, size int, text func(T) string, with func(T, string) T) [][]T {
	var chunks [][]T
	var chunk []T
	n := 0
	for _, item := range items {
		runes := []rune(strings.TrimSpace(text(item)))
		for len(runes) > 0 {
			if n == size {
				chunks, chunk, n = append(chunks, chunk), nil, 0
			}
			k := min(len(runes), size-n)
			chunk = append(chunk, with(item, string(runes[:k])))
			runes, n = runes[k:], n+k
		}
	}
	if len(chunk) > 0 {
		chunks = append(chunks, chunk)
	}
	return chunks
}

// summaryNumberSource 准备数字核对的来源：原文是自然语言，逗号可能是千分位；仅合并完整的三位分组，
// 不改变共用 JSON 数字核对的规则。
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

// maxSummaryChunks 是一份文档最多的文本块数。
const maxSummaryChunks = 40

// citedSection 是一段摘要和它引用的原文位置：公告是 PDF 页码（int），申报是节标签（string）。
// 引用由代码验证后渲染，不相信模型在正文中生成的页码或链接。
type citedSection[R cmp.Ordered] struct {
	Text string
	Refs []R
}

// citedJSON 是模型输出和合并输入里的一段：text 在前，引用按文档用 pages（公告）或 refs（申报）。
// 用结构体而不是 map 编解码：解码时键名不分大小写，编码时字段顺序固定。
type citedJSON[R cmp.Ordered] struct {
	Text  string `json:"text"`
	Pages []R    `json:"pages,omitempty"`
	Refs  []R    `json:"refs,omitempty"`
}

// sectionLimit 是摘要的段数和每段字数上限。
type sectionLimit struct{ sections, runes int }

// summaryDoc 描述一种文档怎么摘要，C 是分块的元素（公告的页、申报的节），R 是引用的类型。
type summaryDoc[C any, R cmp.Ordered] struct {
	call summaryCall
	// pages 为 true 时引用写在 pages 字段（公告），否则写在 refs 字段（申报）。
	pages bool
	// refName 是错误说明里引用的叫法（页码、引用），refWhat 是引用指向的东西（PDF 页码、事项或附件）。
	refName, refWhat string
	// order 是一段里引用的排列顺序，为空时按值排序。
	order func(a, b R) int
	// chunkInput 返回一块的模型输入和这块可以引用的位置；mergeInput 用分块摘要生成合并的模型输入。
	chunkInput func([]C) (any, []R)
	mergeInput func(sections []citedJSON[R]) any
	chunkLimit sectionLimit
	mergeLimit sectionLimit
	// source 返回一个引用位置的原文，用于数字核对；cite 把一段的引用写成正文后面括号里的文字。
	source func(R) []string
	cite   func([]R) string
}

// summarize 并发摘要各块，多块时再合并；合并只能引用分块摘要引用过的位置，即使别的位置在原文里真实存在。
// 返回段落和实际模型名。
func (d summaryDoc[C, R]) summarize(ctx context.Context, chunks [][]C) ([]citedSection[R], string, error) {
	if len(chunks) == 0 || len(chunks) > maxSummaryChunks {
		return nil, "", fmt.Errorf("%w：%s为空或超过 %d 个文本块", d.call.err, d.call.what, maxSummaryChunks)
	}
	parts, model, err := summarizeChunks(ctx, chunks, d.call.model.Name, func(ctx context.Context, chunk []C) ([]citedSection[R], string, error) {
		input, refs := d.chunkInput(chunk)
		allowed := map[R]bool{}
		for _, ref := range refs {
			allowed[ref] = true
		}
		return d.part(ctx, input, allowed, false, d.chunkLimit)
	})
	if err != nil {
		return nil, "", err
	}
	var sections []citedSection[R]
	for _, part := range parts {
		sections = append(sections, part...)
	}
	if len(chunks) == 1 {
		return sections, model, nil
	}
	allowed := map[R]bool{}
	for _, section := range sections {
		for _, ref := range section.Refs {
			allowed[ref] = true
		}
	}
	merged, mergeModel, err := d.part(ctx, d.mergeInput(d.encode(sections)), allowed, true, d.mergeLimit)
	if err != nil {
		return nil, "", err
	}
	if mergeModel != "" {
		model = mergeModel
	}
	return merged, model, nil
}

// encode 把段落写成模型输出的格式，合并时交给模型。
func (d summaryDoc[C, R]) encode(sections []citedSection[R]) []citedJSON[R] {
	out := make([]citedJSON[R], 0, len(sections))
	for _, s := range sections {
		section := citedJSON[R]{Text: s.Text}
		if d.pages {
			section.Pages = s.Refs
		} else {
			section.Refs = s.Refs
		}
		out = append(out, section)
	}
	return out
}

// part 请求一次分块摘要或合并（merge），校验段落和引用；引用去重后按 order 排列。
func (d summaryDoc[C, R]) part(ctx context.Context, input any, allowed map[R]bool, merge bool, limit sectionLimit) ([]citedSection[R], string, error) {
	text, model, err := d.call.chat(ctx, input, merge)
	if err != nil {
		return nil, "", err
	}
	var output struct {
		Sections []citedJSON[R] `json:"sections"`
	}
	if json.Unmarshal([]byte(text), &output) != nil || len(output.Sections) < 1 || len(output.Sections) > limit.sections {
		return nil, "", fmt.Errorf("%w：模型没有返回带%s的结构化摘要，请重试或更换模型", d.call.err, d.refName)
	}
	order := d.order
	if order == nil {
		order = cmp.Compare[R]
	}
	sections := make([]citedSection[R], len(output.Sections))
	for i, raw := range output.Sections {
		section := &sections[i]
		section.Text, section.Refs = raw.Text, raw.Refs
		if d.pages {
			section.Refs = raw.Pages
		}
		// 模型可能在节标签前后多带空格，去掉后再和输入比较。
		if labels, ok := any(section.Refs).([]string); ok {
			for j := range labels {
				labels[j] = strings.TrimSpace(labels[j])
			}
		}
		section.Text = strings.TrimSpace(section.Text)
		slices.SortFunc(section.Refs, order)
		section.Refs = slices.Compact(section.Refs)
		switch n := utf8.RuneCountInString(section.Text); {
		case n == 0:
			return nil, "", fmt.Errorf("%w：模型返回了空段落，请重试或更换模型", d.call.err)
		case n > limit.runes:
			return nil, "", fmt.Errorf("%w：模型返回的段落有 %d 字，超过 %d 字的上限，请重试或更换模型", d.call.err, n, limit.runes)
		case len(section.Refs) == 0:
			return nil, "", fmt.Errorf("%w：段落缺少有效%s，请重试或更换模型", d.call.err, d.refName)
		}
		for _, ref := range section.Refs {
			if !allowed[ref] {
				return nil, "", fmt.Errorf("%w：模型引用了输入中不存在的%s", d.call.err, d.refWhat)
			}
		}
	}
	return sections, model, nil
}

// check 逐段核对数字和建议用语，返回带引用的全文。每段只与其引用的原文核对，不能用别处恰好相同的数字给错误引用背书。
func (d summaryDoc[C, R]) check(sections []citedSection[R]) (string, Check) {
	check := Check{Unsourced: []NumberRef{}, AdviceTerms: []TermRef{}}
	var text strings.Builder
	offset := 0 // text 已有的字符数，逐段累加，不每段重新数一遍
	for i, section := range sections {
		if i > 0 {
			text.WriteString("\n\n")
			offset += 2
		}
		var sources []string
		for _, ref := range section.Refs {
			sources = append(sources, d.source(ref)...)
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
		cited := fmt.Sprintf("%s（%s）", section.Text, d.cite(section.Refs))
		text.WriteString(cited)
		offset += utf8.RuneCountInString(cited)
	}
	return text.String(), check
}

// summaryCacheKey 是摘要缓存的文件名，parts 是文档标识、模型和提示词；dir 为空时不缓存，返回空字符串。
func summaryCacheKey(dir string, parts ...string) string {
	if dir == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

// readSummaryCache 读取缓存的摘要，valid 检查是否属于这份文档和这一版提示词；读不到或不合格时返回 nil，重新生成。
func readSummaryCache[T any](dir, key string, valid func(*T) bool) *T {
	if key == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(dir, key+".json"))
	if err != nil {
		return nil
	}
	var result T
	if json.Unmarshal(data, &result) != nil || !valid(&result) {
		return nil
	}
	return &result
}

// writeSummaryCache 写入缓存。调用方只缓存完整、正常结束且核对无警告的结果；失败、截断和取消都不写入。
func writeSummaryCache(dir, key string, result any) {
	if key == "" {
		return
	}
	if data, err := json.MarshalIndent(result, "", "  "); err == nil {
		_ = watchlist.AtomicWrite(filepath.Join(dir, key+".json"), data)
	}
}
