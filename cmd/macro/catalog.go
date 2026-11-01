package macro

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"time"

	"github.com/alwqx/sec/provider/pbc"
	"github.com/spf13/cobra"
)

// ErrInvalidQuery 表示请求的指标或选项不在指标清单内。
var ErrInvalidQuery = errors.New("无效的宏观指标查询")

// Category 是宏观指标的主题分类，顺序即展示顺序。
type Category struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// OptionValue 是单选选项的一个取值。
type OptionValue struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Option 是影响统一导出内容的命令参数；Values 为空时是开关。
type Option struct {
	Flag   string        `json:"flag"`
	Label  string        `json:"label"`
	Values []OptionValue `json:"values,omitempty"`
}

// Indicator 是一个宏观命令的展示信息。Period 是 --begin、--end 的格式：month、quarter 或 date。
// DefaultBegin 用于命令默认窗口经常没有观测的事件型指标，页面首次打开时使用。
// LatestEnd 是历史已封存指标的最后日期 YYYYMMDD，快捷区间从这里往前推算，而不是今天。
type Indicator struct {
	Region       string   `json:"region"`
	Command      string   `json:"command"`
	Title        string   `json:"title"`
	Category     string   `json:"category"`
	Period       string   `json:"period"`
	Options      []Option `json:"options,omitempty"`
	DefaultBegin string   `json:"default_begin,omitempty"`
	LatestEnd    string   `json:"latest_end,omitempty"`
}

var categories = []Category{
	{"growth", "增长与产出"}, {"prices", "价格"}, {"labor", "就业"}, {"consumption", "消费与收入"}, {"investment", "投资与房地产"},
	{"business", "企业"}, {"money", "货币与信贷"}, {"rates", "利率与收益率"}, {"fiscal", "财政与国债"}, {"trade", "对外贸易"},
}

var sectorOption = Option{"sector", "部门", []OptionValue{{"total", "工业总量"}, {"manufacturing", "制造业"}}}
var tradeComponentOption = Option{"component", "分类", []OptionValue{{"total", "合计"}, {"goods", "商品"}, {"services", "服务"}}}

var indicators = []Indicator{
	{"cn", "gdp", "实际GDP", "growth", "quarter", nil, "", ""},
	{"cn", "industrial", "规模以上工业增加值", "growth", "month", nil, "", ""},
	{"cn", "pmi", "制造业PMI", "growth", "month", nil, "", ""},
	{"cn", "nonmanufacturing-pmi", "非制造业PMI", "growth", "month", nil, "", ""},
	{"cn", "cpi", "CPI", "prices", "month", nil, "", ""},
	{"cn", "core-cpi", "核心CPI", "prices", "month", nil, "", ""},
	{"cn", "ppi", "PPI", "prices", "month", nil, "", ""},
	{"cn", "unemployment", "城镇调查失业率", "labor", "month", []Option{{Flag: "age-groups", Label: "年龄分组"}}, "", ""},
	{"cn", "retail", "社会消费品零售总额", "consumption", "month", nil, "", ""},
	{"cn", "household", "居民收入与消费", "consumption", "quarter", nil, "", ""},
	{"cn", "investment", "固定资产投资", "investment", "month", []Option{{"sector", "分项", []OptionValue{{"total", "总量"}, {"manufacturing", "制造业"}, {"infrastructure", "基建"}, {"real-estate", "房地产开发"}}}}, "", ""},
	{"cn", "property", "房地产", "investment", "month", []Option{{"component", "指标", []OptionValue{{"all", "全部"}, {"sales-area", "销售面积"}, {"sales-value", "销售额"}, {"starts-area", "新开工面积"}}}}, "", ""},
	{"cn", "enterprise", "工业企业经营", "business", "month", []Option{{"metric", "指标", []OptionValue{{"all", "全部"}, {"profit", "利润"}, {"inventory", "存货"}, {"finished-goods", "产成品存货"}, {"receivables", "应收账款"}}}}, "", ""},
	{"cn", "money", "M1、M2", "money", "month", nil, "", ""},
	{"cn", "financing", "社会融资规模", "money", "month", nil, "", ""},
	{"cn", "loans", "人民币贷款", "money", "month", nil, "", ""},
	{"cn", "deposits", "住户存款", "money", "month", nil, "", ""},
	{"cn", "lpr", "LPR", "rates", "month", nil, "", ""},
	{"cn", "reverse-repo", "七天逆回购政策利率", "rates", "date", nil, "", ""},
	{"cn", "reverse-repo-history", "七天逆回购中标利率历史", "rates", "date", nil, "", pbc.ReverseRepoHistoryVerifiedThrough},
	{"cn", "rrr", "存款准备金率调整", "rates", "date", nil, "", ""},
	{"cn", "treasury", "中债国债收益率", "rates", "date", nil, "", ""},
	{"cn", "fiscal", "财政收支", "fiscal", "month", []Option{{"budget", "预算", []OptionValue{{"all", "全部"}, {"general", "一般公共预算"}, {"fund", "政府性基金"}}}}, "", ""},
	{"cn", "trade", "货物贸易", "trade", "month", []Option{{"partners-currency", "伙伴币种", []OptionValue{{"usd", "美元"}, {"cny", "人民币"}}}}, "", ""},
	{"cn", "services-trade", "服务贸易", "trade", "month", nil, "", ""},

	{"us", "gdp", "实际GDP", "growth", "quarter", nil, "", ""},
	{"us", "industrial-production", "工业生产", "growth", "month", []Option{sectorOption}, "", ""},
	{"us", "capacity-utilization", "产能利用率", "growth", "month", []Option{sectorOption}, "", ""},
	{"us", "cpi", "CPI", "prices", "month", nil, "", ""},
	{"us", "core-cpi", "核心CPI", "prices", "month", nil, "", ""},
	{"us", "pce", "PCE价格指数", "prices", "month", nil, "", ""},
	{"us", "core-pce", "核心PCE价格指数", "prices", "month", nil, "", ""},
	{"us", "payrolls", "非农就业", "labor", "month", nil, "", ""},
	{"us", "unemployment", "失业率", "labor", "month", nil, "", ""},
	{"us", "participation", "劳动参与率", "labor", "month", nil, "", ""},
	{"us", "earnings", "平均时薪", "labor", "month", nil, "", ""},
	{"us", "weekly-hours", "平均每周工时", "labor", "month", nil, "", ""},
	{"us", "initial-claims", "初请失业金", "labor", "date", nil, "", ""},
	{"us", "continued-claims", "续请失业金", "labor", "date", nil, "", ""},
	{"us", "real-pce", "实际个人消费支出", "consumption", "month", []Option{{"component", "分项", []OptionValue{{"total", "总量"}, {"goods", "商品"}, {"durable-goods", "耐用品"}, {"nondurable-goods", "非耐用品"}, {"services", "服务"}}}}, "", ""},
	{"us", "real-income", "实际可支配个人收入", "consumption", "month", nil, "", ""},
	{"us", "equipment-investment", "设备投资", "investment", "quarter", []Option{{"price-basis", "价格口径", []OptionValue{{"real", "实际"}, {"nominal", "名义"}}}}, "", ""},
	{"us", "housing-starts", "住宅开工与建筑许可", "investment", "month", []Option{{"component", "分类", []OptionValue{{"housing-starts", "住宅开工"}, {"building-permits", "建筑许可"}}}}, "", ""},
	{"us", "housing", "新房与成屋销售", "investment", "month", []Option{{"component", "分类", []OptionValue{{"all", "全部"}, {"new-sales", "新房"}, {"existing-sales", "成屋"}}}}, "", ""},
	{"us", "house-price", "FHFA房价指数", "investment", "quarter", nil, "", ""},
	{"us", "corporate-profits", "企业税前利润", "business", "quarter", nil, "", ""},
	{"us", "business-inventories", "商业库存", "business", "month", nil, "", ""},
	{"us", "new-orders", "制造业新订单", "business", "month", nil, "", ""},
	{"us", "money", "M2", "money", "month", nil, "", ""},
	{"us", "bank-credit", "商业银行信贷", "money", "month", nil, "", ""},
	{"us", "lending-standards", "贷款标准", "money", "quarter", nil, "", ""},
	{"us", "debt-service", "家庭偿债比率", "money", "quarter", nil, "", ""},
	{"us", "target-rate", "联邦基金目标区间", "rates", "date", nil, "20081216", ""},
	{"us", "effr", "有效联邦基金利率", "rates", "date", nil, "", ""},
	{"us", "treasury", "美债收益率", "rates", "date", nil, "", ""},
	{"us", "fiscal", "财政收支", "fiscal", "month", nil, "", ""},
	{"us", "debt", "国债记账发行与赎回", "fiscal", "date", nil, "", ""},
	{"us", "debt-cash", "国债现金发行与赎回", "fiscal", "date", nil, "", ""},
	{"us", "auctions", "国债拍卖", "fiscal", "date", []Option{{"date-basis", "日期口径", []OptionValue{{"issue", "发行日"}, {"auction", "拍卖日"}}}}, "", ""},
	{"us", "trade", "名义进出口", "trade", "month", []Option{tradeComponentOption}, "", ""},
	{"us", "real-trade", "实际进出口", "trade", "quarter", []Option{tradeComponentOption}, "", ""},
}

// Categories 返回全部主题分类。
func Categories() []Category { return slices.Clone(categories) }

// Indicators 返回全部宏观指标，按分类和展示顺序排列。
func Indicators() []Indicator { return slices.Clone(indicators) }

// applyCategoryGroups 让 --help 按主题分类列出 parent 下的指标命令。
func applyCategoryGroups(parent *cobra.Command, region string) {
	for _, category := range categories {
		parent.AddGroup(&cobra.Group{ID: category.ID, Title: category.Title + ":"})
	}
	for _, indicator := range indicators {
		if indicator.Region != region {
			continue
		}
		for _, command := range parent.Commands() {
			if command.Name() == indicator.Command {
				command.GroupID = indicator.Category
			}
		}
	}
}

var quarterPattern = regexp.MustCompile(`^\d{4}Q[1-4]$`)

// validPeriod 校验 --begin、--end 的格式；覆盖范围由各命令校验。
func validPeriod(period, value string) bool {
	layout := "20060102"
	switch period {
	case "quarter":
		return quarterPattern.MatchString(value)
	case "month":
		layout = "200601"
	}
	parsed, err := time.Parse(layout, value)
	return err == nil && parsed.Format(layout) == value
}

// ExportQuery 是一次统一导出查询，只接受指标清单内的指标和选项。
type ExportQuery struct {
	Region, Command string
	Begin, End      string
	Options         map[string]string
	Refresh         bool
}

// Export 在进程内运行指标命令的 --standard-json，返回统一格式 JSON。
func Export(ctx context.Context, query ExportQuery) ([]byte, error) {
	index := slices.IndexFunc(indicators, func(item Indicator) bool {
		return item.Region == query.Region && item.Command == query.Command
	})
	if index < 0 {
		return nil, fmt.Errorf("%w: 未知指标 %s %s", ErrInvalidQuery, query.Region, query.Command)
	}
	for _, value := range []string{query.Begin, query.End} {
		if value != "" && !validPeriod(indicators[index].Period, value) {
			return nil, fmt.Errorf("%w: 所属期 %q 格式无效", ErrInvalidQuery, value)
		}
	}
	if query.Begin != "" && query.End != "" && query.Begin > query.End {
		return nil, fmt.Errorf("%w: 开始不能晚于结束", ErrInvalidQuery)
	}
	args := []string{query.Region, query.Command, "--standard-json"}
	if query.Begin != "" {
		args = append(args, "--begin="+query.Begin)
	}
	if query.End != "" {
		args = append(args, "--end="+query.End)
	}
	for _, option := range indicators[index].Options {
		value := query.Options[option.Flag]
		switch {
		case value == "":
		case len(option.Values) == 0:
			if value != "true" {
				return nil, fmt.Errorf("%w: 选项 %s 的取值 %q 无效", ErrInvalidQuery, option.Flag, value)
			}
			args = append(args, "--"+option.Flag)
		case slices.ContainsFunc(option.Values, func(item OptionValue) bool { return item.Value == value }):
			args = append(args, "--"+option.Flag+"="+value)
		default:
			return nil, fmt.Errorf("%w: 选项 %s 的取值 %q 无效", ErrInvalidQuery, option.Flag, value)
		}
	}
	root := NewMacroCLI()
	if query.Refresh {
		// 部分命令没有缓存，也就没有 --refresh。
		if command, _, err := root.Find(args[:2]); err == nil && command.Flags().Lookup("refresh") != nil {
			args = append(args, "--refresh")
		}
	}
	var out bytes.Buffer
	root.SetArgs(args)
	root.SetOut(&out)
	root.SetErr(io.Discard)
	root.SilenceUsage, root.SilenceErrors = true, true
	if err := root.ExecuteContext(ctx); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
