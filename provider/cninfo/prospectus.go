package cninfo

import (
	"html"
	"regexp"
	"strings"
)

const (
	KindProspectus  = "招股说明书"
	KindPreliminary = "招股意向书"
)

var (
	// titleQualifiers 是标题末尾括号里的说明，可能有好几个，如「（注册稿）（更新后）」「(更新后)」。
	titleQualifiers = regexp.MustCompile(`(\s*[（(][^（()）]*[)）])+$`)
	// titleMarkup 是巨潮在标题里加的高亮标签，如「<em>招股</em>」。
	titleMarkup = regexp.MustCompile(`<[^>]*>`)
)

// ProspectusKind 返回公告标题是哪种招股书全文：KindProspectus、KindPreliminary，都不是时返回空字符串。
// 先去掉巨潮的高亮标签和 HTML 实体，再去掉末尾括号里的说明，标题要以它们结尾：
// 「招股说明书提示性公告」「招股意向书附录」「招股说明书摘要」不是全文。
// sec ipo ask 和 Desktop 招股书问答都用它挑能问的文档，传原始标题即可。
func ProspectusKind(title string) string {
	title = strings.TrimSpace(html.UnescapeString(titleMarkup.ReplaceAllString(title, "")))
	title = strings.TrimSpace(titleQualifiers.ReplaceAllString(title, ""))
	for _, kind := range []string{KindProspectus, KindPreliminary} {
		if strings.HasSuffix(title, kind) {
			return kind
		}
	}
	return ""
}
