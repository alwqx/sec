package termtable

import (
	"bytes"
	"strings"
	"testing"

	"github.com/olekukonko/tablewriter"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	var out bytes.Buffer
	table := New(&out, []string{"日期", "收盘"}, Compact, PlainHeader)
	table.Append([]string{"2026-11-03", "840.01"})
	table.Render()
	require.Equal(t, "   日期   \t 收盘  \n2026-11-03\t840.01\t\n", out.String(), "无边框、制表符分隔、表头不加粗、默认居中")

	out.Reset()
	table = New(&out, []string{"日期", "收盘"}, Compact, PlainHeader, LeftHeader)
	table.Append([]string{"2026-11-03", "840.01"})
	table.Render()
	require.Equal(t, "日期      \t收盘   \n2026-11-03\t840.01\t\n", out.String(), "LeftHeader 表头左对齐")

	out.Reset()
	table = New(&out, []string{"标题"}, ColWidth(4))
	table.Append([]string{"aaaa bbbb cccc"})
	table.Render()
	require.Contains(t, out.String(), "\x1b[1m", "表头默认加粗")
	require.Greater(t, strings.Count(out.String(), "\n"), 2, "列宽在添加行之前生效，长单元格折行")
}

func TestChange(t *testing.T) {
	require.Equal(t, tablewriter.Colors{tablewriter.Bold, tablewriter.FgRedColor}, Change(0.5, false))
	require.Equal(t, tablewriter.Colors{tablewriter.Bold, tablewriter.UnderlineSingle, tablewriter.FgGreenColor}, Change(-1, true))
	require.Empty(t, Change(0, true))
}
