package llm

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/alwqx/sec/provider/edgar"
	"github.com/alwqx/sec/utils"
)

//go:embed prompts/risk_changes.md
var riskPrompt string

const RiskPromptVersion = "edgar-risk-changes-v1"

const (
	// riskChunkRunes 是每块变化的字符数上限，和 10-K 摘要一样约 1 万 token。
	riskChunkRunes = 40000
	// riskExcerptRunes 是每条变化每段正文发给模型的字数上限。新增、删除的风险因素正文常有几千字，
	// 开头的部分足够说明讲的是什么风险；改写只发两年正文里不同的句子。
	riskExcerptRunes = 4000
)

var ErrRiskExplain = errors.New("风险因素变化解读生成失败")

// RiskSection 的引用是变化的标签（如 新增 1、改写 3），对应对比结果里的序号，由代码验证后渲染。
type RiskSection struct {
	Text string   `json:"text"`
	Refs []string `json:"refs"`
}

// RiskExplanation 是一份 10-K 风险因素变化的解读。Changes 是确定性的对比结果，解读的引用指向其中的序号。
type RiskExplanation struct {
	Changes       *edgar.RiskChanges `json:"changes"`
	Sections      []RiskSection      `json:"sections"` // 没有新增、删除和改写时为空，不请求模型
	Text          string             `json:"text"`
	Notes         []string           `json:"notes"`
	Check         Check              `json:"check"`
	Disclaimer    string             `json:"disclaimer"`
	Model         string             `json:"model"`
	Profile       string             `json:"profile"`
	PromptVersion string             `json:"prompt_version"`
	PromptHash    string             `json:"prompt_hash"`
	Cached        bool               `json:"cached"`
	CreatedAt     time.Time          `json:"created_at"`
}

// RiskExplainer 用模型解释 10-K 风险因素的逐年变化。变化由 edgar.CompareRiskFactors 确定性地找出，
// 模型只解释，不找差异。
type RiskExplainer struct {
	Client   Client
	Model    Model
	CacheDir string
	// Read 读取两年的风险因素对比，一般是 (*edgar.Client).RiskChanges。没有默认值：EDGAR 要求请求带联系邮箱。
	Read func(context.Context, string) (*edgar.RiskChanges, error)
}

// riskChange 是发给模型的一条变化。
type riskChange struct {
	Label       string `json:"label"`
	Kind        string `json:"kind"`
	Category    string `json:"category,omitempty"`
	Title       string `json:"title,omitempty"`
	Text        string `json:"text,omitempty"`
	OldTitle    string `json:"old_title,omitempty"`
	NewTitle    string `json:"new_title,omitempty"`
	AddedText   string `json:"added_text,omitempty"`
	RemovedText string `json:"removed_text,omitempty"`
}

// size 是这条变化发给模型的字数，用来分块。
func (c riskChange) size() int {
	n := 0
	for _, s := range []string{c.Category, c.Title, c.Text, c.OldTitle, c.NewTitle, c.AddedText, c.RemovedText} {
		n += utf8.RuneCountInString(s)
	}
	return n
}

// source 是数字核对的来源：发给模型的全部文字。
func (c riskChange) source() string {
	return strings.Join([]string{c.Category, c.Title, c.Text, c.OldTitle, c.NewTitle, c.AddedText, c.RemovedText}, "\n")
}

// riskInputVersion 是组装模型输入的版本：正文截取的字数、改写时找不同句子的方法等改了要改这个值。
const riskInputVersion = "1"

// riskPromptHash 记在结果里，读缓存时核对。除了提示词，也包含对比规则和模型输入的版本：
// 它们改了，缓存的对比结果和解读的引用就和现在 --risks 列出的不一致。
var riskPromptHash = promptHash(RiskPromptVersion+"\x00"+edgar.RiskComparisonVersion+"\x00"+riskInputVersion, riskPrompt)

// Explain 解读 rawURL 所在 10-K 和上一年 10-K 的风险因素变化。rawURL 是该申报目录下任意文件的地址。
func (e *RiskExplainer) Explain(ctx context.Context, rawURL string) (*RiskExplanation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ref, err := edgar.ParseFilingURL(rawURL)
	if err != nil {
		return nil, err
	}
	key := e.cacheKey(ref)
	if cached := readSummaryCache(e.CacheDir, key, func(r *RiskExplanation) bool {
		return r.Changes != nil && r.Changes.Current.Accession == ref.Accession && r.PromptHash == riskPromptHash && r.Text != "" && r.Check.OK()
	}); cached != nil {
		cached.Profile, cached.Cached = e.Model.Profile, true
		return cached, nil
	}
	if e.Read == nil {
		return nil, fmt.Errorf("%w：没有设置读取风险因素对比的方法", ErrRiskExplain)
	}
	changes, err := e.Read(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	result := &RiskExplanation{
		Changes: changes, Sections: []RiskSection{},
		Notes: []string{
			"解读只基于对比找到的新增、删除和改写，改写只看两年正文里不同的句子；小标题相同的风险因素即使正文有改动也不在解读范围内。新增、删除和改写的正文较长时只发送开头的部分。",
		},
		Check:      Check{Unsourced: []NumberRef{}, AdviceTerms: []TermRef{}},
		Disclaimer: defaultDisclaimer, Profile: e.Model.Profile, PromptVersion: RiskPromptVersion, PromptHash: riskPromptHash, CreatedAt: time.Now(),
	}
	entries := riskEntries(changes)
	if len(entries) == 0 {
		result.Notes = append(result.Notes, "两年的风险因素小标题相同，没有需要解读的变化，没有请求模型。")
		return result, nil
	}
	order, sources := map[string]int{}, map[string]string{}
	for i, entry := range entries {
		order[entry.Label], sources[entry.Label] = i, filingNumberSource(entry.source())
	}
	// 两份 10-K 的日期和条数也发给了模型，每段都可以用（如「2026 财年由 27 条增至 28 条」）。
	cur, prev := riskFilingInput(changes.Current), riskFilingInput(changes.Previous)
	meta := fmt.Sprintf("%s %s %d %s %s %d", cur.FilingDate, cur.ReportDate, cur.Count, prev.FilingDate, prev.ReportDate, prev.Count)
	d := summaryDoc[riskChange, string]{
		call:    summaryCall{client: e.Client, model: e.Model, system: riskPrompt, err: ErrRiskExplain, what: "风险因素变化"},
		refName: "引用", refWhat: "变化",
		order: func(a, b string) int { return order[a] - order[b] },
		chunkInput: func(chunk []riskChange) (any, []string) {
			var labels []string
			for _, entry := range chunk {
				labels = append(labels, entry.Label)
			}
			return struct {
				Task     string           `json:"task"`
				Current  edgar.RiskFiling `json:"current"`
				Previous edgar.RiskFiling `json:"previous"`
				Changes  []riskChange     `json:"changes"`
			}{"分块解读；current 是今年的 10-K，previous 是上一年的，report_date 是财年截止日", cur, prev, chunk}, labels
		},
		mergeInput: func(sections []citedJSON[string]) any {
			return struct {
				Task     string              `json:"task"`
				Sections []citedJSON[string] `json:"sections"`
			}{"合并分块解读，按主题保留实质变化和引用，措辞调整合并简述", sections}
		},
		// 一块常有十几条改写，模型常一条一段，提示词要求最多 8 段，上限留出余量。
		chunkLimit: sectionLimit{12, 800}, mergeLimit: sectionLimit{12, 800},
		source:       func(label string) []string { return []string{sources[label], meta} },
		cite:         func(labels []string) string { return strings.Join(labels, "、") },
		normalizeRef: normalizeRiskLabel,
		mask:         func(text string) string { return maskRiskLabels(text, order) },
	}
	cited, model, err := d.summarize(ctx, packRiskChanges(entries, riskChunkRunes))
	if err != nil {
		return nil, err
	}
	for _, section := range cited {
		result.Sections = append(result.Sections, RiskSection{Text: section.Text, Refs: section.Refs})
	}
	result.Model = model
	result.Text, result.Check = d.check(cited)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// 有核对警告的完整结果仍可查看，但不缓存。
	if result.Check.OK() {
		writeSummaryCache(e.CacheDir, key, result)
	}
	return result, nil
}

// riskLabel 匹配正文里写出的变化标签，如「改写13」「改写 7」。
var riskLabel = regexp.MustCompile(`(新增|删除|改写)\s*(\d+)`)

// normalizeRiskLabel 把模型写的「改写3」「改写  3」规范成输入里的「改写 3」，其他写法原样返回，由引用校验拒绝。
func normalizeRiskLabel(ref string) string {
	if m := riskLabel.FindStringSubmatch(ref); m != nil && m[0] == ref {
		return m[1] + " " + m[2]
	}
	return ref
}

// maskRiskLabels 把正文里写出的变化标签的序号换成同样长度的 #：提示词要求引用写在 refs 里，模型仍可能在正文里
// 提到标签，序号不是需要核对来源的数字。只换输入里真有的标签，后面紧跟单位（如「新增 3 条」「新增 50 亿美元」）的
// 是数量或金额，照常核对。
func maskRiskLabels(text string, labels map[string]int) string {
	var out strings.Builder
	last := 0
	for _, loc := range riskLabel.FindAllStringSubmatchIndex(text, -1) {
		start, end := loc[0], loc[1]
		label := text[loc[2]:loc[3]] + " " + text[loc[4]:loc[5]]
		if _, ok := labels[label]; !ok || startsWithUnit(text[end:]) {
			continue
		}
		out.WriteString(text[last:start])
		out.WriteString(text[start:loc[4]] + strings.Repeat("#", loc[5]-loc[4]))
		last = end
	}
	out.WriteString(text[last:])
	return out.String()
}

// startsWithUnit 判断数字后面（可以隔着空格）是不是量词或单位。
func startsWithUnit(rest string) bool {
	rest = strings.TrimLeft(rest, " ")
	for _, unit := range []string{"条", "项", "个", "%", "％", "亿", "万", "年", "月", "日", "家", "名", "倍", "."} {
		if strings.HasPrefix(rest, unit) {
			return true
		}
	}
	return false
}

// riskFilingInput 只保留发给模型的字段，不带地址和申报编号。
func riskFilingInput(f edgar.RiskFiling) edgar.RiskFiling {
	return edgar.RiskFiling{Form: f.Form, FilingDate: f.FilingDate, ReportDate: f.ReportDate, Count: f.Count}
}

// riskEntries 把对比结果转成发给模型的变化，标签的序号和 CLI 列表的序号一致。
func riskEntries(c *edgar.RiskChanges) []riskChange {
	var entries []riskChange
	for i, r := range c.Added {
		entries = append(entries, riskChange{Label: "新增 " + strconv.Itoa(i+1), Kind: "added", Category: r.Category, Title: r.Title, Text: truncateRunes(r.Text, riskExcerptRunes)})
	}
	for i, r := range c.Removed {
		entries = append(entries, riskChange{Label: "删除 " + strconv.Itoa(i+1), Kind: "removed", Category: r.Category, Title: r.Title, Text: truncateRunes(r.Text, riskExcerptRunes)})
	}
	for i, r := range c.Revised {
		added, removed := sentenceDiff(r.Old.Text, r.New.Text)
		entries = append(entries, riskChange{Label: "改写 " + strconv.Itoa(i+1), Kind: "revised", Category: r.New.Category, OldTitle: r.Old.Title, NewTitle: r.New.Title,
			AddedText: truncateRunes(added, riskExcerptRunes), RemovedText: truncateRunes(removed, riskExcerptRunes)})
	}
	return entries
}

// packRiskChanges 按顺序把变化装进不超过 size 字的块，一条变化不拆开（每条有字数上限，不会超过一块）。
func packRiskChanges(entries []riskChange, size int) [][]riskChange {
	var chunks [][]riskChange
	var chunk []riskChange
	n := 0
	for _, entry := range entries {
		if k := entry.size(); len(chunk) > 0 && n+k > size {
			chunks, chunk, n = append(chunks, chunk), nil, 0
		}
		chunk = append(chunk, entry)
		n += entry.size()
	}
	if len(chunk) > 0 {
		chunks = append(chunks, chunk)
	}
	return chunks
}

// sentenceDiff 返回新正文里新出现的句子和旧正文里没有了的句子，按原文顺序，句子比较时不计大小写、空白和引号。
func sentenceDiff(old, new string) (added, removed string) {
	oldSentences, newSentences := sentences(old), sentences(new)
	seen := func(list []string) map[string]bool {
		set := map[string]bool{}
		for _, s := range list {
			set[sentenceKey(s)] = true
		}
		return set
	}
	inOld, inNew := seen(oldSentences), seen(newSentences)
	var a, r []string
	for _, s := range newSentences {
		if !inOld[sentenceKey(s)] {
			a = append(a, s)
		}
	}
	for _, s := range oldSentences {
		if !inNew[sentenceKey(s)] {
			r = append(r, s)
		}
	}
	return strings.Join(a, " "), strings.Join(r, " ")
}

// sentences 按行和句末标点（后面是空格和大写字母或数字）切分英文正文。缩写（如 U.S. Government）可能被切开，
// 两年切法相同，比较结果不受影响。
func sentences(text string) []string {
	var out []string
	for line := range strings.SplitSeq(text, "\n") {
		runes := []rune(line)
		start := 0
		for i := 0; i < len(runes)-2; i++ {
			if strings.ContainsRune(".?!", runes[i]) && runes[i+1] == ' ' && (unicode.IsUpper(runes[i+2]) || unicode.IsDigit(runes[i+2])) {
				if s := strings.TrimSpace(string(runes[start : i+1])); s != "" {
					out = append(out, s)
				}
				start = i + 2
			}
		}
		if s := strings.TrimSpace(string(runes[start:])); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func sentenceKey(s string) string {
	s = strings.NewReplacer("’", "'", "‘", "'", "“", `"`, "”", `"`).Replace(strings.ToLower(s))
	return strings.Join(strings.Fields(s), " ")
}

// truncateRunes 截取前 n 个字符，截断时在末尾说明，模型不会把截断的正文当成完整的。
func truncateRunes(text string, n int) string {
	if truncated := utils.TruncateRunes(text, n); truncated != text {
		return truncated + "（截断）"
	}
	return text
}

// cacheKey 是缓存的文件名：申报编号和目录页地址确定了今年的 10-K，上一年的 10-K 和对比结果由它确定地得出。
// 提示词记在结果的 PromptHash 里核对。
func (e *RiskExplainer) cacheKey(ref *edgar.FilingRef) string {
	return summaryCacheKey(e.CacheDir, ref.Accession, ref.IndexURL, e.Model.ID(), "edgar-risk-changes")
}
