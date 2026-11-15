package edgar

import (
	"cmp"
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// RiskFactor 是 10-K Item 1A 里的一条风险因素。
type RiskFactor struct {
	Category string `json:"category,omitempty"` // 所在的风险类别，如 Business Risks；没有类别标题时为空
	Title    string `json:"title"`
	Text     string `json:"-"` // 小标题下的正文，不含小标题；只给模型解读用，不输出
}

const (
	// minTitleWords 是风险因素小标题最少的词数，排除「Note.」这类加粗的短句。
	minTitleWords = 4
	// maxCategoryRunes 是风险类别标题最多的字数。
	maxCategoryRunes = 100
)

// riskTitle 返回一行开头的风险因素小标题：行首连续加粗或斜体、并且是一个完整句子的文字。小标题可以单独成段，
// 也可以和正文写在同一段（如微软的「We may experience supply problems. There are…」，句号有时不在加粗范围内）。
// 风险类别（Business Risks）和小标题下的分项（微软的 Competition in the technology sector）不是句子，不算。
func riskTitle(line docLine) (string, bool) {
	lead := line.lead
	if lead == "" || len(strings.Fields(lead)) < minTitleWords {
		return "", false
	}
	rest := strings.TrimSpace(line.text[min(len(lead), len(line.text)):])
	if !strings.HasPrefix(line.text, lead) {
		rest = ""
	}
	switch {
	case endsSentence(lead):
		return lead, true
	case strings.HasPrefix(rest, "."):
		return lead + ".", true
	}
	return "", false
}

// endsSentence 判断文字是否以句号或问号结尾，句末的右引号和右括号不算（如 a "trade war."）。
func endsSentence(text string) bool {
	text = strings.TrimRight(text, `"”’')]`)
	return strings.HasSuffix(text, ".") || strings.HasSuffix(text, "?")
}

// riskFactors 从 Item 1A 的行范围 [start, end) 取出风险因素：小标题、所在类别和小标题下的正文，跳过页眉页脚。
//
// 不同公司排版不同（Apple、特斯拉的类别加粗、小标题粗斜体，摩根大通都只加粗，微软小标题加粗、分项粗斜体），
// 所以只按两条规则判断：小标题是完整的句子；同一份 10-K 的小标题样式相同，取句子形式里最常见的样式（加粗、斜体或
// 粗斜体），排除样式不同的引言（特斯拉、摩根大通开头的斜体说明）。类别是整行加粗、斜体或带下划线、不是句子、
// 下一行就是小标题的短行（微软用带下划线的大写类别）。
func riskFactors(lines []docLine, scanned []tenKLine, start, end int) []RiskFactor {
	if start < 0 {
		return nil
	}
	type style struct{ bold, italic bool }
	titles := map[int]string{}
	count := map[style]int{}
	var order []style
	for i := start + 1; i < end; i++ {
		if scanned[i].furniture {
			continue
		}
		if title, ok := riskTitle(lines[i]); ok {
			titles[i] = title
			s := style{lines[i].bold, lines[i].italic}
			if count[s] == 0 {
				order = append(order, s)
			}
			count[s]++
		}
	}
	if len(order) == 0 {
		return nil
	}
	// 次数相同时取先出现的样式。
	main := order[0]
	for _, s := range order[1:] {
		if count[s] > count[main] {
			main = s
		}
	}
	for i := range titles {
		if (style{lines[i].bold, lines[i].italic}) != main {
			delete(titles, i)
		}
	}
	// nextLine 返回 i 之后第一个不是页眉页脚的行。
	nextLine := func(i int) int {
		for i++; i < end && scanned[i].furniture; i++ {
		}
		return i
	}
	var risks []RiskFactor
	var body []string
	category, current := "", -1 // current 是正在收集正文的风险因素，类别标题和它后面的引言不属于任何一条
	finish := func() {
		if current >= 0 {
			risks[current].Text = strings.Join(body, "\n")
		}
		current, body = -1, nil
	}
	for i := start + 1; i < end; i++ {
		line := lines[i]
		if scanned[i].furniture {
			continue
		}
		if title, ok := titles[i]; ok {
			finish()
			risks = append(risks, RiskFactor{Category: category, Title: title})
			current = len(risks) - 1
			// 和正文写在同一段时，小标题后面的部分是正文。
			if rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line.text, line.lead), ".")); rest != "" {
				body = append(body, rest)
			}
			continue
		}
		// 「Item 1A.」和「Risk Factors」分成两行时，第二行是章节标题，不是类别。
		isTitle := strings.EqualFold(strings.TrimRight(line.text, ".:"), "risk factors")
		if _, ok := titles[nextLine(i)]; ok && line.emphasized && !isTitle && !endsSentence(line.text) && utf8.RuneCountInString(line.text) <= maxCategoryRunes {
			finish()
			category = line.text
			continue
		}
		if current >= 0 {
			body = append(body, line.text)
		}
	}
	finish()
	return risks
}

// RiskRevision 是两年都有、小标题改了的一条风险因素。
type RiskRevision struct {
	Old             RiskFactor `json:"old"`
	New             RiskFactor `json:"new"`
	TitleSimilarity float64    `json:"title_similarity"` // 小标题的相似度，0 到 1
	TextSimilarity  float64    `json:"text_similarity"`  // 正文的相似度，0 到 1
}

// RiskComparison 是相邻两年 10-K 风险因素的对比。Added、Revised、Kept 按新一年的顺序，Removed 按旧一年的顺序。
type RiskComparison struct {
	Added   []RiskFactor   `json:"added"`
	Removed []RiskFactor   `json:"removed"`
	Revised []RiskRevision `json:"revised"`
	Kept    []RiskFactor   `json:"kept"` // 小标题相同（规范化后），正文可能有改动
}

const (
	// minTitleSimilarity 是只看小标题就算同一条风险的最低相似度。
	minTitleSimilarity = 0.6
	// minPairScore 是小标题和正文相似度的平均值的下限：小标题改写得多时（摩根大通 2026 年改写了大半），
	// 正文通常还保留很多原句。实测同一条风险的平均值在 0.33 以上，明显不同的风险在 0.2 以下。
	minPairScore = 0.3
)

// CompareRiskFactors 确定性地对比两年的风险因素，不调用模型：小标题规范化后相同的算保留；其余两两计算小标题的词
// 相似度（Dice 系数，去掉虚词和套话）和正文的相似度（连续三个词的 Dice 系数），小标题相似度不低于
// minTitleSimilarity、或两者的平均值不低于 minPairScore 的算改写，按平均值从高到低配对；剩下的是新增和删除。
func CompareRiskFactors(old, new []RiskFactor) RiskComparison {
	c := RiskComparison{Added: []RiskFactor{}, Removed: []RiskFactor{}, Revised: []RiskRevision{}, Kept: []RiskFactor{}}
	oldMatched := make([]int, len(old)) // 配对的另一年序号加 1，0 表示没有配对
	newMatched := make([]int, len(new))
	byTitle := map[string][]int{}
	for i, r := range old {
		key := normalizeTitle(r.Title)
		byTitle[key] = append(byTitle[key], i)
	}
	for j, r := range new {
		key := normalizeTitle(r.Title)
		if candidates := byTitle[key]; len(candidates) > 0 {
			i := candidates[0]
			byTitle[key] = candidates[1:]
			oldMatched[i], newMatched[j] = j+1, i+1
		}
	}
	type pair struct {
		i, j        int
		title, text float64
	}
	var pairs []pair
	oldWords, oldShingles := make([]map[string]bool, len(old)), make([]map[string]bool, len(old))
	for i, r := range old {
		oldWords[i], oldShingles[i] = titleWords(r.Title), shingles(r.Text)
	}
	for j, r := range new {
		if newMatched[j] != 0 {
			continue
		}
		words, text := titleWords(r.Title), shingles(r.Text)
		for i := range old {
			if oldMatched[i] != 0 {
				continue
			}
			p := pair{i, j, dice(oldWords[i], words), dice(oldShingles[i], text)}
			if p.title >= minTitleSimilarity || (p.title+p.text)/2 >= minPairScore {
				pairs = append(pairs, p)
			}
		}
	}
	// 平均值相同时保持生成的顺序（新一年在前、再按旧一年），结果是确定的。
	slices.SortStableFunc(pairs, func(a, b pair) int {
		return cmp.Compare(b.title+b.text, a.title+a.text)
	})
	revisions := make([]*pair, len(new))
	for k, p := range pairs {
		if oldMatched[p.i] == 0 && newMatched[p.j] == 0 {
			oldMatched[p.i], newMatched[p.j] = p.j+1, p.i+1
			revisions[p.j] = &pairs[k]
		}
	}
	round := func(x float64) float64 { return math.Round(x*100) / 100 }
	for j, r := range new {
		switch p := revisions[j]; {
		case newMatched[j] == 0:
			c.Added = append(c.Added, r)
		case p != nil:
			c.Revised = append(c.Revised, RiskRevision{Old: old[p.i], New: r, TitleSimilarity: round(p.title), TextSimilarity: round(p.text)})
		default:
			c.Kept = append(c.Kept, r)
		}
	}
	for i, r := range old {
		if oldMatched[i] == 0 {
			c.Removed = append(c.Removed, r)
		}
	}
	return c
}

// shingles 返回正文里连续三个词的组合（小写，只保留字母和数字），比较正文时对语序敏感，常见单词不会让两段不同的正文显得相似。
func shingles(text string) map[string]bool {
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	out := map[string]bool{}
	for i := 0; i+3 <= len(words); i++ {
		out[words[i]+" "+words[i+1]+" "+words[i+2]] = true
	}
	return out
}

// normalizeTitle 规范化小标题：小写，弯引号换成直引号，合并空白，去掉末尾的句号。
func normalizeTitle(title string) string {
	title = strings.NewReplacer("’", "'", "‘", "'", "“", `"`, "”", `"`).Replace(strings.ToLower(title))
	return strings.TrimRight(strings.Join(strings.Fields(title), " "), ".?")
}

// stopWords 是比较小标题时忽略的词：常见虚词，以及小标题末尾常见的后果套话（could materially adversely affect the
// Company's business, results of operations and financial condition）。Apple 2026 财年把很多小标题的套话删掉了，
// 不忽略它们时同一条风险的相似度不到 0.6。
var stopWords = map[string]bool{}

func init() {
	for w := range strings.FieldsSeq(`a an the and or of to in on for by with from as at its it our we us is are be been has have
		could may can will would might which that this these such other any all also not no if s company
		material materially adverse adversely affect affects affected affecting impact impacts impacted
		business businesses result results operations operating financial condition harm harmed
		significant significantly subject risk risks including time`) {
		stopWords[w] = true
	}
}

// titleWords 返回小标题里除虚词以外的词。
func titleWords(title string) map[string]bool {
	words := map[string]bool{}
	for _, w := range strings.FieldsFunc(normalizeTitle(title), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if !stopWords[w] {
			words[w] = true
		}
	}
	return words
}

// dice 是两组词的 Dice 系数：2|A∩B| / (|A|+|B|)。
func dice(a, b map[string]bool) float64 {
	if len(a)+len(b) == 0 {
		return 0
	}
	common := 0
	for w := range a {
		if b[w] {
			common++
		}
	}
	return 2 * float64(common) / float64(len(a)+len(b))
}

// RiskComparisonVersion 是识别风险因素小标题和对比规则的版本。规则改了（小标题、类别的识别，相似度的计算和阈值）
// 要改这个值，依赖对比结果的缓存（如模型的解读）随之失效。
const RiskComparisonVersion = "1"

// RiskFiling 是参与对比的一份 10-K。
type RiskFiling struct {
	Accession  string `json:"accession"`
	Form       string `json:"form"`
	FilingDate string `json:"filing_date"`
	ReportDate string `json:"report_date"` // 财年截止日
	URL        string `json:"url"`         // 主文件
	Count      int    `json:"count"`       // 识别出的风险因素条数
}

// RiskChanges 是一份 10-K 和上一年 10-K 的风险因素对比。
type RiskChanges struct {
	Current  RiskFiling `json:"current"`
	Previous RiskFiling `json:"previous"`
	RiskComparison
	Notes []string `json:"notes"`
}

// riskChangeNotes 是对比结果的说明。
var riskChangeNotes = []string{
	"风险因素按 Item 1A 里加粗或斜体的句子小标题识别；没有这样排版的小标题、纯文本的旧申报无法对比。",
	"保留是小标题相同（不计大小写、引号和末尾句号），正文可能有改动；改写按小标题和正文的相似度配对，一条拆成两条或两条合成一条时，没配上的算新增或删除。新增和删除只说明小标题的变化，不代表公司面临的风险本身变了。",
}

// RiskChanges 读取 raw 所在的 10-K 和上一年的 10-K（报告期更早的最近一份原始 10-K 或 10-KT，不含修订版），
// 对比两年的风险因素。raw 是该申报目录下任意文件的地址。不调用模型，不受摘要的字数上限限制。
func (c *Client) RiskChanges(ctx context.Context, raw string) (*RiskChanges, error) {
	current, err := c.readTenK(ctx, raw)
	if err != nil {
		return nil, err
	}
	if len(current.RiskFactors) == 0 {
		return nil, fmt.Errorf("%w：这份 10-K 的 Item 1A 里没有识别出风险因素小标题", ErrDocument)
	}
	previous, err := c.previousTenK(ctx, current)
	if err != nil {
		return nil, err
	}
	old, err := c.readTenK(ctx, previous.URL)
	if err != nil {
		return nil, fmt.Errorf("读取上一年的 10-K（%s）失败：%w", previous.FilingDate, err)
	}
	if len(old.RiskFactors) == 0 {
		return nil, fmt.Errorf("%w：上一年的 10-K（%s）的 Item 1A 里没有识别出风险因素小标题", ErrDocument, previous.FilingDate)
	}
	return &RiskChanges{
		Current:        riskFiling(current),
		Previous:       riskFiling(old),
		RiskComparison: CompareRiskFactors(old.RiskFactors, current.RiskFactors),
		Notes:          slices.Clone(riskChangeNotes),
	}, nil
}

func riskFiling(doc *FilingDocument) RiskFiling {
	return RiskFiling{Accession: doc.Accession, Form: doc.Form, FilingDate: doc.FilingDate, ReportDate: doc.ReportDate, URL: doc.URL, Count: len(doc.RiskFactors)}
}

// previousTenK 返回 doc 之前最近的一份原始 10-K 或 10-KT：报告期早于 doc 的报告期，跳过修订版（多半只补充第三部分，
// 没有风险因素）。没有报告期时按申报日比较。
func (c *Client) previousTenK(ctx context.Context, doc *FilingDocument) (*Filing, error) {
	// 申报日不晚于 doc 的 10-K 里，最多是本年的 10-K、它的修订版、上一年的修订版和上一年的 10-K，4 份就够；
	// 多要会让高盛、摩根大通这类申报很多的公司多读历史分页和全文检索。
	result, err := c.Filings(ctx, doc.CIK, FilingQuery{Forms: []string{"10-K", "10-KT"}, End: doc.FilingDate, Want: 4})
	// 读申报列表失败和下载主文件失败一样是 ErrDownload，调用方（如 Desktop）不会说成模型的问题。
	if err != nil {
		return nil, fmt.Errorf("%w（申报列表）：%w", ErrDownload, err)
	}
	for _, f := range result.Filings {
		earlier := f.FilingDate < doc.FilingDate
		if doc.ReportDate != "" && f.ReportDate != "" {
			earlier = f.ReportDate < doc.ReportDate
		}
		if f.Form == BaseForm(f.Form) && f.Accession != doc.Accession && earlier {
			return &f, nil
		}
	}
	return nil, fmt.Errorf("%w：没有找到 %s 之前的上一份 10-K", ErrDocument, doc.FilingDate)
}
