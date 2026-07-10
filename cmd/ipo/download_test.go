package ipo

import (
	"bytes"
	"strings"
	"testing"

	"github.com/alwqx/sec/provider/cninfo"
)

/* ------------------------------------------------------------------ */
/* promptSelection                                                     */
/* ------------------------------------------------------------------ */

func TestPromptSelection_DefaultEmptyInput(t *testing.T) {
	var out bytes.Buffer
	// 用户直接按回车 → 默认选第 1 条
	in := strings.NewReader("\n")
	got, err := promptSelection(&out, in, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0] != 0 {
		t.Fatalf("expected [0] (default=1), got %v", got)
	}
	// 检查提示信息已写入
	if !strings.Contains(out.String(), "请选择") {
		t.Fatalf("expected prompt in output, got: %s", out.String())
	}
}

func TestPromptSelection_QuitLower(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("q\n")
	got, err := promptSelection(&out, in, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for quit, got %v", got)
	}
}

func TestPromptSelection_QuitFullWord(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("quit\n")
	got, err := promptSelection(&out, in, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for quit, got %v", got)
	}
}

func TestPromptSelection_QuitCaseInsensitive(t *testing.T) {
	for _, input := range []string{"Q\n", "QUIT\n", "Quit\n", "qUiT\n"} {
		var out bytes.Buffer
		in := strings.NewReader(input)
		got, err := promptSelection(&out, in, 5)
		if err != nil {
			t.Fatalf("input %q: unexpected error: %v", input, err)
		}
		if got != nil {
			t.Fatalf("input %q: expected nil for quit, got %v", input, got)
		}
	}
}

func TestPromptSelection_AllLower(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("all\n")
	got, err := promptSelection(&out, in, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []int{0, 1, 2}
	if len(got) != len(expected) {
		t.Fatalf("expected %v, got %v", expected, got)
	}
	for i, v := range got {
		if v != expected[i] {
			t.Fatalf("expected %v, got %v", expected, got)
		}
	}
}

func TestPromptSelection_AllShortAlias(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("a\n")
	got, err := promptSelection(&out, in, 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []int{0, 1, 2, 3}
	if len(got) != len(expected) {
		t.Fatalf("expected %v, got %v", expected, got)
	}
	for i, v := range got {
		if v != expected[i] {
			t.Fatalf("expected %v, got %v", expected, got)
		}
	}
}

func TestPromptSelection_AllCaseInsensitive(t *testing.T) {
	for _, input := range []string{"ALL\n", "All\n", "aLl\n"} {
		var out bytes.Buffer
		in := strings.NewReader(input)
		got, err := promptSelection(&out, in, 2)
		if err != nil {
			t.Fatalf("input %q: unexpected error: %v", input, err)
		}
		expected := []int{0, 1}
		if len(got) != len(expected) {
			t.Fatalf("input %q: expected %v, got %v", input, expected, got)
		}
	}
}

func TestPromptSelection_AliasCaseInsensitive(t *testing.T) {
	for _, input := range []string{"A\n"} {
		var out bytes.Buffer
		in := strings.NewReader(input)
		got, err := promptSelection(&out, in, 2)
		if err != nil {
			t.Fatalf("input %q: unexpected error: %v", input, err)
		}
		expected := []int{0, 1}
		if len(got) != len(expected) {
			t.Fatalf("input %q: expected %v, got %v", input, expected, got)
		}
	}
}

func TestPromptSelection_SingleIndex(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("1\n")
	got, err := promptSelection(&out, in, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0] != 0 {
		t.Fatalf("expected [0] for index 1, got %v", got)
	}
}

func TestPromptSelection_SingleIndexMiddle(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("3\n")
	got, err := promptSelection(&out, in, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0] != 2 {
		t.Fatalf("expected [2] for index 3, got %v", got)
	}
}

func TestPromptSelection_MultipleIndices(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("1,3,5\n")
	got, err := promptSelection(&out, in, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []int{0, 2, 4}
	if len(got) != len(expected) {
		t.Fatalf("expected %v, got %v", expected, got)
	}
	for i, v := range got {
		if v != expected[i] {
			t.Fatalf("expected %v, got %v", expected, got)
		}
	}
}

func TestPromptSelection_IndicesWithSpaces(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader(" 1 ,  3  , 5 \n")
	got, err := promptSelection(&out, in, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []int{0, 2, 4}
	if len(got) != len(expected) {
		t.Fatalf("expected %v, got %v", expected, got)
	}
	for i, v := range got {
		if v != expected[i] {
			t.Fatalf("expected %v, got %v", expected, got)
		}
	}
}

func TestPromptSelection_OutOfRange(t *testing.T) {
	var out bytes.Buffer
	// max=3 → 只有序号 1,2,3 有效；10 越界
	in := strings.NewReader("10\n")
	_, err := promptSelection(&out, in, 3)
	if err == nil {
		t.Fatal("expected error for out-of-range index")
	}
}

func TestPromptSelection_NonNumeric(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("abc\n")
	_, err := promptSelection(&out, in, 5)
	if err == nil {
		t.Fatal("expected error for non-numeric input")
	}
}

func TestPromptSelection_AllWithMaxOne(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("all\n")
	got, err := promptSelection(&out, in, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []int{0}
	if len(got) != len(expected) || got[0] != expected[0] {
		t.Fatalf("expected %v, got %v", expected, got)
	}
}

func TestPromptSelection_TrailingSpaces(t *testing.T) {
	var out bytes.Buffer
	// "1  \n" → trim 后 "1" → index 0
	in := strings.NewReader("1  \n")
	got, err := promptSelection(&out, in, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0] != 0 {
		t.Fatalf("expected [0], got %v", got)
	}
}

func TestPromptSelection_ZeroNotAllowed(t *testing.T) {
	var out bytes.Buffer
	// 0 号不在 1-based 范围
	in := strings.NewReader("0\n")
	_, err := promptSelection(&out, in, 5)
	if err == nil {
		t.Fatal("expected error for index 0")
	}
}

/* ------------------------------------------------------------------ */
/* parseIndexList                                                      */
/* ------------------------------------------------------------------ */

func TestParseIndexList_Normal(t *testing.T) {
	got, err := parseIndexList("1,2,3", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []int{0, 1, 2}
	assertIntSlice(t, got, expected)
}

func TestParseIndexList_Single(t *testing.T) {
	got, err := parseIndexList("1", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []int{0}
	assertIntSlice(t, got, expected)
}

func TestParseIndexList_WithSpaces(t *testing.T) {
	got, err := parseIndexList(" 1 , 2 , 3 ", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []int{0, 1, 2}
	assertIntSlice(t, got, expected)
}

func TestParseIndexList_Deduplication(t *testing.T) {
	// "1,1,2" → dedup → [0,1]
	got, err := parseIndexList("1,1,2", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []int{0, 1}
	assertIntSlice(t, got, expected)
}

func TestParseIndexList_DeduplicationWithSpaces(t *testing.T) {
	got, err := parseIndexList("1, 2, 1, 3, 2", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []int{0, 1, 2}
	assertIntSlice(t, got, expected)
}

func TestParseIndexList_EmptyString(t *testing.T) {
	got, err := parseIndexList("", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty slice, got %v", got)
	}
}

func TestParseIndexList_OnlyCommas(t *testing.T) {
	got, err := parseIndexList(",,,", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty slice, got %v", got)
	}
}

func TestParseIndexList_ZeroOutOfRange(t *testing.T) {
	_, err := parseIndexList("0", 10)
	if err == nil {
		t.Fatal("expected error for index 0 (1-based)")
	}
}

func TestParseIndexList_OutOfRange(t *testing.T) {
	_, err := parseIndexList("10", 5)
	if err == nil {
		t.Fatal("expected error for out-of-range index")
	}
}

func TestParseIndexList_Negative(t *testing.T) {
	_, err := parseIndexList("-1", 10)
	if err == nil {
		t.Fatal("expected error for negative index")
	}
}

func TestParseIndexList_NonNumeric(t *testing.T) {
	_, err := parseIndexList("abc", 10)
	if err == nil {
		t.Fatal("expected error for non-numeric input")
	}
}

func TestParseIndexList_MixedValidInvalid(t *testing.T) {
	_, err := parseIndexList("1,abc,3", 10)
	if err == nil {
		t.Fatal("expected error for mixed valid/invalid")
	}
}

func TestParseIndexList_MaxBoundary(t *testing.T) {
	// max 有效值
	got, err := parseIndexList("5", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0] != 4 {
		t.Fatalf("expected [4], got %v", got)
	}

	// max+1 越界
	_, err = parseIndexList("6", 5)
	if err == nil {
		t.Fatal("expected error for max+1")
	}
}

func TestParseIndexList_MaintainsOrder(t *testing.T) {
	// 去重但保持首次出现顺序
	got, err := parseIndexList("3,1,2", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []int{2, 0, 1}
	assertIntSlice(t, got, expected)
}

/* ------------------------------------------------------------------ */
/* extractShortTitle                                                   */
/* ------------------------------------------------------------------ */

func TestExtractShortTitle_ProspectusFull(t *testing.T) {
	title := "首次公开发行股票并在创业板上市招股说明书"
	got := extractShortTitle(title)
	if got != "招股说明书" {
		t.Fatalf("expected '招股说明书', got %q", got)
	}
}

func TestExtractShortTitle_ProspectusIntent(t *testing.T) {
	title := "首次公开发行股票招股意向书"
	got := extractShortTitle(title)
	if got != "招股意向书" {
		t.Fatalf("expected '招股意向书', got %q", got)
	}
}

func TestExtractShortTitle_ListingNotice(t *testing.T) {
	title := "北京证券交易所上市公告书"
	got := extractShortTitle(title)
	if got != "上市公告书" {
		t.Fatalf("expected '上市公告书', got %q", got)
	}
}

func TestExtractShortTitle_IssueNotice(t *testing.T) {
	title := "发行公告"
	got := extractShortTitle(title)
	if got != "发行公告" {
		t.Fatalf("expected '发行公告', got %q", got)
	}
}

func TestExtractShortTitle_FallbackZhaoGu(t *testing.T) {
	title := "某某公司招股书及发行方案"
	got := extractShortTitle(title)
	if got != "招股书" {
		t.Fatalf("expected '招股书', got %q", got)
	}
}

func TestExtractShortTitle_NoKeyword(t *testing.T) {
	title := "短期融资券发行公告"
	// "发行公告" is in titleKeywords, so it should match
	got := extractShortTitle(title)
	if got != "发行公告" {
		t.Fatalf("expected '发行公告', got %q", got)
	}
}

func TestExtractShortTitle_NoKeywordFallback(t *testing.T) {
	title := "年度报告摘要"
	got := extractShortTitle(title)
	// no keywords match, title is short
	if got != "年度报告摘要" {
		t.Fatalf("expected '年度报告摘要', got %q", got)
	}
}

func TestExtractShortTitle_LongNoKeyword(t *testing.T) {
	title := "这是一个非常长的标题用来测试当标题中没有匹配任何关键词时的截断行为"
	got := extractShortTitle(title)
	runes := []rune(title)
	expected := string(runes[:15])
	if got != expected {
		t.Fatalf("expected first 15 runes (%q), got %q", expected, got)
	}
}

func TestExtractShortTitle_PriorityOrder(t *testing.T) {
	// 同时包含"招股说明书"和"上市公告书" → 优先返回靠前的 "招股说明书"
	title := "招股说明书及上市公告书"
	got := extractShortTitle(title)
	if got != "招股说明书" {
		t.Fatalf("expected '招股说明书' (higher priority), got %q", got)
	}
}

/* ------------------------------------------------------------------ */
/* sanitizeFilename                                                    */
/* ------------------------------------------------------------------ */

func TestSanitizeFilename_Normal(t *testing.T) {
	got := sanitizeFilename("300750_宁德时代_招股说明书_20180522")
	if got != "300750_宁德时代_招股说明书_20180522" {
		t.Fatalf("expected unchanged, got %q", got)
	}
}

func TestSanitizeFilename_Slashes(t *testing.T) {
	got := sanitizeFilename("a/b\\c")
	if got != "a_b_c" {
		t.Fatalf("expected 'a_b_c', got %q", got)
	}
}

func TestSanitizeFilename_Colon(t *testing.T) {
	got := sanitizeFilename("file:name")
	if got != "file_name" {
		t.Fatalf("expected 'file_name', got %q", got)
	}
}

func TestSanitizeFilename_WindowsIllegal(t *testing.T) {
	input := `test<value>with"chars|*?`
	got := sanitizeFilename(input)
	if strings.ContainsAny(got, `<>"|*?`) {
		t.Fatalf("illegal chars should be replaced, got %q", got)
	}
}

func TestSanitizeFilename_Newlines(t *testing.T) {
	got := sanitizeFilename("line1\nline2\rline3")
	if strings.Contains(got, "\n") || strings.Contains(got, "\r") {
		t.Fatalf("newlines should be removed, got %q", got)
	}
}

func TestSanitizeFilename_Tabs(t *testing.T) {
	got := sanitizeFilename("col1\tcol2")
	if strings.Contains(got, "\t") {
		t.Fatalf("tabs should be replaced with space, got %q", got)
	}
	if !strings.Contains(got, " ") {
		t.Fatalf("tab should become space, got %q", got)
	}
}

/* ------------------------------------------------------------------ */
/* buildFilename                                                       */
/* ------------------------------------------------------------------ */

func TestBuildFilename_Normal(t *testing.T) {
	a := &cninfo.Announcement{
		Title: "首次公开发行股票并在创业板上市招股说明书",
		Date:  "20180522",
	}
	got := buildFilename("300750", "宁德时代", a)
	expected := "300750_宁德时代_招股说明书_20180522.pdf"
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

func TestBuildFilename_NoDateButHasTime(t *testing.T) {
	a := &cninfo.Announcement{
		Title: "首次公开发行股票招股意向书",
		// Date 为空，但 Time 有值（毫秒时间戳对应 2018-05-22）
		Time: 1526947200000,
	}
	got := buildFilename("600036", "招商银行", a)
	// 时间戳毫秒 / 1000 = unix 秒 → 以字符串形式入文件名
	expected := "600036_招商银行_招股意向书"
	if !strings.HasPrefix(got, expected) {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

func TestBuildFilename_IllegalCharsInTitle(t *testing.T) {
	// 标题中的非法字符不会影响文件名（extractShortTitle 返回固定关键词，不含非法字符）
	a := &cninfo.Announcement{
		Title: "某公司:招股说明书/修订版",
		Date:  "20240101",
	}
	got := buildFilename("000001", "平安银行", a)
	if strings.ContainsAny(got, `/:<>"|*?`) {
		t.Fatalf("filename should not contain illegal chars, got %q", got)
	}
}

func TestBuildFilename_NormalTitle(t *testing.T) {
	a := &cninfo.Announcement{
		Title: "北京证券交易所上市公告书",
		Date:  "20231115",
	}
	got := buildFilename("830001", "某某生物", a)
	expected := "830001_某某生物_上市公告书_20231115.pdf"
	if got != expected {
		t.Fatalf("expected %q, got %q", expected, got)
	}
}

/* ------------------------------------------------------------------ */
/* filterValidPDFs                                                     */
/* ------------------------------------------------------------------ */

func makeAnnouncement(title, adjunctURL string, existFlag, invalidationFlag int) *cninfo.Announcement {
	return &cninfo.Announcement{
		Title:            title,
		AdjunctURL:       adjunctURL,
		ExistFlag:        existFlag,
		InvalidationFlag: invalidationFlag,
	}
}

func TestFilterValidPDFs_AllValid(t *testing.T) {
	input := []*cninfo.Announcement{
		makeAnnouncement("招股说明书", "/finalpage/a.pdf", 0, 0),
		makeAnnouncement("上市公告书", "/finalpage/b.pdf", 0, 0),
	}
	got := filterValidPDFs(input)
	if len(got) != 2 {
		t.Fatalf("expected 2 valid, got %d", len(got))
	}
}

func TestFilterValidPDFs_ExistFlag(t *testing.T) {
	input := []*cninfo.Announcement{
		makeAnnouncement("招股说明书", "/finalpage/a.pdf", 0, 0),
		makeAnnouncement("上市公告书", "/finalpage/b.pdf", 1, 0), // ExistFlag=1 应被过滤
	}
	got := filterValidPDFs(input)
	if len(got) != 1 {
		t.Fatalf("expected 1 valid after existFlag filter, got %d", len(got))
	}
	if got[0].Title != "招股说明书" {
		t.Fatalf("expected '招股说明书' to survive, got %q", got[0].Title)
	}
}

func TestFilterValidPDFs_InvalidationFlag(t *testing.T) {
	input := []*cninfo.Announcement{
		makeAnnouncement("招股说明书", "/finalpage/a.pdf", 0, 0),
		makeAnnouncement("旧版招股书", "/finalpage/b.pdf", 0, 1), // InvalidationFlag=1 应被过滤
	}
	got := filterValidPDFs(input)
	if len(got) != 1 {
		t.Fatalf("expected 1 valid after invalidationFlag filter, got %d", len(got))
	}
}

func TestFilterValidPDFs_EmptyAdjunctURL(t *testing.T) {
	input := []*cninfo.Announcement{
		makeAnnouncement("招股说明书", "/finalpage/a.pdf", 0, 0),
		makeAnnouncement("无附件公告", "", 0, 0), // AdjunctURL 为空 → 无法下载 → 过滤
	}
	got := filterValidPDFs(input)
	if len(got) != 1 {
		t.Fatalf("expected 1 valid after empty adjunctURL filter, got %d", len(got))
	}
}

func TestFilterValidPDFs_MixedInvalid(t *testing.T) {
	input := []*cninfo.Announcement{
		makeAnnouncement("A", "/a.pdf", 0, 0),
		makeAnnouncement("B", "/b.pdf", 1, 0), // existFlag
		makeAnnouncement("C", "/c.pdf", 0, 1), // invalidationFlag
		makeAnnouncement("D", "", 0, 0),       // empty adjunctURL
		makeAnnouncement("E", "/e.pdf", 1, 1), // both flags
	}
	got := filterValidPDFs(input)
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 valid, got %d", len(got))
	}
	if got[0].Title != "A" {
		t.Fatalf("expected 'A' to survive, got %q", got[0].Title)
	}
}

func TestFilterValidPDFs_Empty(t *testing.T) {
	got := filterValidPDFs(nil)
	if len(got) != 0 {
		t.Fatalf("expected empty slice for nil input, got %v", got)
	}
}

/* ------------------------------------------------------------------ */
/* helpers                                                             */
/* ------------------------------------------------------------------ */

func assertIntSlice(t *testing.T, got, expected []int) {
	t.Helper()
	if len(got) != len(expected) {
		t.Fatalf("length mismatch: expected %v, got %v", expected, got)
	}
	for i := range got {
		if got[i] != expected[i] {
			t.Fatalf("at index %d: expected %d, got %d (full: expected %v, got %v)",
				i, expected[i], got[i], expected, got)
		}
	}
}
