package pbc

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/PuerkitoBio/goquery"
)

type financingCell struct{ Amount, YoY *float64 }
type financingTable struct {
	Unit   string
	Values map[string]map[string]financingCell
	Names  map[string]string
	Notes  []string
	seen   map[string]bool
}

var monthPattern = regexp.MustCompile(`^(\d{4})[.年/\-](\d{1,2})月?$`)
var unitPattern = regexp.MustCompile(`单位[:：](万亿元|亿元)`)

// 注释编号只有一两位，不能匹配 2017.01 这样的月份行。
var numberedNotePattern = regexp.MustCompile(`^\d{1,2}[.、．]`)

// appendixPattern 匹配主表之后附带的口径调整表标题，如“表1：2017年以来各月完善后的社会融资规模存量余额”。
var appendixPattern = regexp.MustCompile(`^表\d+[:：]`)

func monthCode(text string) string {
	m := monthPattern.FindStringSubmatch(compact(text))
	if m == nil {
		return ""
	}
	n, _ := strconv.Atoi(m[2])
	if n < 1 || n > 12 {
		return ""
	}
	return fmt.Sprintf("%s%02d", m[1], n)
}

// maxGridCells 是展开后的总格数上限，真实的社融表不到 4 万格。
const maxGridCells = 200000

// tableGrid 展开 Excel 导出 HTML 的合并单元格，避免月份与金额/增速列错位。
func tableGrid(table *goquery.Selection) ([][]string, error) {
	cells := map[int]map[int]string{}
	width := 0
	var parseErr error
	count := 0
	// 总格数上限：单个单元格的跨行跨列都合法时，很多这样的单元格仍可能占用大量内存。
	total := 0
	table.Find("tr").Each(func(row int, tr *goquery.Selection) {
		count = row + 1
		if row > 2000 {
			parseErr = fmt.Errorf("统计表行数超过限制")
			return
		}
		if cells[row] == nil {
			cells[row] = map[int]string{}
		}
		column := 0
		tr.ChildrenFiltered("td,th").Each(func(_ int, td *goquery.Selection) {
			if parseErr != nil {
				return
			}
			for {
				if _, ok := cells[row][column]; !ok {
					break
				}
				column++
			}
			// Excel 导出的注释行会横跨两百多列；跨行没有这么多。上限分开，避免一个单元格展开过多格子。
			span := func(name string, limit int) int {
				value, ok := td.Attr(name)
				if !ok {
					return 1
				}
				n, err := strconv.Atoi(value)
				if err != nil || n < 1 || n > limit {
					parseErr = fmt.Errorf("统计表合并单元格无效")
					return 1
				}
				return n
			}
			rs, cs := span("rowspan", 100), span("colspan", 300)
			text := compact(td.Text())
			for r := row; r < row+rs; r++ {
				if cells[r] == nil {
					cells[r] = map[int]string{}
				}
				for c := column; c < column+cs; c++ {
					if _, ok := cells[r][c]; ok {
						parseErr = fmt.Errorf("统计表合并单元格重叠")
						return
					}
					if total++; total > maxGridCells {
						parseErr = fmt.Errorf("统计表单元格超过限制")
						return
					}
					cells[r][c] = text
				}
			}
			column += cs
			if column > width {
				width = column
			}
		})
	})
	if parseErr != nil {
		return nil, parseErr
	}
	if width > 500 {
		return nil, fmt.Errorf("统计表列数超过限制")
	}
	grid := make([][]string, count)
	for r := range grid {
		grid[r] = make([]string, width)
		for c, text := range cells[r] {
			grid[r][c] = text
		}
	}
	return grid, nil
}

// isMainHeader 判断是不是主表的表头行：增量表有“社会融资规模增量”列，存量表有“存量”列。
func isMainHeader(row []string) bool {
	for _, text := range row {
		if text == "社会融资规模增量" || text == "存量" {
			return true
		}
	}
	return false
}

// firstText 返回一行中第一个非空单元格，附表标题不一定在第一列。
func firstText(row []string) string {
	for _, text := range row {
		if text != "" {
			return text
		}
	}
	return ""
}

func number(text string) (*float64, error) {
	text = strings.ReplaceAll(compact(text), ",", "")
	text = strings.ReplaceAll(text, "，", "")
	switch text {
	case "", "--", "—", "-", "…", "...":
		return nil, nil
	}
	n, err := strconv.ParseFloat(text, 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		return nil, fmt.Errorf("非法社融数值: %q", text)
	}
	return &n, nil
}

func component(text string) (string, string) {
	text = compact(text)
	var label []rune
	for _, r := range text {
		if unicode.IsLetter(r) && r < 128 {
			break
		}
		label = append(label, r)
	}
	// 存量表把第一个分项写成“其中:人民币贷款”。
	name := strings.TrimSpace(string(label))
	name = strings.TrimPrefix(strings.TrimPrefix(name, "其中:"), "其中：")
	for _, pair := range []struct{ prefix, key string }{
		{"社会融资规模", "total"}, {"人民币贷款", "rmb-loans"}, {"外币贷款", "foreign-currency-loans"},
		{"委托贷款", "entrusted-loans"}, {"信托贷款", "trust-loans"}, {"未贴现银行承兑汇票", "undiscounted-acceptances"},
		{"企业债券", "corporate-bonds"}, {"政府债券", "government-bonds"}, {"地方政府专项债券", "local-government-special-bonds"},
		{"非金融企业境内股票", "domestic-equity"}, {"存款类金融机构资产支持证券", "depository-abs"}, {"贷款核销", "loan-writeoffs"},
	} {
		if strings.HasPrefix(name, pair.prefix) {
			return pair.key, name
		}
	}
	return "other:" + name, name
}

func parseFinancing(doc *goquery.Document, year int, kind string) (financingTable, error) {
	result := financingTable{Values: map[string]map[string]financingCell{}, Names: map[string]string{}}
	title := "社会融资规模增量统计表"
	if kind == "stock" {
		title = "社会融资规模存量统计表"
	}
	if !strings.Contains(compact(doc.Text()), title) {
		return result, fmt.Errorf("页面缺少 %s", title)
	}
	m := unitPattern.FindStringSubmatch(compact(doc.Text()))
	if m == nil {
		return result, fmt.Errorf("无法识别社融单位")
	}
	result.Unit = m[1]
	grid, err := tableGrid(doc.Find("table").First())
	if err != nil {
		return result, err
	}
	// 部分年份在同一张表里附带口径调整后的历史表，其中又有本年月份；只解析主表，用当时公布的数据。
	// 读到主表表头之后才按附表标题截断，主表标题本身以“表1：”开头时不受影响。
	header := false
	for i, row := range grid {
		if !header {
			header = isMainHeader(row)
			continue
		}
		if appendixPattern.MatchString(firstText(row)) {
			grid = grid[:i]
			result.Notes = append(result.Notes, "页面附带的口径调整历史表未采用，数值为当时公布的主表")
			break
		}
	}
	if len(grid) == 0 {
		return result, fmt.Errorf("社融表为空")
	}
	for _, row := range grid {
		if len(row) > 0 && (strings.HasPrefix(row[0], "注") || numberedNotePattern.MatchString(row[0])) {
			// 合并单元格已展开，注释文本只保留一次。
			var parts []string
			for _, text := range row {
				if text != "" && (len(parts) == 0 || parts[len(parts)-1] != text) {
					parts = append(parts, text)
				}
			}
			result.Notes = append(result.Notes, strings.Join(parts, " "))
		}
	}
	if kind == "flow" {
		err = parseFlowGrid(grid, year, &result)
	} else {
		err = parseStockGrid(grid, year, &result)
	}
	if err != nil {
		return result, err
	}
	if len(result.Values) == 0 {
		return result, fmt.Errorf("社融表未包含 %d 年月度数据", year)
	}
	if _, ok := result.Names["total"]; !ok {
		return result, fmt.Errorf("社融表缺少总量数据行")
	}
	return result, nil
}

func putCell(result *financingTable, period, key, name string, value *float64, yoy bool) error {
	if result.seen == nil {
		result.seen = map[string]bool{}
	}
	seenKey := period + "|" + key + "|" + strconv.FormatBool(yoy)
	if result.seen[seenKey] {
		return fmt.Errorf("社融观测重复: %s %s", period, name)
	}
	result.seen[seenKey] = true
	if result.Values[period] == nil {
		result.Values[period] = map[string]financingCell{}
	}
	cell := result.Values[period][key]
	if yoy {
		if cell.YoY != nil {
			return fmt.Errorf("社融增速重复: %s %s", period, name)
		}
		cell.YoY = value
	} else {
		if cell.Amount != nil {
			return fmt.Errorf("社融金额重复: %s %s", period, name)
		}
		cell.Amount = value
	}
	result.Values[period][key] = cell
	result.Names[key] = name
	return nil
}

func parseFlowGrid(grid [][]string, year int, result *financingTable) error {
	var headers []string
	for _, row := range grid {
		for _, cell := range row {
			if cell == "社会融资规模增量" {
				if headers != nil {
					return fmt.Errorf("社融增量表头重复")
				}
				headers = row
				break
			}
		}
	}
	if headers == nil {
		return fmt.Errorf("社融增量表缺少总量列")
	}
	seen := map[string]bool{}
	for _, row := range grid {
		period := monthCode(row[0])
		if period == "" || period[:4] != strconv.Itoa(year) {
			continue
		}
		if seen[period] {
			return fmt.Errorf("社融增量月份重复: %s", period)
		}
		seen[period] = true
		for col, label := range headers {
			if col == 0 || label == "" {
				continue
			}
			key, name := component(label)
			if name == "" {
				continue
			}
			value, err := number(row[col])
			if err != nil {
				return err
			}
			if err := putCell(result, period, key, name, value, false); err != nil {
				return err
			}
		}
	}
	return nil
}

func parseStockGrid(grid [][]string, year int, result *financingTable) error {
	var months, measures []string
	for _, row := range grid {
		isMonth := false
		isMeasure := false
		for _, cell := range row {
			if code := monthCode(cell); code != "" && code[:4] == strconv.Itoa(year) {
				isMonth = true
			}
			if cell == "存量" {
				isMeasure = true
			}
		}
		if isMonth {
			months = row
		}
		if isMeasure {
			measures = row
		}
	}
	if months == nil || measures == nil {
		return fmt.Errorf("社融存量表缺少月份或增速表头")
	}
	for _, row := range grid {
		if row[0] == "" || strings.HasPrefix(row[0], "注") || numberedNotePattern.MatchString(row[0]) {
			continue
		}
		key, name := component(row[0])
		if name == "" {
			continue
		}
		// 数据行必须有中文项目名称；英文行和其他表头不当作融资结构。
		if strings.HasPrefix(key, "other:") {
			hasHan := false
			for _, r := range name {
				if unicode.Is(unicode.Han, r) {
					hasHan = true
				}
			}
			if !hasHan || strings.HasPrefix(name, "项目") || strings.HasPrefix(name, "单位") {
				continue
			}
		}
		if key == "total" && name != "社会融资规模存量" {
			continue
		}
		for col := 1; col < len(months); col++ {
			period := monthCode(months[col])
			if period == "" || period[:4] != strconv.Itoa(year) {
				continue
			}
			measure := measures[col]
			if measure != "存量" && !strings.Contains(measure, "增速") {
				continue
			}
			value, err := number(row[col])
			if err != nil {
				return err
			}
			if err := putCell(result, period, key, name, value, measure != "存量"); err != nil {
				return err
			}
		}
	}
	return nil
}
