package balancesheet

import (
	"bytes"
	"testing"

	"github.com/alwqx/sec/provider/eastmoney"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func newTestCmd() *cobra.Command {
	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	return cmd
}

func TestFormatDate(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"2024-12-31 00:00:00", "2024-12-31"},
		{"2024-03-15T08:30:00Z", "2024-03-15"},
		{"2024-06-30", "2024-06-30"},
		{"short", "short"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			require.Equal(t, tt.want, formatDate(tt.input))
		})
	}
}

func TestFormatFieldValue(t *testing.T) {
	tests := []struct {
		name     string
		apiField string
		value    interface{}
		want     string
	}{
		{
			name:     "nil value",
			apiField: "ANY_FIELD",
			value:    nil,
			want:     "-",
		},
		{
			name:     "report date with time",
			apiField: "REPORT_DATE",
			value:    "2024-12-31 00:00:00",
			want:     "2024-12-31",
		},
		{
			name:     "report date short",
			apiField: "REPORT_DATE",
			value:    "2024-12-31",
			want:     "2024-12-31",
		},
		{
			name:     "basic EPS",
			apiField: "BASIC_EPS",
			value:    5.66,
			want:     "5.66",
		},
		{
			name:     "diluted EPS",
			apiField: "DILUTED_EPS",
			value:    3.14159,
			want:     "3.14",
		},
		{
			name:     "large number uses HumanNum",
			apiField: "TOTAL_ASSETS",
			value:    337377000000.0,
			want:     "3373.77亿",
		},
		{
			name:     "small number uses HumanNum",
			apiField: "TOTAL_PROFIT",
			value:    500000.0,
			want:     "50.00万",
		},
		{
			name:     "string value",
			apiField: "SECURITY_NAME_ABBR",
			value:    "招商银行",
			want:     "招商银行",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatFieldValue(tt.apiField, tt.value)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestPrintSummary(t *testing.T) {
	t.Run("empty results", func(t *testing.T) {
		cmd := newTestCmd()
		printSummary(cmd, nil)
		out := cmd.OutOrStdout().(*bytes.Buffer).String()
		require.Contains(t, out, "无财务报表数据")
	})

	t.Run("empty items", func(t *testing.T) {
		cmd := newTestCmd()
		printSummary(cmd, []result{})
		out := cmd.OutOrStdout().(*bytes.Buffer).String()
		require.Contains(t, out, "无财务报表数据")
	})

	t.Run("with data", func(t *testing.T) {
		cmd := newTestCmd()
		results := []result{
			{
				rt: eastmoney.ReportBalance,
				items: []*eastmoney.FinancialReportItem{
					{
						ReportDate: "2024-12-31 00:00:00",
						PeriodCode: eastmoney.PeriodAnnual,
					},
					{
						ReportDate: "2024-09-30 00:00:00",
						PeriodCode: eastmoney.PeriodQ3,
					},
				},
			},
			{
				rt: eastmoney.ReportIncome,
				items: []*eastmoney.FinancialReportItem{
					{
						ReportDate: "2024-12-31 00:00:00",
						PeriodCode: eastmoney.PeriodAnnual,
					},
				},
			},
		}
		printSummary(cmd, results)
		out := cmd.OutOrStdout().(*bytes.Buffer).String()

		require.Contains(t, out, "资产负债表")
		require.Contains(t, out, "利润表")
		require.Contains(t, out, "年报")
		require.Contains(t, out, "2024-12-31")
		require.Contains(t, out, "三季报")
		require.Contains(t, out, "2024-09-30")
	})
}

func TestPrintDetailed(t *testing.T) {
	t.Run("empty fields", func(t *testing.T) {
		cmd := newTestCmd()
		r := result{
			rt:    eastmoney.FinancialReportType("unknown"),
			items: []*eastmoney.FinancialReportItem{{}},
		}
		printDetailed(cmd, r)
		out := cmd.OutOrStdout().(*bytes.Buffer).String()
		require.Contains(t, out, "无")
		require.Contains(t, out, "数据")
	})

	t.Run("empty items", func(t *testing.T) {
		cmd := newTestCmd()
		r := result{
			rt:    eastmoney.ReportBalance,
			items: nil,
		}
		printDetailed(cmd, r)
		out := cmd.OutOrStdout().(*bytes.Buffer).String()
		require.Contains(t, out, "无")
		require.Contains(t, out, "资产负债表")
		require.Contains(t, out, "数据")
	})

	t.Run("with income data", func(t *testing.T) {
		cmd := newTestCmd()
		r := result{
			rt: eastmoney.ReportIncome,
			items: []*eastmoney.FinancialReportItem{
				{
					ReportDate:   "2024-12-31 00:00:00",
					SecurityCode: "600036",
					SecurityName: "招商银行",
					Fields: map[string]interface{}{
						"REPORT_DATE":          "2024-12-31 00:00:00",
						"TOTAL_OPERATE_INCOME": 337377000000.0,
						"OPERATE_PROFIT":       165623000000.0,
						"TOTAL_PROFIT":         165623000000.0,
						"INCOME_TAX":           15845000000.0,
						"PARENT_NETPROFIT":     149778000000.0,
						"BASIC_EPS":            5.66,
					},
				},
			},
		}
		printDetailed(cmd, r)
		out := cmd.OutOrStdout().(*bytes.Buffer).String()

		require.Contains(t, out, "营业总收入")
		require.Contains(t, out, "营业利润")
		require.Contains(t, out, "归属母公司净利润")
		require.Contains(t, out, "基本每股收益")
		require.Contains(t, out, "3373.77亿")
		require.Contains(t, out, "5.66")
		require.Contains(t, out, "2024-12-31")
	})

	t.Run("with balance data", func(t *testing.T) {
		cmd := newTestCmd()
		r := result{
			rt: eastmoney.ReportBalance,
			items: []*eastmoney.FinancialReportItem{
				{
					ReportDate:   "2023-12-31 00:00:00",
					SecurityCode: "000001",
					SecurityName: "平安银行",
					Fields: map[string]interface{}{
						"REPORT_DATE":                "2023-12-31 00:00:00",
						"TOTAL_ASSETS":               558000000000.0,
						"TOTAL_LIABILITIES":          510000000000.0,
						"TOTAL_EQUITY":               48000000000.0,
						"TOTAL_CURRENT_ASSETS":       nil,
						"TOTAL_NON_CURRENT_ASSETS":   nil,
						"TOTAL_CURRENT_LIABILITIES":  nil,
						"TOTAL_NON_CURRENT_LIABILITIES": nil,
						"MONETARYFUNDS":              nil,
					},
				},
			},
		}
		printDetailed(cmd, r)
		out := cmd.OutOrStdout().(*bytes.Buffer).String()

		require.Contains(t, out, "资产总计")
		require.Contains(t, out, "负债合计")
		require.Contains(t, out, "所有者权益")
		require.Contains(t, out, "5580.00亿")
	})

	t.Run("with cashflow data", func(t *testing.T) {
		cmd := newTestCmd()
		r := result{
			rt: eastmoney.ReportCashFlow,
			items: []*eastmoney.FinancialReportItem{
				{
					ReportDate:   "2024-12-31 00:00:00",
					SecurityCode: "600036",
					SecurityName: "招商银行",
					Fields: map[string]interface{}{
						"REPORT_DATE":                "2024-12-31 00:00:00",
						"NETCASH_OPERATE":            50000000000.0,
						"NETCASH_INVEST":             -20000000000.0,
						"NETCASH_FINANCE":            -10000000000.0,
						"CASH_EQUIVALENTS_INCREASE":  20000000000.0,
					},
				},
				{
					ReportDate:   "2023-12-31 00:00:00",
					SecurityCode: "600036",
					SecurityName: "招商银行",
					Fields: map[string]interface{}{
						"REPORT_DATE":                "2023-12-31 00:00:00",
						"NETCASH_OPERATE":            45000000000.0,
						"NETCASH_INVEST":             -15000000000.0,
						"NETCASH_FINANCE":            -8000000000.0,
						"CASH_EQUIVALENTS_INCREASE":  22000000000.0,
					},
				},
			},
		}
		printDetailed(cmd, r)
		out := cmd.OutOrStdout().(*bytes.Buffer).String()

		require.Contains(t, out, "经营活动现金流")
		require.Contains(t, out, "投资活动现金流")
		require.Contains(t, out, "现金净增加额")
			require.Contains(t, out, "500.00亿")
			require.Contains(t, out, "2024-12-31")
			require.Contains(t, out, "2023-12-31")
	})
}
