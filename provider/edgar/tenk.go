package edgar

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
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
	formFooter = regexp.MustCompile(`(?i)form\s+10-k\b.*\b\d{1,3}$`)
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
	tenKSections = []struct {
		item, title, label, short string
	}{
		{"1a", "risk factors", "Item 1A", "Item 1A 风险因素正文只有 %d 字，可能不适用（如较小的申报公司）或只是引用其他部分，未参与摘要。"},
		{"7", "management", "Item 7", "Item 7 管理层讨论与分析正文只有 %d 字，可能只是引用年报的其他部分（银行等公司常见），未参与摘要。"},
	}
)

// Read10K 读取一份 10-K（含 10-K/A、过渡期年报 10-KT）主文件里的 Item 1A 风险因素和 Item 7 管理层讨论与分析，
// 各为一节，去掉目录、页眉页脚和页码。找不到或太短的章节在 Notes 里说明；两节都没有时报错，
// 比如只补充第三部分的 10-K/A。raw 是该申报目录下任意文件的地址。
func (c *Client) Read10K(ctx context.Context, raw string) (*FilingDocument, error) {
	doc, _, text, err := c.readPrimary(ctx, raw, "10-K", "10-KT")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(text, "\n")
	scanned := scanTenK(lines)
	total := 0
	for _, want := range tenKSections {
		section := findSection(lines, scanned, want.item, want.title)
		n := utf8.RuneCountInString(section)
		switch {
		case section == "":
			doc.Notes = append(doc.Notes, fmt.Sprintf("10-K 正文里没有找到 %s。", want.label))
		case n < minSectionRunes:
			doc.Notes = append(doc.Notes, fmt.Sprintf(want.short, n))
		default:
			total += n
			doc.Sections = append(doc.Sections, DocumentSection{Label: want.label, URL: doc.URL, Text: section})
		}
	}
	if len(doc.Sections) == 0 {
		return nil, fmt.Errorf("%w：10-K 正文里没有可摘要的 Item 1A 或 Item 7", ErrDocument)
	}
	if total > MaxDocumentRunes {
		return nil, fmt.Errorf("%w：10-K 的 Item 1A 和 Item 7 超过 %d 字", ErrDocument, MaxDocumentRunes)
	}
	return doc, nil
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

// findSection 返回从标题包含 title 的 Item item 标题行到下一个其他 Item 的标题（或 SIGNATURES）之前的正文，去掉页眉页脚。
// 目录里也有同样的标题行，但它到下一个目录行之间几乎没有正文，所以取最长的一段；章节中间只有编号的页眉
// （如 Item 1A）和每页重复的完整页眉是同一个 Item，不会截断章节。用前缀和计算各段长度，每页都有完整页眉时也不会反复拼接。
func findSection(lines []string, scanned []tenKLine, item, title string) string {
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
	start, end, best := -1, 0, 0
	for i, l := range scanned {
		if l.item != item || !strings.Contains(strings.ToLower(l.title), title) {
			continue
		}
		if stop := next[i+1]; kept[stop]-kept[i] > best {
			start, end, best = i, stop, kept[stop]-kept[i]
		}
	}
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

// pageFurniture 判断一行是不是页眉页脚：Table of Contents、PART 标题、只有编号的 Item 页眉，以及以页码结尾、
// 带 Form 10-K 字样的短行（如 Apple Inc. | 2026 Form 10-K | 5）。正文里提到「this Form 10-K.」的句子不算。
// 不按重复次数判断：MD&A 表格的表头（如 2026 Change 2025）也会反复出现，不能删。单独一行的页码见 scanTenK。
func pageFurniture(line string) bool {
	switch {
	case partHeader.MatchString(line), bareItem.MatchString(line), strings.EqualFold(line, "table of contents"):
		return true
	}
	return utf8.RuneCountInString(line) < 100 && formFooter.MatchString(line)
}
