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

// negations 是否定短语，出现在同一句里建议用语前面不远处时不算给建议。只用短语，不用单字：
// 「非常」「无论」「不断」「不错」里的非、无、不不是否定。
var negations = []string{"不构成", "不建议", "不能", "不会", "不提供", "不给", "不应", "不宜", "不要", "不是", "并非", "无法", "没有", "勿"}

// immediateNegations 紧挨在建议用语前面时是否定，如「不买入」「非目标价」「无止损」。只在紧挨时算：
// 「非常适合买入」里的非后面不是建议用语。
const immediateNegations = "不勿别非无"

// negationWindow 是往前找否定短语的字符数。
const negationWindow = 8

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
	check.AdviceTerms = append(check.AdviceTerms, findAdvice(answer)...)
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
		if strings.ContainsRune("。！？；，,.!?;、\n ", r) {
			return false
		}
		after = after[size:]
	}
	return false
}

// findAdvice 找出投资建议用语，前面有否定的不算。
func findAdvice(answer string) []TermRef {
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
			if negated(answer[:start]) {
				continue
			}
			out = append(out, TermRef{Text: term, Start: utf8.RuneCountInString(answer[:start]), End: utf8.RuneCountInString(answer[:end])})
		}
	}
	// 按出现位置排序，方便界面依次标出。
	slices.SortFunc(out, func(a, b TermRef) int { return a.Start - b.Start })
	return out
}

// negated 判断建议用语前面是否否定：紧挨着不、勿、别，或同一句里前面 negationWindow 个字以内有否定短语。
func negated(before string) bool {
	if r, _ := utf8.DecodeLastRuneInString(before); strings.ContainsRune(immediateNegations, r) {
		return true
	}
	// 往前取最多 negationWindow 个字，遇到句号、逗号等就停，只看同一句。
	rest := before
	for count := 0; count < negationWindow && rest != ""; count++ {
		_, size := utf8.DecodeLastRuneInString(rest)
		rest = rest[:len(rest)-size]
	}
	clause := before[len(rest):]
	if i := strings.LastIndexAny(clause, "。！？；，,.!?;\n"); i >= 0 {
		_, size := utf8.DecodeRuneInString(clause[i:])
		clause = clause[i+size:]
	}
	for _, n := range negations {
		if strings.Contains(clause, n) {
			return true
		}
	}
	return false
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
