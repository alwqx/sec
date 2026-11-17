package edgar

import (
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// minSectionRunes 是可以摘要的章节最少字数。更短的通常是「不适用」（较小的申报公司不必披露风险因素）
// 或只是一句引用（银行等公司的 MD&A 在年报其他部分，Item 7 只写「见第 X 页」）。
const minSectionRunes = 1500

// maxHeadingBytes 是普通 Item 标题行的最大长度。更长的行多半是以「Item 8 of this Form 10-K」开头的句子；
// 标题和正文写在同一段时，标题必须是该 Item 的标准标题（见 tenKTitles）才算。
const maxHeadingBytes = 200

var (
	// tenKHeading 匹配 Item 标题行，也接受「Items 7 and 7A」这样的复数标题，取第一个编号。
	tenKHeading = regexp.MustCompile(`(?i)^items?\s*(\d{1,2}[a-c]?)\b[\s.:—–-]*(.*)$`)
	// 页眉页脚：页码、目录链接、PART 标题、只有编号的 Item 页眉（如 Item 1A、Item 1B, 1C）、带页码的 Form 10-K 页脚。
	pageNumber = regexp.MustCompile(`(?i)^(page\s+)?\d{1,3}(\s+of\s+\d{1,3})?$`)
	partHeader = regexp.MustCompile(`(?i)^parts?\s+[ivx]+(\s*(,|and|&)\s*[ivx]+)*$`)
	bareItem   = regexp.MustCompile(`(?i)^items?\s*\d{1,2}[a-c]?\.?(\s*(,|and|&)\s*\d{1,2}[a-c]?)*$`)
	formFooter = regexp.MustCompile(`(?i)(form\s+10-k\b.*\b\d{1,3}$)|(^\d{1,3}\s.*\b(19|20)\d{2}\s+form\s+10-k$)`)
	// tenKTitles 是 10-K 各 Item 标准标题的开头（小写），用来识别标题和正文写在同一段、或编号和标题分成两行的标题。
	tenKTitles = map[string][]string{
		"1": {"business"}, "1a": {"risk factors"}, "1b": {"unresolved staff comments"}, "1c": {"cybersecurity"},
		"2": {"properties"}, "3": {"legal proceedings"}, "4": {"mine safety", "submission of matters", "(removed and reserved)", "reserved"},
		"5": {"market for"}, "6": {"selected financial data", "[reserved]", "reserved"}, "7": {"management"},
		"7a": {"quantitative and qualitative"}, "8": {"financial statements"}, "9": {"changes in and disagreements"},
		"9a": {"controls and procedures"}, "9b": {"other information"}, "9c": {"disclosure regarding foreign"},
		"10": {"directors"}, "11": {"executive compensation"}, "12": {"security ownership"}, "13": {"certain relationships"},
		"14": {"principal account"}, "15": {"exhibit"}, "16": {"form 10-k summary"},
	}
	// tenKSections 是参与摘要的两节。Item 太短或没有 Item 标题时按标题在年报正文或年报附件里找（byTitle），what 写进说明：
	// 银行的 Item 7 只写「见年报第 X 页」（摩根大通），花旗的 10-K 正文不写 Item 编号，富国银行的在附件 Exhibit 13 里。
	tenKSections = []struct {
		item, title, label, short string
		byTitle                   func([]docLine, []tenKLine) (start, end int, text string)
		what                      string
	}{
		{"1a", "risk factors", "Item 1A", "Item 1A 风险因素正文只有 %d 字，可能不适用（如较小的申报公司）或只是引用其他部分，未参与摘要。",
			riskFactorsByTitle, "风险因素"},
		{"7", "management", "Item 7", "Item 7 管理层讨论与分析正文只有 %d 字，可能只是引用年报的其他部分（银行等公司常见），未参与摘要。",
			mdnaByTitle, "管理层讨论与分析"},
	}
	// notApplicable 匹配写明不适用的章节（较小的申报公司不必披露风险因素），这时不按标题去找别的部分。
	notApplicable = regexp.MustCompile(`(?i)not applicable|smaller reporting compan`)
)

// Read10K 读取一份 10-K（含 10-K/A、过渡期年报 10-KT）主文件里的 Item 1A 风险因素和 Item 7 管理层讨论与分析，
// 各为一节，去掉目录、页眉页脚和页码。找不到或太短的章节在 Notes 里说明；两节都没有时报错，
// 比如只补充第三部分的 10-K/A。raw 是该申报目录下任意文件的地址。
func (c *Client) Read10K(ctx context.Context, raw string) (*FilingDocument, error) {
	doc, err := c.readTenK(ctx, raw, "1a", "7")
	if err != nil {
		return nil, err
	}
	return doc, checkTenKSize(doc)
}

// readTenK 读取一份 10-K 的章节和风险因素，不检查摘要的字数上限。annualItems 是可以在年报附件里找的章节：
// 风险因素对比不调用模型，也用不到 Item 7，只为风险因素读取附件。
func (c *Client) readTenK(ctx context.Context, raw string, annualItems ...string) (*FilingDocument, error) {
	doc, docs, lines, err := c.readPrimary(ctx, raw, "10-K", "10-KT")
	if err != nil {
		return nil, err
	}
	return read10K(doc, lines, c.annualReport(ctx, docs, annualItems...))
}

// annualExhibit 匹配年报附件 EX-13 的类型，也接受 EX-13.1 这样的编号。
var annualExhibit = regexp.MustCompile(`(?i)^EX-13(\.\d+)?$`)

// annualReport 是 10-K 附带的年报（Exhibit 13）：富国银行等公司的 Item 1A 和 Item 7 只引用年报，风险因素和 MD&A 在附件里。
// items 是可以在附件里找的章节（tenKSections 的 item）。load 只在主文件里找不到这些章节时调用（富国银行的 EX-13 有 11 MB），
// 只下载一次。
type annualReport struct {
	url   string
	items []string
	load  func() (*annualLines, error)
}

// annualLines 是年报附件的各行和分类结果。
type annualLines struct {
	lines   []docLine
	scanned []tenKLine
}

// annualReport 返回申报目录里第一个 HTML 或文本格式的 EX-13，可以在里面找 items 这些章节；没有 EX-13 时返回 nil。
func (c *Client) annualReport(ctx context.Context, docs []indexDocument, items ...string) *annualReport {
	for _, d := range docs {
		if annualExhibit.MatchString(d.Type) && textDocument[strings.ToLower(path.Ext(d.URL))] {
			return &annualReport{url: d.URL, items: items, load: sync.OnceValues(func() (*annualLines, error) {
				lines, err := c.documentLines(ctx, d.URL)
				if err != nil {
					return nil, err
				}
				return &annualLines{lines, scanTenK(lineTexts(lines))}, nil
			})}
		}
	}
	return nil
}

// checkTenKSize 检查参与摘要的 Item 1A 和 Item 7 合计不超过 MaxDocumentRunes。
func checkTenKSize(doc *FilingDocument) error {
	total := 0
	for _, section := range doc.Sections {
		total += utf8.RuneCountInString(section.Text)
	}
	if total > MaxDocumentRunes {
		return fmt.Errorf("%w：10-K 的 Item 1A 和 Item 7 超过 %d 字", ErrDocument, MaxDocumentRunes)
	}
	return nil
}

// read10K 从已读到的 10-K 主文件里取出 Item 1A 和 Item 7，以及 Item 1A 里的风险因素小标题；字数上限由调用方检查。
// Item 太短或没有 Item 标题时先按标题在主文件里找，再找年报附件 annual（可以为 nil），找到的章节引用附件的地址。
func read10K(doc *FilingDocument, docLines []docLine, annual *annualReport) (*FilingDocument, error) {
	lines := lineTexts(docLines)
	scanned := scanTenK(lines)
	annualNoted := false
	for _, want := range tenKSections {
		start, end := findSection(lines, scanned, want.item, want.title)
		section := sectionText(lines, scanned, start, end)
		n := utf8.RuneCountInString(section)
		// 章节所在的文件：主文件，或者按标题在年报附件里找到时是附件。
		srcLines, srcScanned, srcURL := docLines, scanned, doc.URL
		var notes []string
		if n < minSectionRunes && !notApplicable.MatchString(section) {
			where := want.label + " 只是引用年报"
			if section == "" {
				where = "10-K 正文里没有 " + want.label + " 标题"
			}
			// 说明里写找到的标题行，如「Management’s discussion and analysis」「Financial Review」。
			found := func(from, to int, text, in string) {
				section, n, start, end = text, utf8.RuneCountInString(text), from, to
				heading, _, _ := strings.Cut(text, "\n")
				notes = append(notes, where+"，按「"+heading+"」标题在"+in+"里取出"+want.what+"。")
			}
			if from, to, text := want.byTitle(docLines, scanned); utf8.RuneCountInString(text) >= minSectionRunes {
				found(from, to, text, "年报正文")
			} else if annual != nil && slices.Contains(annual.items, want.item) {
				a, err := annual.load()
				switch {
				// 下载失败可能是暂时的，报错让用户重试，不返回（并缓存）缺了这一节的结果。
				case errors.Is(err, ErrDownload):
					return nil, err
				// 附件本身用不了（超过大小等），已经找到的章节照常返回。
				case err != nil:
					if !annualNoted {
						annualNoted = true
						doc.Notes = append(doc.Notes, fmt.Sprintf("年报附件 Exhibit 13 无法读取，未参与摘要：%v。", err))
					}
				default:
					if from, to, text := want.byTitle(a.lines, a.scanned); utf8.RuneCountInString(text) >= minSectionRunes {
						found(from, to, text, "年报附件 Exhibit 13 ")
						srcLines, srcScanned, srcURL = a.lines, a.scanned, annual.url
					}
				}
			}
		}
		if want.item == "7" && n > maxMDNARunes {
			section = truncateLines(section, maxMDNARunes)
			notes = append(notes, fmt.Sprintf("管理层讨论与分析有 %d 字，只摘要开头的 %d 字（通常是概览、经营结果和财务状况），后面的部分未参与摘要。", n, utf8.RuneCountInString(section)))
		}
		switch {
		case section == "":
			doc.Notes = append(doc.Notes, fmt.Sprintf("10-K 正文里没有找到 %s。", want.label))
		case n < minSectionRunes:
			doc.Notes = append(doc.Notes, fmt.Sprintf(want.short, n))
		default:
			doc.Sections = append(doc.Sections, DocumentSection{Label: want.label, URL: srcURL, Text: section})
			doc.Notes = append(doc.Notes, notes...)
			if want.item == "1a" {
				doc.RiskFactors = riskFactors(srcLines, srcScanned, start, end)
			}
		}
	}
	if len(doc.Sections) == 0 {
		return nil, fmt.Errorf("%w：10-K 正文里没有可摘要的 Item 1A 或 Item 7", ErrDocument)
	}
	return doc, nil
}

// maxMDNARunes 是参与摘要的管理层讨论与分析的字数上限。大银行的 MD&A 有 30 多万字（高盛的 Item 7、摩根大通年报里的
// MD&A），全部摘要要十几块、加上 Item 1A 超过 MaxDocumentRunes；开头的 10 万字通常已经包括概览、经营结果和财务状况。
const maxMDNARunes = 100000

// mdnaEnds 是年报里管理层讨论与分析之后的部分的标题（规范化后的开头）。
var mdnaEnds = []string{"report of independent registered public accounting firm", "management's report on internal control"}

// maxHeadingRunes 是按标题找章节时标题行的最大字数，更长的是正文。
const maxHeadingRunes = 120

// headingText 返回短行规范化后的文字，长行返回空字符串，不必为正文做规范化。
func headingText(lines []docLine, i int) string {
	if len(lines[i].text) > maxHeadingRunes*4 || utf8.RuneCountInString(lines[i].text) > maxHeadingRunes {
		return ""
	}
	return normalizeTitle(lines[i].text)
}

// sectionByTitle 在不按 Item 编排的年报正文里按标题找章节：从加粗或斜体、match 的标题行 start 到结束行 end（不含），
// 取正文最长的一段：目录、每页重复的同名页眉开始的段都更短，下一行是目录行的标题不算。endAt(start) 返回判断结束行的函数，
// 每个标题行只准备一次。每页的同名页眉和「(continued)」页眉不是结束行，和其他页眉页脚一样不放进正文。
// 找不到时 start 为 -1、text 为空字符串。
func sectionByTitle(lines []docLine, scanned []tenKLine, match func(header string) bool, endAt func(start int) func(i int) bool) (start, end int, text string) {
	var best []string
	start, bestRunes := -1, 0
	for from, l := range lines {
		if !l.emphasized || scanned[from].furniture {
			continue
		}
		header := headingText(lines, from)
		if header == "" || !match(header) {
			continue
		}
		// 目录里的标题：下一行是带页码的目录行（富国银行年报开头的「Financial Review」下面是「2 Overview 114 4 Equity Securities」）。
		if next := nextContentLine(scanned, from, len(lines)); next < len(lines) && tocLine.MatchString(lines[next].text) {
			continue
		}
		isEnd := endAt(from)
		section, n, i := []string{l.text}, utf8.RuneCountInString(l.text), from+1
		for ; i < len(lines); i++ {
			if continuedHeader(lines[i]) || (lines[i].emphasized && headingText(lines, i) == header) {
				continue
			}
			if isEnd(i) {
				break
			}
			if !scanned[i].furniture {
				section, n = append(section, lines[i].text), n+utf8.RuneCountInString(lines[i].text)
			}
		}
		if n > bestRunes {
			best, bestRunes, start, end = section, n, from, i
		}
	}
	return start, end, strings.Join(best, "\n")
}

// mdnaByTitle 在年报正文里按标题找管理层讨论与分析：从加粗或斜体的「Management's discussion and analysis」标题行
// 或「Financial Review」（富国银行年报里的 MD&A）
// （以冒号结尾的是目录）到下一个结束行（审计报告或管理层内部控制报告的标题或目录行、单独成行的 Risk Factors 标题、
// Item 8、SIGNATURES）。
// 找不到时 start 为 -1。
func mdnaByTitle(lines []docLine, scanned []tenKLine) (start, end int, text string) {
	// 结束行不要求加粗：目录里「Report of Independent Registered Public Accounting Firm 162」这样的行也结束目录开始的段。
	isEnd := func(i int) bool {
		if scanned[i].end || scanned[i].item == "8" {
			return true
		}
		t := headingText(lines, i)
		// 年报里风险因素可能在 MD&A 后面（花旗）或里面（富国银行的 Financial Review），单独成行的标题处结束，不重复摘要。
		return t != "" && (slices.ContainsFunc(mdnaEnds, func(end string) bool { return strings.HasPrefix(t, end) }) || lines[i].emphasized && t == "risk factors")
	}
	return sectionByTitle(lines, scanned, func(header string) bool {
		return header == "financial review" || strings.HasPrefix(header, "management's discussion and analysis") && !strings.HasSuffix(header, ":")
	}, func(int) func(int) bool { return isEnd })
}

// riskFactorsByTitle 在年报正文里按标题找风险因素：花旗的 10-K 正文不写 Item 编号，只在交叉索引表里列出页码，
// 风险因素是单独成行的「RISK FACTORS」一章。从这个标题行到下一个样式相同（加粗、斜体、是否全大写都相同）、
// 不是句子的标题行，或其他 Item 的标题、SIGNATURES（只有编号的 Item 1A 页眉不算）。风险类别（MARKET-RELATED RISKS）
// 和章节标题样式一样，但下一行是风险因素小标题（或断成两行的小标题），不算结束。
func riskFactorsByTitle(lines []docLine, scanned []tenKLine) (start, end int, text string) {
	return sectionByTitle(lines, scanned, func(header string) bool { return header == "risk factors" }, func(start int) func(int) bool {
		h := lines[start]
		caps := allCaps(h.text)
		return func(i int) bool {
			if scanned[i].end || (scanned[i].item != "" && scanned[i].item != "1a") {
				return true
			}
			l := lines[i]
			if scanned[i].furniture || !l.emphasized || l.bold != h.bold || l.italic != h.italic || headingText(lines, i) == "" ||
				endsSentence(l.text) || allCaps(l.text) != caps {
				return false
			}
			// 类别下一行是小标题；小标题断成两行时第一行加粗或斜体、不是句子，再看下一行。
			next := nextContentLine(scanned, i, len(lines))
			if next < len(lines) && lines[next].emphasized && !endsSentence(lines[next].text) {
				next = nextContentLine(scanned, next, len(lines))
			}
			if next == len(lines) {
				return true
			}
			_, title := riskTitle(lines[next])
			return !title
		}
	})
}

// nextContentLine 返回 i 之后、end 之前第一个不是页眉页脚的行，没有时返回 end。
func nextContentLine(scanned []tenKLine, i, end int) int {
	for i++; i < end && scanned[i].furniture; i++ {
	}
	return i
}

// allCaps 判断文字里的字母是否都是大写，没有字母时为 false。
func allCaps(text string) bool {
	return strings.ToUpper(text) == text && strings.ToLower(text) != text
}

// truncateLines 截取不超过 n 个字的开头部分，尽量按整行截；按整行截剩下不到一半时（后面是很长的一行）截这一行的开头。
func truncateLines(text string, n int) string {
	var b strings.Builder
	total := 0
	for i, line := range strings.Split(text, "\n") {
		sep := min(i, 1)
		k := utf8.RuneCountInString(line)
		if total+sep+k > n {
			if room := n - total - sep; total < n/2 && room > 0 {
				if sep > 0 {
					b.WriteByte('\n')
				}
				b.WriteString(string([]rune(line)[:room]))
			}
			return b.String()
		}
		if sep > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line)
		total += sep + k
	}
	return text
}

// tenKLine 是一行的分类结果。
type tenKLine struct {
	item      string // Item 标题或只有编号的 Item 页眉的编号（小写），都不是时为空
	title     string // Item 标题的文字，页眉为空
	end       bool   // SIGNATURES：章节到此为止，后面是签名和附件清单
	furniture bool   // 页眉页脚，不放进章节正文
}

// scanTenK 一次分类所有的行，findSection 只按分类查找，不再重复匹配。
func scanTenK(lines []string) []tenKLine {
	out := make([]tenKLine, len(lines))
	for i, line := range lines {
		l := &out[i]
		l.end = signatures.MatchString(line)
		l.furniture = pageFurniture(line)
		m := tenKHeading.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		l.item, l.title = strings.ToLower(m[1]), strings.TrimSpace(m[2])
		switch {
		// 编号和标题分成两行（<p>Item 1A.</p><p>Risk Factors</p>）：下一行是该 Item 的标准标题时才算标题；
		// 否则是只有编号的页眉（如微软每页的「Item 1A」），下一行是正文。
		case bareItem.MatchString(line):
			if i+1 < len(lines) && utf8.RuneCountInString(lines[i+1]) <= 100 && standardTitle(l.item, lines[i+1]) {
				l.title, l.furniture = lines[i+1], false
			}
		// 以「Item 8 of this Form 10-K」开头的长句不是标题；标题和正文在同一段时，标题必须是标准标题。
		case len(line) > maxHeadingBytes && !standardTitle(l.item, l.title):
			l.item, l.title = "", ""
		// 编号后面不是标准标题、标准标题在下一行（美国银行的「Item 7. Bank of America Corporation and Subsidiaries」
		// 下一行是「Management's Discussion and Analysis…」）。
		case !standardTitle(l.item, l.title) && i+1 < len(lines) && utf8.RuneCountInString(lines[i+1]) <= 100 && standardTitle(l.item, lines[i+1]):
			l.title = lines[i+1]
		}
	}
	markPageNumbers(lines, out)
	return out
}

// markPageNumbers 标出单独一行的页码。单独一行的数字在不用表格排版的申报里可能是正文里的金额，不能都删；
// 只有挨着其他页眉页脚（如特斯拉的「12」后面是「Table of Contents」），或者和前后单独一行的数字连成递增的页码
// （相差 1 或 2，摩根大通只在偶数页印页码）时才算页码。「Page 5」「5 of 120」这样写明的总是页码。
func markPageNumbers(lines []string, out []tenKLine) {
	type number struct{ line, value int }
	var numbers []number
	for i, line := range lines {
		if !pageNumber.MatchString(line) || out[i].item != "" {
			continue
		}
		value, err := strconv.Atoi(line)
		if err != nil {
			out[i].furniture = true // Page 5、5 of 120
			continue
		}
		numbers = append(numbers, number{i, value})
		if (i > 0 && out[i-1].furniture) || (i+1 < len(lines) && out[i+1].furniture) {
			out[i].furniture = true
		}
	}
	step := func(a, b int) bool { return b-a >= 1 && b-a <= 2 }
	for k, n := range numbers {
		if (k > 0 && step(numbers[k-1].value, n.value)) || (k+1 < len(numbers) && step(n.value, numbers[k+1].value)) {
			out[n.line].furniture = true
		}
	}
}

// standardTitle 判断 text 是否以 Item item 的标准标题开头。
func standardTitle(item, text string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	for _, title := range tenKTitles[item] {
		if strings.HasPrefix(text, title) {
			return true
		}
	}
	return false
}

// findSection 返回从标题包含 title 的 Item item 标题行到下一个其他 Item 的标题（或 SIGNATURES）之前的行范围 [start, end)，
// 找不到时 start 为 -1。
// 目录里也有同样的标题行，但它到下一个目录行之间几乎没有正文，所以取最长的一段；章节中间只有编号的页眉
// （如 Item 1A）和每页重复的完整页眉是同一个 Item，不会截断章节。用前缀和计算各段长度，每页都有完整页眉时也不会反复拼接。
func findSection(lines []string, scanned []tenKLine, item, title string) (start, end int) {
	// next[i] 是 i 及之后第一个结束章节的行：其他 Item 的标题或页眉，或 SIGNATURES。
	next := make([]int, len(lines)+1)
	next[len(lines)] = len(lines)
	for i := len(lines) - 1; i >= 0; i-- {
		next[i] = next[i+1]
		if l := scanned[i]; l.end || (l.item != "" && l.item != item) {
			next[i] = i
		}
	}
	// kept[i] 是前 i 行中正文的字数。
	kept := make([]int, len(lines)+1)
	for i, line := range lines {
		kept[i+1] = kept[i]
		if !scanned[i].furniture {
			kept[i+1] += utf8.RuneCountInString(line)
		}
	}
	start, best := -1, 0
	for i, l := range scanned {
		if l.item != item || !strings.Contains(strings.ToLower(l.title), title) {
			continue
		}
		if stop := next[i+1]; kept[stop]-kept[i] > best {
			start, end, best = i, stop, kept[stop]-kept[i]
		}
	}
	return start, end
}

// sectionText 返回 [start, end) 范围内去掉页眉页脚的正文，start 为 -1 时返回空字符串。
func sectionText(lines []string, scanned []tenKLine, start, end int) string {
	if start < 0 {
		return ""
	}
	var section []string
	for i := start; i < end; i++ {
		if !scanned[i].furniture {
			section = append(section, lines[i])
		}
	}
	return strings.Join(section, "\n")
}

// pageFurniture 判断一行是不是页眉页脚：Table of Contents、PART 标题、只有编号的 Item 页眉，以及以页码结尾或开头、
// 带 Form 10-K 字样的短行（如 Apple Inc. | 2026 Form 10-K | 5，摩根大通偶数页的 160 JPMorgan Chase & Co./2025 Form 10-K；
// 页码在开头时要求 Form 10-K 前面是年份，「1 See Note 2 of this Form 10-K」这样的脚注不算）。正文里提到「this Form 10-K.」的句子不算。
// 不按重复次数判断：MD&A 表格的表头（如 2026 Change 2025）也会反复出现，不能删。单独一行的页码见 scanTenK。
func pageFurniture(line string) bool {
	switch {
	case partHeader.MatchString(line), bareItem.MatchString(line), strings.EqualFold(line, "table of contents"):
		return true
	}
	return utf8.RuneCountInString(line) < 100 && formFooter.MatchString(line)
}

// tocLine 匹配带页码的目录行：以页码开头或结尾，如「2 Overview 114 4 Equity Securities」「RISK FACTORS 49」。
var tocLine = regexp.MustCompile(`^\d{1,3}\s+\S|\S\s+\d{1,3}$`)

// continuedHeader 判断一行是不是「Risk Factors (continued)」这样加粗或斜体、以 (continued) 结尾的页眉（富国银行的年报
// 每页都有）。只在按标题找的章节和风险因素里当页眉跳过，不算全局的页眉页脚：表格的续表标题也可能这样写。
func continuedHeader(line docLine) bool {
	return line.emphasized && utf8.RuneCountInString(line.text) < 100 && strings.HasSuffix(strings.ToLower(line.text), "(continued)")
}
