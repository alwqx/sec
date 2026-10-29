package fred

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alwqx/sec/utils"
)

const gdpVintagesURL = "https://apps.bea.gov/national/xls/gdp-gdi-vintage-history.xlsx"

// GDPVintage 是BEA发布档案中的一个版本。档案没有实际GDP水平，不能将名义金额或最新水平填入RealLevel。
type GDPVintage struct {
	Estimate         string     `json:"estimate"`
	ReleaseDate      string     `json:"release_date"`
	PublishedAt      *time.Time `json:"published_at"`
	RealLevel        *float64   `json:"real_level"`
	NominalLevel     *float64   `json:"nominal_level"`
	QoQAnnualized    *float64   `json:"qoq_annualized_percent"`
	RawNominalLevel  string     `json:"raw_nominal_level"`
	RawQoQAnnualized string     `json:"raw_qoq_annualized"`
	RawRelease       string     `json:"raw_release"`
	Note             string     `json:"note,omitempty"`
}

type gdpVintageCache struct {
	Source    string    `json:"source"`
	FetchedAt time.Time `json:"fetched_at"`
	Workbook  []byte    `json:"workbook"`
}

// fetchGDPVintages 的缓存与当前水平CSV独立；显式要求版本时，来源故障必须报错。
func fetchGDPVintages(ctx context.Context, cacheDir string, refresh bool, endpoint string) (map[string][]GDPVintage, time.Time, error) {
	if err := ctx.Err(); err != nil {
		return nil, time.Time{}, err
	}
	cachePath := filepath.Join(cacheDir, "gdp-vintages.json")
	if cacheDir != "" && !refresh {
		if raw, err := os.ReadFile(cachePath); err == nil {
			var saved gdpVintageCache
			if json.Unmarshal(raw, &saved) == nil && saved.Source == endpoint && time.Since(saved.FetchedAt) >= 0 && time.Since(saved.FetchedAt) < 6*time.Hour {
				if values, err := parseGDPVintages(saved.Workbook); err == nil {
					return values, saved.FetchedAt, nil
				}
			}
		}
	}
	resp, err := utils.MakeRequest(ctx, http.MethodGet, endpoint, nil, nil, 30*time.Second)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("下载BEA GDP发布档案失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, time.Time{}, fmt.Errorf("BEA GDP发布档案返回HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
	if err != nil {
		return nil, time.Time{}, err
	}
	if len(raw) > 8<<20 {
		return nil, time.Time{}, fmt.Errorf("BEA GDP发布档案超过大小限制")
	}
	values, err := parseGDPVintages(raw)
	if err != nil {
		return nil, time.Time{}, err
	}
	if err := ctx.Err(); err != nil {
		return nil, time.Time{}, err
	}
	fetched := time.Now().UTC()
	if cacheDir != "" {
		if err := os.MkdirAll(cacheDir, 0755); err != nil {
			return nil, time.Time{}, err
		}
		data, err := json.Marshal(gdpVintageCache{endpoint, fetched, raw})
		if err != nil {
			return nil, time.Time{}, err
		}
		file, err := os.CreateTemp(cacheDir, ".gdp-vintages-*")
		if err != nil {
			return nil, time.Time{}, err
		}
		defer os.Remove(file.Name())
		_, writeErr := file.Write(data)
		closeErr := file.Close()
		if writeErr != nil {
			return nil, time.Time{}, writeErr
		}
		if closeErr != nil {
			return nil, time.Time{}, closeErr
		}
		if err := os.Rename(file.Name(), cachePath); err != nil {
			return nil, time.Time{}, err
		}
	}
	return values, fetched, nil
}

type gdpXMLText struct{ Text string }

// Excel共享字符串可能使用富文本；顺序拼接每个t节点，而非只读取第一个片段。
func (s *gdpXMLText) UnmarshalXML(decoder *xml.Decoder, start xml.StartElement) error {
	var text strings.Builder
	depth := 1
	for depth > 0 {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		switch node := token.(type) {
		case xml.StartElement:
			if node.Name.Local == "t" {
				var part string
				if err := decoder.DecodeElement(&part, &node); err != nil {
					return err
				}
				text.WriteString(part)
			} else {
				depth++
			}
		case xml.EndElement:
			depth--
		}
	}
	s.Text = text.String()
	return nil
}

type gdpXMLCell struct {
	Ref    string     `xml:"r,attr"`
	Type   string     `xml:"t,attr"`
	Value  string     `xml:"v"`
	Inline gdpXMLText `xml:"is"`
}

var gdpReleaseDate = regexp.MustCompile(`^([A-Z][a-z]{2} [0-9]{1,2}, [0-9]{4})(?:\s|$)`)

// parseGDPVintages 只读取官方GDP档案的季度、名义金额、实际增速和发布日期，明确校验列含义。
func parseGDPVintages(raw []byte) (map[string][]GDPVintage, error) {
	book, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("读取BEA GDP发布档案失败: %w", err)
	}
	read := func(name string, target any) error {
		var entry *zip.File
		for _, file := range book.File {
			if file.Name == name {
				if entry != nil {
					return fmt.Errorf("BEA档案XML重复: %s", name)
				}
				entry = file
			}
		}
		if entry == nil {
			return fmt.Errorf("BEA档案缺少XML: %s", name)
		}
		if entry.UncompressedSize64 > 8<<20 {
			return fmt.Errorf("BEA档案XML超过大小限制: %s", name)
		}
		reader, err := entry.Open()
		if err != nil {
			return err
		}
		defer reader.Close()
		return xml.NewDecoder(io.LimitReader(reader, (8<<20)+1)).Decode(target)
	}
	var workbook struct {
		Sheets []struct {
			Name string `xml:"name,attr"`
			ID   string `xml:"id,attr"`
		} `xml:"sheets>sheet"`
	}
	if err := read("xl/workbook.xml", &workbook); err != nil {
		return nil, err
	}
	var relationship string
	for _, sheet := range workbook.Sheets {
		if sheet.Name == "Vintage History" {
			relationship = sheet.ID
		}
	}
	if relationship == "" {
		return nil, fmt.Errorf("BEA档案缺少Vintage History工作表")
	}
	var relationships struct {
		Items []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	if err := read("xl/_rels/workbook.xml.rels", &relationships); err != nil {
		return nil, err
	}
	var sheetPath string
	for _, rel := range relationships.Items {
		if rel.ID == relationship {
			sheetPath = path.Join("xl", rel.Target)
			if strings.HasPrefix(rel.Target, "/") {
				sheetPath = strings.TrimPrefix(rel.Target, "/")
			}
		}
	}
	if !strings.HasPrefix(sheetPath, "xl/worksheets/") || !strings.HasSuffix(sheetPath, ".xml") {
		return nil, fmt.Errorf("BEA档案工作表路径无效")
	}
	var shared struct {
		Items []gdpXMLText `xml:"si"`
	}
	if err := read("xl/sharedStrings.xml", &shared); err != nil {
		return nil, err
	}
	var sheet struct {
		Rows []struct {
			Cells []gdpXMLCell `xml:"c"`
		} `xml:"sheetData>row"`
	}
	if err := read(sheetPath, &sheet); err != nil {
		return nil, err
	}
	values := map[string][]GDPVintage{}
	seen := map[string]bool{}
	quarter := ""
	header := false
	units := false
	percent := false
	title := false
	for _, row := range sheet.Rows {
		cells := map[string]string{}
		for _, cell := range row.Cells {
			column := strings.TrimRight(cell.Ref, "0123456789")
			if column == "" || column == cell.Ref {
				return nil, fmt.Errorf("BEA档案单元格位置无效: %s", cell.Ref)
			}
			if _, ok := cells[column]; ok {
				return nil, fmt.Errorf("BEA档案单元格列重复: %s", cell.Ref)
			}
			value := cell.Value
			switch cell.Type {
			case "s":
				index, err := strconv.Atoi(value)
				if err != nil || index < 0 || index >= len(shared.Items) {
					return nil, fmt.Errorf("BEA档案共享字符串索引无效")
				}
				value = shared.Items[index].Text
			case "inlineStr":
				value = cell.Inline.Text
			case "", "n", "str":
			default:
				return nil, fmt.Errorf("BEA档案单元格类型无效: %s", cell.Type)
			}
			cells[column] = strings.TrimSpace(value)
		}
		if strings.Contains(cells["A"], "Vintage History of Quarterly Gross Domestic Product (GDP)") {
			title = true
		}
		if _, err := gdpQuarter(cells["A"]); err == nil {
			quarter = cells["A"]
			if quarter < "2002Q1" {
				return nil, fmt.Errorf("BEA档案出现未支持的历史GDP季度: %s", quarter)
			}
			header = false
			units = false
			percent = false
			continue
		}
		if cells["E"] == "Percent Change" {
			percent = true
			continue
		}
		if cells["B"] == "Vintage" {
			units = cells["C"] == "Billions of Current Dollars" && cells["E"] == "from Preceding Period"
			continue
		}
		if cells["C"] == "GDP" && cells["E"] == "Real GDP" && cells["G"] == "Release Date" {
			header = units && percent
			continue
		}
		if quarter == "" || cells["B"] == "" || cells["B"] == "Vintage" {
			continue
		}
		if !header {
			return nil, fmt.Errorf("BEA档案GDP列含义无法确认: %s", quarter)
		}
		match := gdpReleaseDate.FindStringSubmatch(cells["G"])
		if match == nil {
			return nil, fmt.Errorf("BEA档案发布日期无效: %q", cells["G"])
		}
		date, err := time.Parse("Jan 2, 2006", match[1])
		if err != nil {
			return nil, fmt.Errorf("BEA档案发布日期无效: %w", err)
		}
		item := GDPVintage{Estimate: cells["B"], ReleaseDate: date.Format("2006-01-02"), RawNominalLevel: cells["C"], RawQoQAnnualized: cells["E"], RawRelease: cells["G"], Note: strings.TrimSpace(strings.TrimPrefix(cells["G"], match[1]))}
		for _, field := range []struct {
			raw    string
			target **float64
			level  bool
		}{{item.RawNominalLevel, &item.NominalLevel, true}, {item.RawQoQAnnualized, &item.QoQAnnualized, false}} {
			text := strings.TrimSpace(field.raw)
			if text == "" || strings.Trim(text, ".") == "" {
				continue
			}
			number, err := strconv.ParseFloat(strings.ReplaceAll(text, ",", ""), 64)
			if err != nil || math.IsNaN(number) || math.IsInf(number, 0) || field.level && number < 0 {
				return nil, fmt.Errorf("BEA档案GDP数值无效: %q", field.raw)
			}
			*field.target = &number
		}
		if item.NominalLevel == nil || item.QoQAnnualized == nil {
			return nil, fmt.Errorf("BEA档案GDP发布记录不完整: %s %s", quarter, item.ReleaseDate)
		}
		key := quarter + item.ReleaseDate
		if seen[key] {
			return nil, fmt.Errorf("BEA档案GDP发布记录重复: %s", key)
		}
		seen[key] = true
		values[quarter] = append(values[quarter], item)
	}
	if !title || len(values) == 0 {
		return nil, fmt.Errorf("BEA档案未返回有效GDP版本记录")
	}
	for quarter := range values {
		sort.Slice(values[quarter], func(i, j int) bool { return values[quarter][i].ReleaseDate < values[quarter][j].ReleaseDate })
	}
	return values, nil
}
