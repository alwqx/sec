// Package testpdf 为离线测试生成带中文 ToUnicode 映射的多页 PDF，不需要外部工具或真实公告。
package testpdf

import (
	"bytes"
	"fmt"
	"strings"
	"unicode/utf16"
)

// Build 每个字符串对应一页；空字符串表示无文本页。
func Build(pages ...string) []byte {
	var objects []string
	objects = append(objects, "<< /Type /Catalog /Pages 2 0 R >>", "", "<< /Type /Font /Subtype /Type0 /BaseFont /TestFont /Encoding /Identity-H /DescendantFonts [4 0 R] /ToUnicode 5 0 R >>", "<< /Type /Font /Subtype /CIDFontType2 /BaseFont /TestFont /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> >>")
	// 只映射测试文本实际用到的字符，覆盖中英文和数字，避免依赖系统字体。
	seen := map[uint16]bool{}
	var mapping strings.Builder
	for _, page := range pages {
		for _, code := range utf16.Encode([]rune(page)) {
			if !seen[code] {
				fmt.Fprintf(&mapping, "<%04X> <%04X>\n", code, code)
				seen[code] = true
			}
		}
	}
	cmap := fmt.Sprintf("/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n/CMapName /Test def\n/CMapType 2 def\n1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n%d beginbfchar\n%sendbfchar\nendcmap\nCMapName currentdict /CMap defineresource pop\nend\nend", len(seen), mapping.String())
	objects = append(objects, stream(cmap))
	var kids []string
	for _, page := range pages {
		n := len(objects) + 1
		kids = append(kids, fmt.Sprintf("%d 0 R", n))
		objects = append(objects, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 600 800] /Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>", n+1))
		var encoded strings.Builder
		for _, code := range utf16.Encode([]rune(page)) {
			fmt.Fprintf(&encoded, "%04X", code)
		}
		content := ""
		if page != "" {
			content = "BT /F1 12 Tf 50 750 Td <" + encoded.String() + "> Tj ET"
		}
		objects = append(objects, stream(content))
	}
	objects[1] = fmt.Sprintf("<< /Type /Pages /Count %d /Kids [%s] >>", len(pages), strings.Join(kids, " "))
	var out bytes.Buffer
	out.WriteString("%PDF-1.7\n")
	offsets := []int{0}
	for i, object := range objects {
		offsets = append(offsets, out.Len())
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&out, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref)
	return out.Bytes()
}

func stream(content string) string {
	return fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content)
}
