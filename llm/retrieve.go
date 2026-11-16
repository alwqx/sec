package llm

import (
	"math"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/alwqx/sec/provider/cninfo"
)

// 招股书问答只把和问题相关的几页发给模型：按问题里的词给每页打分（BM25），取分数最高的页，总字数不超过预算。
// 中文不分词，用相邻两个汉字作为词；英文和数字按整词。先去掉问句的说法（「是否」「有哪些」）和「公司」「发行人」这类
// 每页都有的词，再在虚字处断开，单个字不计。

const (
	bm25K1 = 1.2
	bm25B  = 0.75
)

// questionStopWords 是去掉问句说法后仍不参与打分的两字词。
var questionStopWords = map[string]bool{
	"公司": true, "发行": true, "情况": true, "主要": true, "以及": true, "还有": true, "没有": true,
	"一下": true, "介绍": true, "说明": true, "分别": true,
}

// questionStopChars 是问句里的虚字，在这里断开，不和前后的字组成词（「收入和净利润」不产生「入和」「和净」）。
// 只用不会出现在实词里的字：「对」「有」「在」会切断「对赌」「有限」「存在」。
var questionStopChars = map[rune]bool{
	'的': true, '了': true, '是': true, '和': true, '与': true, '及': true, '或': true, '吗': true, '呢': true, '吧': true, '呀': true,
}

// questionStopPhrases 在分词前把问句的说法和每页都有的词换成空格，不和前后的字组成「否存」「司最」这样的词。
// 同一位置按参数顺序匹配，「子公司」原样保留，不被「公司」拆开；「怎么样」排在「怎么」前。
var questionStopPhrases = strings.NewReplacer(
	"子公司", "子公司",
	"为什么", " ", "是什么", " ", "什么", " ", "有哪些", " ", "哪些", " ", "哪个", " ", "哪家", " ",
	"是不是", " ", "是否", " ", "有没有", " ", "存在", " ", "多少", " ", "怎么样", " ", "怎么", " ", "怎样", " ",
	"如何", " ", "是谁", " ", "请问", " ",
	"本公司", " ", "公司", " ", "发行人", " ", "招股说明书", " ", "招股书", " ",
)

// questionTerms 返回问题里参与打分的词，去重；单个汉字、字母或数字不计。
func questionTerms(question string) []string {
	question = questionStopPhrases.Replace(question)
	var terms []string
	seen := map[string]bool{}
	add := func(term string) {
		if term != "" && !seen[term] && !questionStopWords[term] {
			seen[term] = true
			terms = append(terms, term)
		}
	}
	var han []rune
	var word []rune
	flush := func() {
		for i := 0; i+1 < len(han); i++ {
			add(string(han[i : i+2]))
		}
		han = han[:0]
		if len(word) > 1 {
			add(strings.ToLower(string(word)))
		}
		word = word[:0]
	}
	for _, r := range question {
		switch {
		case questionStopChars[r]:
			flush()
		case unicode.Is(unicode.Han, r):
			if len(word) > 0 {
				flush()
			}
			han = append(han, r)
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if len(han) > 0 {
				flush()
			}
			word = append(word, r)
		default:
			flush()
		}
	}
	flush()
	return terms
}

// rankPages 按和问题的相关程度给页排序，返回页在 pages 里的下标，分数为 0 的页不返回。
func rankPages(pages []cninfo.DocumentPage, question string) []int {
	terms := questionTerms(question)
	if len(terms) == 0 || len(pages) == 0 {
		return nil
	}
	lower := make([]string, len(pages))
	lengths := make([]float64, len(pages))
	avg := 0.0
	for i, page := range pages {
		lower[i] = strings.ToLower(page.Text)
		lengths[i] = float64(utf8.RuneCountInString(page.Text))
		avg += lengths[i]
	}
	avg = max(avg/float64(len(pages)), 1)
	scores := make([]float64, len(pages))
	for _, term := range terms {
		counts := make([]int, len(pages))
		df := 0
		for i, text := range lower {
			if counts[i] = strings.Count(text, term); counts[i] > 0 {
				df++
			}
		}
		if df == 0 {
			continue
		}
		idf := math.Log(1 + (float64(len(pages))-float64(df)+0.5)/(float64(df)+0.5))
		for i, c := range counts {
			if c > 0 {
				tf := float64(c)
				scores[i] += idf * tf * (bm25K1 + 1) / (tf + bm25K1*(1-bm25B+bm25B*lengths[i]/avg))
			}
		}
	}
	var ranked []int
	for i, score := range scores {
		if score > 0 {
			ranked = append(ranked, i)
		}
	}
	// 分数相同时页码小的在前，结果是确定的。
	slices.SortStableFunc(ranked, func(a, b int) int {
		switch {
		case scores[a] > scores[b]:
			return -1
		case scores[a] < scores[b]:
			return 1
		}
		return a - b
	})
	return ranked
}

// selectPages 按分数从高到低取页，每取一页也取它的下一页（风险因素、财务表格常跨页），总字数不超过 budget、
// 最多 maxPages 页，按页码排序返回。
func selectPages(pages []cninfo.DocumentPage, ranked []int, budget, maxPages int) []cninfo.DocumentPage {
	picked := map[int]bool{}
	total := 0
	take := func(i int) {
		if i >= len(pages) || picked[i] || len(picked) == maxPages {
			return
		}
		if n := utf8.RuneCountInString(pages[i].Text); total+n <= budget {
			picked[i], total = true, total+n
		}
	}
	for _, i := range ranked {
		take(i)
		if picked[i] {
			take(i + 1)
		}
	}
	keys := make([]int, 0, len(picked))
	for i := range picked {
		keys = append(keys, i)
	}
	slices.Sort(keys)
	out := make([]cninfo.DocumentPage, len(keys))
	for k, i := range keys {
		out[k] = pages[i]
	}
	return out
}
