package llm

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

// 数字核对：提示词约束靠不住，所以对回答做一层确定性检查，设计见 docs/ai.md 的「数字核对」。
// 回答里的每个数字都应该能在本次的工具结果（或用户的问题）里找到；找不到的列出来，出现投资建议用语也列出来。
// 只提示、不拦截，误报率在离线评测里统计后再决定是否收紧。

// Check 是一次核对的结果。位置按字符（rune）计，界面可以据此标出。
type Check struct {
	Numbers     int         `json:"numbers"`      // 参与核对的数字个数
	Unsourced   []NumberRef `json:"unsourced"`    // 在数据里找不到的数字
	AdviceTerms []TermRef   `json:"advice_terms"` // 出现的投资建议用语
}

// NumberRef 是回答里的一个数字。
type NumberRef struct {
	Text  string `json:"text"` // 回答里的原文，如 1,523.5、12.5%、1.2万亿
	Start int    `json:"start"`
	End   int    `json:"end"`
}

// TermRef 是回答里的一个投资建议用语。
type TermRef struct {
	Text  string `json:"text"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

// OK 表示没有找不到来源的数字，也没有投资建议用语。
func (c Check) OK() bool { return len(c.Unsourced) == 0 && len(c.AdviceTerms) == 0 }

// Warnings 是给用户看的提示，每条一行；核对通过时为空。
func (c Check) Warnings() []string {
	var out []string
	if len(c.Unsourced) > 0 {
		texts := make([]string, 0, len(c.Unsourced))
		for _, n := range c.Unsourced {
			texts = append(texts, n.Text)
		}
		out = append(out, "以下数字未在数据中找到："+strings.Join(dedupe(texts), "、"))
	}
	if len(c.AdviceTerms) > 0 {
		texts := make([]string, 0, len(c.AdviceTerms))
		for _, t := range c.AdviceTerms {
			texts = append(texts, t.Text)
		}
		out = append(out, "回答里出现了投资建议用语："+strings.Join(dedupe(texts), "、")+"；sec 只解释数据，不构成投资建议")
	}
	return out
}

// Sources 收集对话里可以作为数字来源的文字：用户消息的文字和所有工具结果。
func Sources(messages []Message) []string {
	var out []string
	for _, m := range messages {
		if m.Role == RoleUser && m.Text != "" {
			out = append(out, m.Text)
		}
		for _, r := range m.ToolResults {
			out = append(out, r.Content)
		}
	}
	return out
}

// numberPattern 匹配一个数字：整数、小数和 JSON 的指数形式。千分位只在回答里合并（见 mergeThousands），
// 来源（工具结果的 JSON）里逗号是分隔符，「[5,1523.45]」是两个数。
var numberPattern = regexp.MustCompile(`\d+(?:\.\d+)?(?:[eE][+-]?\d+)?`)

// unitPattern 匹配数字后面的单位，允许中间有一个空格。
var unitPattern = regexp.MustCompile(`^\s?(万亿|亿|万|%|％)`)

// approxWords 出现在数字前面时放宽到 5% 的误差：回答说「约 1500」时不要求和 1523.4 精确对上。长的放前面先匹配。
// 「左右」通常在数字后面，见 followedBy；也接受放在前面的写法。
var approxWords = []string{"大约", "接近", "将近", "超过", "左右", "约", "近", "逾"}

// approxTolerance 是带约数词时允许的相对误差。
const approxTolerance = 0.05

// approxLinks 是约数词和数字之间常见的字，如「约为」「大约是」「接近在」。
var approxLinks = []string{"为", "是", "在"}

// adviceTerms 是投资建议用语。前面有否定时不算，比如「不构成买入或卖出建议」。
var adviceTerms = []string{"买入", "卖出", "加仓", "减仓", "建仓", "清仓", "抄底", "止损", "止盈", "目标价", "逢低吸纳", "逢高减持", "满仓", "重仓"}

// adviceCompound 是包含建议用语、意思却不同的词，比如「目标价值」是股权奖励的目标金额（8-K Item 5.02 常见），
// 「建仓期」是基金合同约定的建仓期限（基金公告常见）。同样的词也能用来给建议（「该股目标价值 50 元」「当前是较好的建仓期」），
// 所以只有来源里有 evidence 之一（不分大小写）时才不算：摘要里这个词转述的是原文；估值解读的来源是工具数据，没有这些词，
// 照常提示。notBefore 是紧跟在词后面时说明切分不同的字：「目标价值得参考」是「目标价 / 值得」。「目标价格」就是目标价，不排除。
type adviceCompound struct {
	word      string
	evidence  []string
	notBefore string
}

var adviceCompounds = []adviceCompound{
	{word: "目标价值", evidence: []string{"目标价值", "target value"}, notBefore: "得"},
	{word: "建仓期", evidence: []string{"建仓期"}},
}

// 否定的范围：否定短语管到同一句里它后面的建议用语，遇到断句标点或连词（stopWords）为止，最多隔 negationReach 个字。
// 数字数时，闭合的引号连同里面的话、以及中间列举的其他建议用语都不算：「不能给出买入、持有或卖出的建议」
// 「我不能回答“现在是否适合买入”或给出目标价」。建议用语在引号里时看整个引号有没有被否定，或者引号复述的是用户的话（reportCues）。
// 以「关于」等开头的一句是话题，后面同一句话里有否定时不算（topicMarkers）。

// negations 是否定短语。只用短语，不用单字：「非常」「无论」「不断」「不错」里的非、无、不不是否定。
var negations = []string{"不构成", "不建议", "不能", "不会", "不提供", "不给", "不应", "不宜", "不要", "不是", "不代表", "不意味着", "不等于", "不做", "并非", "无法", "没法", "没有", "勿"}

// immediateNegations 紧挨在建议用语前面时是否定，如「不买入」「非目标价」「无止损」。
// 只在紧挨时算：「非常适合买入」里的非后面不是建议用语。
const immediateNegations = "不勿别非无"

// questionWords 紧挨在建议用语前面（中间可以有 questionModals 之一）时是在问，不是给建议：「是否买入需要你自己判断」
// 「现在是否适合买入」。隔开时不算：「无论是否回调都建议买入」「看是否放量再加仓」。
var (
	questionWords  = []string{"是否", "能否"}
	questionModals = []string{"适合", "应该", "应当", "要", "该", "可以"}
)

// stopWords 是连词，否定管不到它后面：「我不能保证“茅台”会涨所以买入」「不要止损直接加仓」「不能给出买入的理由而是建议你卖出」。
// 整词匹配，「而且」不算。「建议」「可以」「应该」不算：「我不能给出买入建议或目标价」里的建议是名词，
// 「不代表 sec 建议卖出」的否定管着它。
var stopWords = []string{"但", "却", "而是", "所以", "因此", "直接"}

// topicMarkers 开头的一句是话题，同一句话（到句号等为止）后面有否定时，话题里的建议用语不算：
// 「关于买入时机和目标价，sec 没有这类数据」。
var topicMarkers = []string{"关于", "对于", "至于"}

// reportCues 是复述用户的话的说法，紧跟着的引号里是用户问的内容，不是建议：「关于你问的“目标价”」。
var reportCues = []string{"你问的", "你问", "你提到的", "你提到", "你说的", "你说", "问题里的", "问题中的"}

// quotePairs 是中文引号。只有同一行里、不超过 maxQuoteRunes 个字就闭合的才算引号，漏写闭引号时不会把后面几段都算进去。
// 英文的 " 不分开闭，不处理。
var quotePairs = [][2]string{{"「", "」"}, {"『", "』"}, {"“", "”"}, {"‘", "’"}}

const maxQuoteRunes = 30

// clauseBreaks 是断句的标点，sentenceBreaks 是一句话结束的标点。
const (
	clauseBreaks   = "。！？；，,.!?;：:\n"
	sentenceBreaks = "。！？；.!?;\n"
)

// negationReach 是否定最多管到的字数（引号和列举的建议用语不算）。
const negationReach = 12

// maxListMarker 是行首列表序号的最大值，更大的数（如「1524、」）照常核对。
const maxListMarker = 99

type number struct {
	ref       NumberRef
	value     float64 // 换算单位后的值（万、亿已乘上），百分数不变
	tolerance float64 // 按回答给出的精度允许的误差
	percent   bool
	approx    bool
}

// sourceSet 是来源里的数字，按单位倍数分组，每组去重排序，核对时按区间二分查找。
type sourceSet map[float64][]float64

// Verify 核对回答里的数字和投资建议用语。sources 是工具结果和用户问题等数字来源，一般用 Sources 收集。
func Verify(answer string, sources ...string) Check {
	set := sourceSet{}
	for _, s := range sources {
		scales := sourceScales(s)
		for _, n := range extractNumbers(s, false) {
			for _, scale := range scales {
				set[scale] = append(set[scale], math.Abs(n.value))
			}
		}
	}
	for scale, values := range set {
		slices.Sort(values)
		set[scale] = slices.Compact(values)
	}
	// 空的列表编码成 []，不是 null。
	check := Check{Unsourced: []NumberRef{}, AdviceTerms: []TermRef{}}
	for _, n := range extractNumbers(answer, true) {
		check.Numbers++
		if !set.has(n) {
			check.Unsourced = append(check.Unsourced, n.ref)
		}
	}
	check.AdviceTerms = append(check.AdviceTerms, findAdvice(answer, sources)...)
	return check
}

// sourceScales 是一个来源里的数字可能用的单位倍数，按来源自己写明的单位判断：aitool 的金额写明「单位是亿」，
// 宏观序列写明 "unit":"USD_million" 等。没有写明单位的只按原值比较，不猜：
// 固定地乘 1 万、1 亿会让把万、亿、万亿写错的回答也通过。
func sourceScales(text string) []float64 {
	scales := []float64{1}
	lower := strings.ToLower(text)
	for _, u := range []struct {
		hints []string
		scale float64
	}{
		{[]string{"亿", "100_million"}, 1e8},
		{[]string{"万元", "单位是万", `"万"`, "10_thousand"}, 1e4},
		{[]string{"million", "百万"}, 1e6},
		{[]string{"billion", "十亿"}, 1e9},
		{[]string{"thousand", "千元"}, 1e3},
	} {
		if slices.ContainsFunc(u.hints, func(h string) bool { return strings.Contains(lower, h) }) {
			scales = append(scales, u.scale)
		}
	}
	return scales
}

// has 判断回答里的数字能不能在来源里找到：允许按回答的精度四舍五入，正负号不计（「下跌 3.2%」对应 -3.2），
// 100% 以内的百分数也按比例比较（12.5% 对应 0.125）。
func (set sourceSet) has(n number) bool {
	type target struct{ value, tolerance float64 }
	targets := []target{{math.Abs(n.value), n.tolerance}}
	// 工具的百分比都以百分数给出，「127%」按比例去对 1.27 多半是把 -1.27% 错写了，不能算对上。
	if n.percent && math.Abs(n.value) < 100 {
		targets = append(targets, target{math.Abs(n.value) / 100, n.tolerance / 100})
	}
	for _, t := range targets {
		tolerance := t.tolerance
		if n.approx {
			tolerance = math.Max(tolerance, t.value*approxTolerance)
		}
		// 留一点浮点误差的余量。
		tolerance = tolerance*(1+1e-9) + 1e-12*math.Max(1, t.value)
		for scale, values := range set {
			lo, hi := (t.value-tolerance)/scale, (t.value+tolerance)/scale
			i, _ := slices.BinarySearch(values, lo)
			if i < len(values) && values[i] <= hi {
				return true
			}
		}
	}
	return false
}

// extractNumbers 找出文字里的数字。forAnswer 为 true 时合并千分位、计算位置，并跳过不需要核对的数字：
// 0 到 10 的整数（个数、序号）、行首的列表序号和紧跟在字母后面的整数（MA20、Q3、SH600519 这类名称和代码）。
func extractNumbers(text string, forAnswer bool) []number {
	var out []number
	matches := numberPattern.FindAllStringIndex(text, -1)
	if forAnswer {
		matches = mergeThousands(text, matches)
	}
	// 位置按字符计，边扫描边累加，避免每个数字都从头数一遍。
	runeIndex, byteIndex := 0, 0
	runeAt := func(offset int) int {
		runeIndex += utf8.RuneCountInString(text[byteIndex:offset])
		byteIndex = offset
		return runeIndex
	}
	for _, loc := range matches {
		start, end := loc[0], loc[1]
		raw := text[start:end]
		value, err := strconv.ParseFloat(strings.ReplaceAll(raw, ",", ""), 64)
		if err != nil {
			continue
		}
		n := number{value: value}
		// 小数位数决定允许的四舍五入误差。
		decimals := 0
		if i := strings.IndexByte(raw, '.'); i >= 0 && !strings.ContainsAny(raw, "eE") {
			decimals = len(raw) - i - 1
		}
		integer := decimals == 0 && !strings.ContainsAny(raw, "eE")
		n.tolerance = 0.5 * math.Pow(10, -float64(decimals))
		textEnd := end
		if unit := unitPattern.FindStringSubmatch(text[end:]); unit != nil {
			textEnd = end + len(unit[0])
			switch unit[1] {
			case "%", "％":
				n.percent = true
			case "万":
				n.value, n.tolerance = n.value*1e4, n.tolerance*1e4
			case "亿":
				n.value, n.tolerance = n.value*1e8, n.tolerance*1e8
			case "万亿":
				n.value, n.tolerance = n.value*1e12, n.tolerance*1e12
			}
		}
		if forAnswer {
			hasUnit := textEnd != end
			if integer && !hasUnit && value <= maxListMarker && listMarker(text, start, end) {
				continue
			}
			if integer && !hasUnit && value >= 0 && value <= 10 {
				continue
			}
			if integer && start > 0 && isASCIILetter(text[start-1]) {
				continue
			}
			n.approx = approximate(text[:start]) || followedBy(text[textEnd:], "左右", 3)
			n.ref = NumberRef{Text: text[start:textEnd], Start: runeAt(start), End: runeAt(textEnd)}
		}
		out = append(out, n)
	}
	return out
}

// mergeThousands 把回答里的千分位数字合并成一个：后面每组正好 3 位，最后一组后面不再紧跟数字（正则已保证），
// 如「5,236,000,000」「1,523.45」。「1,5234」不是千分位，按两个数处理。
func mergeThousands(text string, matches [][]int) [][]int {
	out := make([][]int, 0, len(matches))
	for i := 0; i < len(matches); i++ {
		start, end := matches[i][0], matches[i][1]
		first := text[start:end]
		if len(first) > 3 || strings.ContainsAny(first, ".eE") {
			out = append(out, matches[i])
			continue
		}
		for i+1 < len(matches) && end < len(text) && text[end] == ',' && matches[i+1][0] == end+1 {
			group := text[matches[i+1][0]:matches[i+1][1]]
			digits := group
			if j := strings.IndexByte(group, '.'); j >= 0 {
				digits = group[:j]
			}
			if len(digits) != 3 || strings.ContainsAny(group, "eE") {
				break
			}
			i++
			end = matches[i][1]
			if strings.Contains(group, ".") {
				break
			}
		}
		out = append(out, []int{start, end})
	}
	return out
}

func isASCIILetter(b byte) bool { return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' }

// listMarker 判断数字是不是行首的列表序号，如「1. 」「2、」「3）」。
func listMarker(text string, start, end int) bool {
	lineStart := strings.LastIndexByte(text[:start], '\n') + 1
	if strings.TrimSpace(text[lineStart:start]) != "" {
		return false
	}
	rest := text[end:]
	for _, marker := range []string{".", "、", ")", "）"} {
		if strings.HasPrefix(rest, marker) {
			return true
		}
	}
	return false
}

// approximate 判断数字前面是不是约数词，允许中间有空格（含全角空格）和一个「为」「是」「在」，如「约为 1500」「大约是 1500」。
// 「最近」「附近」里的近不是约数。
func approximate(before string) bool {
	before = strings.TrimRight(before, " 　")
	for _, link := range approxLinks {
		if trimmed, ok := strings.CutSuffix(before, link); ok {
			before = strings.TrimRight(trimmed, " 　")
			break
		}
	}
	for _, w := range approxWords {
		if !strings.HasSuffix(before, w) {
			continue
		}
		if w == "近" {
			prev, _ := utf8.DecodeLastRuneInString(strings.TrimSuffix(before, w))
			if prev == '最' || prev == '附' {
				return false
			}
		}
		return true
	}
	return false
}

// followedBy 判断 word 是否紧跟在后面，中间最多隔 gap 个字符的单位（如「22 倍左右」「1500 元左右」），不跨标点。
func followedBy(after, word string, gap int) bool {
	after = strings.TrimLeft(after, " ")
	for skip := 0; skip <= gap && after != ""; skip++ {
		if strings.HasPrefix(after, word) {
			return true
		}
		r, size := utf8.DecodeRuneInString(after)
		if strings.ContainsRune(clauseBreaks+"、 ", r) {
			return false
		}
		after = after[size:]
	}
	return false
}

// findAdvice 找出投资建议用语，被否定的不算（见 negations 上面的说明），出现在来源支持的复合词里的也不算（见 adviceCompound）。
func findAdvice(answer string, sources []string) []TermRef {
	compounds := supportedCompounds(answer, sources)
	quotes := quoteSpans(answer)
	var out []TermRef
	for _, term := range adviceTerms {
		offset := 0
		for {
			i := strings.Index(answer[offset:], term)
			if i < 0 {
				break
			}
			start := offset + i
			end := start + len(term)
			offset = end
			if inCompound(answer, start, term, compounds) || negated(answer, start, end, quotes) {
				continue
			}
			out = append(out, TermRef{Text: term, Start: utf8.RuneCountInString(answer[:start]), End: utf8.RuneCountInString(answer[:end])})
		}
	}
	// 按出现位置排序，方便界面依次标出。
	slices.SortFunc(out, func(a, b TermRef) int { return a.Start - b.Start })
	return out
}

// supportedCompounds 返回回答里出现、并且来源里有对应原文的复合词。回答里没有复合词时不看来源，大来源（如 K 线）不必转小写。
func supportedCompounds(answer string, sources []string) []adviceCompound {
	var candidates []adviceCompound
	for _, c := range adviceCompounds {
		if strings.Contains(answer, c.word) {
			candidates = append(candidates, c)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	lower := make([]string, len(sources))
	for i, source := range sources {
		lower[i] = strings.ToLower(source)
	}
	var out []adviceCompound
	for _, c := range candidates {
		if slices.ContainsFunc(c.evidence, func(e string) bool {
			return slices.ContainsFunc(lower, func(source string) bool { return strings.Contains(source, e) })
		}) {
			out = append(out, c)
		}
	}
	return out
}

// inCompound 判断 answer 中从 start 开始的建议用语 term 是否是 compounds 里某个词的一部分；term 在词里出现几次都检查。
func inCompound(answer string, start int, term string, compounds []adviceCompound) bool {
	for _, c := range compounds {
		for k := 0; ; {
			i := strings.Index(c.word[k:], term)
			if i < 0 {
				break
			}
			k += i
			if begin := start - k; begin >= 0 && strings.HasPrefix(answer[begin:], c.word) && (c.notBefore == "" || !strings.HasPrefix(answer[begin+len(c.word):], c.notBefore)) {
				return true
			}
			k += len(term)
		}
	}
	return false
}

// span 是一对闭合的引号在回答里的字节位置，end 在闭引号之后。
type span struct{ start, end int }

// quoteSpans 找出回答里闭合的引号（见 quotePairs），按位置排序；套着的引号只取外层。
func quoteSpans(answer string) []span {
	var out []span
	for i := 0; i < len(answer); {
		open, pair := -1, [2]string{}
		for _, q := range quotePairs {
			if j := strings.Index(answer[i:], q[0]); j >= 0 && (open < 0 || i+j < open) {
				open, pair = i+j, q
			}
		}
		if open < 0 {
			break
		}
		inner := answer[open+len(pair[0]):]
		closing := strings.Index(inner, pair[1])
		if closing < 0 || strings.Contains(inner[:closing], "\n") || utf8.RuneCountInString(inner[:closing]) > maxQuoteRunes {
			i = open + len(pair[0])
			continue
		}
		end := open + len(pair[0]) + closing + len(pair[1])
		out = append(out, span{open, end})
		i = end
	}
	return out
}

// negated 判断 answer[start:end] 处的建议用语是否被否定：紧挨着不、勿、别、非、无，在否定短语的范围里，或者在后面被否定的话题里。
// 在引号里时看整个引号；引号前面紧挨着复述用户的说法时不算。
func negated(answer string, start, end int, quotes []span) bool {
	if r, _ := utf8.DecodeLastRuneInString(answer[:start]); strings.ContainsRune(immediateNegations, r) || asked(answer[:start]) {
		return true
	}
	for _, q := range quotes {
		if q.start <= start && end <= q.end {
			start = q.start
			break
		}
	}
	reported := func(p int) bool {
		before := strings.TrimRight(answer[:p], " ")
		return slices.ContainsFunc(reportCues, func(cue string) bool { return strings.HasSuffix(before, cue) })
	}
	if reported(start) || asked(answer[:start]) {
		return true
	}
	// 从建议用语往前逐字找，跳过闭合的引号和列举的建议用语。
	counted := 0
	p := start
	for p > 0 {
		if q, ok := quoteEndingAt(quotes, p); ok {
			if p = q.start; reported(p) {
				return true
			}
			continue
		}
		before := answer[:p]
		if slices.ContainsFunc(negations, func(n string) bool { return strings.HasSuffix(before, n) }) {
			return true
		}
		if slices.ContainsFunc(stopWords, func(w string) bool { return strings.HasSuffix(before, w) }) {
			return false
		}
		if i := slices.IndexFunc(adviceTerms, func(t string) bool { return strings.HasSuffix(before, t) }); i >= 0 {
			p -= len(adviceTerms[i])
			continue
		}
		r, size := utf8.DecodeLastRuneInString(before)
		if strings.ContainsRune(clauseBreaks, r) {
			break
		}
		if counted++; counted > negationReach {
			return false
		}
		p -= size
	}
	return topicNegated(answer, p, end)
}

// asked 判断建议用语前面是不是 questionWords（见 questionWords）。
func asked(before string) bool {
	for _, m := range questionModals {
		if trimmed, ok := strings.CutSuffix(before, m); ok && slices.ContainsFunc(questionWords, func(w string) bool { return strings.HasSuffix(trimmed, w) }) {
			return true
		}
	}
	return slices.ContainsFunc(questionWords, func(w string) bool { return strings.HasSuffix(before, w) })
}

// topicNegated 判断从 clauseStart 开始的一句是不是以 topicMarkers 开头的话题、并且同一句话里 end 后面有否定。
func topicNegated(answer string, clauseStart, end int) bool {
	clause := strings.TrimLeft(answer[clauseStart:end], " ")
	if !slices.ContainsFunc(topicMarkers, func(m string) bool { return strings.HasPrefix(clause, m) }) {
		return false
	}
	rest := answer[end:]
	if i := strings.IndexAny(rest, sentenceBreaks); i >= 0 {
		rest = rest[:i]
	}
	return slices.ContainsFunc(negations, func(n string) bool { return strings.Contains(rest, n) })
}

// quoteEndingAt 返回在 p 处结束的引号。
func quoteEndingAt(quotes []span, p int) (span, bool) {
	for _, q := range quotes {
		if q.end == p {
			return q, true
		}
	}
	return span{}, false
}

func dedupe(items []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if !seen[item] {
			seen[item] = true
			out = append(out, item)
		}
	}
	return out
}

// String 便于调试和测试输出。
func (c Check) String() string {
	return fmt.Sprintf("核对 %d 个数字，%d 个未找到来源，%d 个投资建议用语", c.Numbers, len(c.Unsourced), len(c.AdviceTerms))
}
