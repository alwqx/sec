package edgar

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// minSectionRunes 是可以摘要的章节最少字数。更短的通常是「不适用」（较小的申报公司不必披露风险因素）
// 或只是一句引用（银行等公司的 MD&A 在年报其他部分，Item 7 只写「见第 X 页」）。
const minSectionRunes = 1500

var (
	tenKHeading = regexp.MustCompile(`(?i)^item\s*(\d{1,2}[a-c]?)\b[\s.:—–-]*(.*)$`)
	// 页眉页脚：页码、目录链接、PART 标题、只有编号的 Item 页眉（如 Item 1A、Item 1B, 1C）。
	pageNumber   = regexp.MustCompile(`(?i)^(page\s+)?\d{1,3}(\s+of\s+\d{1,3})?$`)
	partHeader   = regexp.MustCompile(`(?i)^parts?\s+[ivx]+(\s*(,|and|&)\s*[ivx]+)*$`)
	bareItem     = regexp.MustCompile(`(?i)^item\s*\d{1,2}[a-c]?(\s*,\s*\d{1,2}[a-c]?)*$`)
	formFooter   = regexp.MustCompile(`(?i)form\s+10-k`)
	tenKSections = []struct {
		item, title, label, short string
	}{
		{"1A", "risk factors", "Item 1A", "Item 1A 风险因素正文只有 %d 字，可能不适用（如较小的申报公司）或只是引用其他部分，未参与摘要。"},
		{"7", "management", "Item 7", "Item 7 管理层讨论与分析正文只有 %d 字，可能只是引用年报的其他部分（银行等公司常见），未参与摘要。"},
	}
)

// Read10K 读取一份 10-K（含 10-K/A、过渡期年报 10-KT）主文件里的 Item 1A 风险因素和 Item 7 管理层讨论与分析，
// 各为一节，去掉目录、页眉页脚和页码。找不到或太短的章节在 Notes 里说明；两节都没有时报错，
// 比如只补充第三部分的 10-K/A。raw 是该申报目录下任意文件的地址。
func (c *Client) Read10K(ctx context.Context, raw string) (*FilingDocument, error) {
	doc, _, text, err := c.readPrimary(ctx, raw, []string{"10-K", "10-KT"}, "10-K")
	if err != nil {
		return nil, err
	}
	lines := strings.Split(text, "\n")
	total := 0
	for _, want := range tenKSections {
		section := findSection(lines, want.item, want.title)
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

// findSection 返回从标题包含 title 的 Item item 标题行到下一个其他 Item 标题之前的正文，去掉页眉页脚。
// 目录里也有同样的标题行，但它到下一个目录行之间几乎没有正文，所以取最长的一段；
// 章节中间只有编号的页眉（如 Item 1A）是同一个 Item，不会截断章节。
func findSection(lines []string, item, title string) string {
	best, bestRunes := "", 0
	for i, line := range lines {
		m := tenKHeading.FindStringSubmatch(line)
		if m == nil || len(line) > 200 || !strings.EqualFold(m[1], item) || !strings.Contains(strings.ToLower(m[2]), title) {
			continue
		}
		var kept []string
		for _, next := range lines[i:] {
			if h := tenKHeading.FindStringSubmatch(next); h != nil && len(next) <= 200 && !strings.EqualFold(h[1], item) {
				break
			}
			if !pageFurniture(next) {
				kept = append(kept, next)
			}
		}
		section := strings.Join(kept, "\n")
		if n := utf8.RuneCountInString(section); n > bestRunes {
			best, bestRunes = section, n
		}
	}
	return best
}

// pageFurniture 判断一行是不是页眉页脚：页码、Table of Contents、PART 标题、只有编号的 Item 页眉，
// 以及带 Form 10-K 字样的短行（如 Apple Inc. | 2026 Form 10-K | 5）。不按重复次数判断：
// MD&A 表格的表头（如 2026 Change 2025）也会反复出现，不能删。
func pageFurniture(line string) bool {
	switch {
	case pageNumber.MatchString(line), partHeader.MatchString(line), bareItem.MatchString(line):
		return true
	case strings.EqualFold(line, "table of contents"):
		return true
	}
	return utf8.RuneCountInString(line) < 100 && formFooter.MatchString(line)
}
