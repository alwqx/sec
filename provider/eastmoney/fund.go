package eastmoney

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

var (
	// fundBasicURL 是天天基金 App 的基金基本信息接口，不带 App 的参数时返回「网络繁忙」。
	fundBasicURL = "https://fundmobapi.eastmoney.com/FundMNewApi/FundMNBasicInformation"
	// fundArchiveURL 是天天基金网页的基金档案（基本概况）页。
	fundArchiveURL = "https://fundf10.eastmoney.com/jbgk_%s.html"
)

// FundBasic 是天天基金 App 接口的基金基本信息。上游没有的值是空字符串或 nil。
// 货币基金的 NAV 是每万份收益（元），AccNAV 是七日年化收益率（%），DayChange 为 nil。
type FundBasic struct {
	Name      string   // 基金简称
	Type      string   // 基金类型，如 混合型-偏股、货币型-普通货币
	Company   string   // 基金管理人
	Managers  string   // 基金经理，多人用逗号分隔
	NAV       *float64 // 单位净值
	AccNAV    *float64 // 累计净值
	NAVDate   string   // 净值日期 2026-11-05
	DayChange *float64 // 日涨幅（%）
	NetAssets *float64 // 净资产（元）
	RiskLevel string   // 风险等级：低、中低、中、中高、高
	Listed    bool     // 是否场内交易
	Returns   FundReturns
}

// FundReturns 是阶段收益率（%），没有时为 nil。
type FundReturns struct {
	Week        *float64 `json:"1w"`
	Month       *float64 `json:"1m"`
	Quarter     *float64 `json:"3m"`
	HalfYear    *float64 `json:"6m"`
	Year        *float64 `json:"1y"`
	TwoYears    *float64 `json:"2y"`
	ThreeYears  *float64 `json:"3y"`
	YearToDate  *float64 `json:"ytd"`
	SinceLaunch *float64 `json:"since_launch"`
}

// riskLevels 是天天基金风险等级编号的名称。
var riskLevels = map[string]string{"1": "低", "2": "中低", "3": "中", "4": "中高", "5": "高"}

// GetFundBasic 查询基金基本信息，code 是 6 位基金代码。没有这只基金时返回 nil 和 nil。
func GetFundBasic(ctx context.Context, code string) (*FundBasic, error) {
	params := url.Values{}
	params.Set("FCODE", code)
	params.Set("deviceid", "sec")
	params.Set("plat", "Iphone")
	params.Set("product", "EFund")
	params.Set("version", "6.4.4")
	body, err := getBody(ctx, fundBasicURL+"?"+params.Encode())
	if err != nil {
		return nil, err
	}
	var resp struct {
		Datas   map[string]any `json:"Datas"`
		Success bool           `json:"Success"`
		ErrMsg  string         `json:"ErrMsg"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal fund basic: %w", err)
	}
	if !resp.Success {
		return nil, fmt.Errorf("fund basic API error: %s", resp.ErrMsg)
	}
	d := resp.Datas
	if d == nil {
		return nil, nil
	}
	b := &FundBasic{
		Name: fundText(d, "SHORTNAME"), Type: fundText(d, "FTYPE"), Company: fundText(d, "JJGS"),
		Managers: fundText(d, "JJJL"), NAV: fundNumber(d, "DWJZ"), AccNAV: fundNumber(d, "LJJZ"),
		NAVDate: fundText(d, "FSRQ"), DayChange: fundNumber(d, "RZDF"), NetAssets: fundNumber(d, "ENDNAV"),
		RiskLevel: riskLevels[fundText(d, "RISKLEVEL")], Listed: fundText(d, "ISEXCHG") == "1",
		Returns: FundReturns{
			Week: fundNumber(d, "SYL_Z"), Month: fundNumber(d, "SYL_Y"), Quarter: fundNumber(d, "SYL_3Y"),
			HalfYear: fundNumber(d, "SYL_6Y"), Year: fundNumber(d, "SYL_1N"), TwoYears: fundNumber(d, "SYL_2N"),
			ThreeYears: fundNumber(d, "SYL_3N"), YearToDate: fundNumber(d, "SYL_JN"), SinceLaunch: fundNumber(d, "SYL_LN"),
		},
	}
	return b, nil
}

// fundText 读取字段的文本，「--」或缺失表示没有，返回空字符串。接口一般把数值也写成字符串，
// 写成 JSON 数字或布尔值时也按文本读，不当作缺失。
func fundText(d map[string]any, key string) string {
	var text string
	switch value := d[key].(type) {
	case nil:
		return ""
	case string:
		text = value
	case float64:
		text = strconv.FormatFloat(value, 'f', -1, 64)
	default:
		text = fmt.Sprint(value)
	}
	return emptyIfDash(strings.TrimSpace(text))
}

// emptyIfDash 把表示没有数据的「--」「---」换成空字符串。
func emptyIfDash(text string) string {
	if strings.Trim(text, "-") == "" {
		return ""
	}
	return text
}

// fundNumber 读取写成字符串的数值字段，「--」或空表示没有，返回 nil。
func fundNumber(d map[string]any, key string) *float64 {
	value, err := strconv.ParseFloat(fundText(d, key), 64)
	if err != nil {
		return nil
	}
	return &value
}

// FundArchive 是天天基金网页的基金档案。字段是网页上的原文，如成立日期「2018年09月05日」、规模「189.33亿元（截止至：2026年09月30日）」。
type FundArchive struct {
	FullName      string // 基金全称
	ShortName     string // 基金简称
	Type          string // 基金类型
	LaunchDate    string // 成立日期
	LaunchSize    string // 成立规模
	NetAssets     string // 净资产规模，带截止日期
	Company       string // 基金管理人
	Custodian     string // 基金托管人
	Managers      string // 基金经理，多人用顿号分隔
	ManagementFee string // 管理费率
	CustodyFee    string // 托管费率
	Benchmark     string // 业绩比较基准
	Tracking      string // 跟踪标的
	Objective     string // 投资目标
}

// GetFundArchive 查询基金档案，code 是 6 位基金代码。没有这只基金时返回 nil 和 nil：
// 网页对不存在的代码也会返回一张填了别的内容的档案表（名称是别的基金、管理人为空），所以没有基金管理人时当作没有这只基金。
func GetFundArchive(ctx context.Context, code string) (*FundArchive, error) {
	body, err := getBody(ctx, fmt.Sprintf(fundArchiveURL, code))
	if err != nil {
		return nil, err
	}
	return parseFundArchive(body)
}

func parseFundArchive(body []byte) (*FundArchive, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("parse fund archive: %w", err)
	}
	fields := map[string]string{}
	doc.Find("table.info th").Each(func(_ int, th *goquery.Selection) {
		fields[collapse(th.Text())] = emptyIfDash(collapse(th.Next().Text()))
	})
	if fields["基金管理人"] == "" {
		return nil, nil
	}
	a := &FundArchive{
		FullName: fields["基金全称"], ShortName: fields["基金简称"], Type: fields["基金类型"], NetAssets: fields["净资产规模"],
		Company: fields["基金管理人"], Custodian: fields["基金托管人"], Managers: fields["基金经理人"],
		ManagementFee: fields["管理费率"], CustodyFee: fields["托管费率"], Benchmark: fields["业绩比较基准"],
		Tracking: fields["跟踪标的"],
	}
	// 成立日期/规模 写在一格里：2018年09月05日 / 26.142亿份。
	launch := strings.SplitN(fields["成立日期/规模"], "/", 2)
	a.LaunchDate = emptyIfDash(strings.TrimSpace(launch[0]))
	if len(launch) == 2 {
		a.LaunchSize = emptyIfDash(strings.TrimSpace(launch[1]))
	}
	doc.Find("div.boxitem").Each(func(_ int, box *goquery.Selection) {
		if collapse(box.Find("label.left").Text()) == "投资目标" {
			a.Objective = collapse(box.Find("p").First().Text())
		}
	})
	return a, nil
}

// collapse 把连续空白合并成一个空格，去掉首尾空白。
func collapse(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
