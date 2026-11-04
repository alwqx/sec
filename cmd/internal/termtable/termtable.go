// Package termtable 是命令行表格的公共样式：无边框、无表头分隔线、左对齐、制表符分隔、表头加粗，以及红涨绿跌的颜色。
package termtable

import (
	"io"

	"github.com/olekukonko/tablewriter"
)

// Option 调整表格样式。tablewriter 在添加行时就按列宽折行，所以选项都在 New 里、添加行之前生效。
type Option func(*options)

type options struct {
	compact, leftHeader, noWrap, plainHeader bool
	colWidth                                 int
}

// Compact 去掉单元格两侧的空格。
func Compact(o *options) { o.compact = true }

// LeftHeader 表头左对齐，默认居中。
func LeftHeader(o *options) { o.leftHeader = true }

// NoWrap 不折行，长单元格保持一行。
func NoWrap(o *options) { o.noWrap = true }

// PlainHeader 表头不加粗。
func PlainHeader(o *options) { o.plainHeader = true }

// ColWidth 设置折行宽度，默认 30。
func ColWidth(width int) Option { return func(o *options) { o.colWidth = width } }

// New 返回设置好公共样式的表格。
func New(out io.Writer, headers []string, opts ...Option) *tablewriter.Table {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	table := tablewriter.NewWriter(out)
	if o.colWidth > 0 {
		table.SetColWidth(o.colWidth)
	}
	if o.noWrap {
		table.SetAutoWrapText(false)
	}
	table.SetHeader(headers)
	if !o.plainHeader {
		bold := make([]tablewriter.Colors, len(headers))
		for i := range bold {
			bold[i] = tablewriter.Colors{tablewriter.Bold}
		}
		table.SetHeaderColor(bold...)
	}
	if o.leftHeader {
		table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	}
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetHeaderLine(false)
	table.SetBorder(false)
	table.SetNoWhiteSpace(o.compact)
	table.SetTablePadding("\t")
	return table
}

// Up 是上涨的颜色：加粗红色，underline 为 true 时加下划线（行情表的收盘价、收益率列）。
func Up(underline bool) tablewriter.Colors { return color(tablewriter.FgRedColor, underline) }

// Down 是下跌的颜色：加粗绿色。
func Down(underline bool) tablewriter.Colors { return color(tablewriter.FgGreenColor, underline) }

// Change 按 value 的正负返回红涨绿跌，0 不着色。
func Change(value float64, underline bool) tablewriter.Colors {
	switch {
	case value > 0:
		return Up(underline)
	case value < 0:
		return Down(underline)
	}
	return tablewriter.Colors{}
}

func color(fg int, underline bool) tablewriter.Colors {
	if underline {
		return tablewriter.Colors{tablewriter.Bold, tablewriter.UnderlineSingle, fg}
	}
	return tablewriter.Colors{tablewriter.Bold, fg}
}
