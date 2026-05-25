package eastmoney

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStripJSONP(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "valid JSONP",
			input: `jQuery1123061445791234567890_1234567890({"success":true,"result":{"data":[]}});`,
			want:  `{"success":true,"result":{"data":[]}}`,
		},
		{
			name:  "plain JSON",
			input: `{"success":true}`,
			want:  `{"success":true}`,
		},
		{
			name:  "JSON array",
			input: `[1,2,3]`,
			want:  `[1,2,3]`,
		},
		{
			name:  "empty string",
			input: ``,
			want:  ``,
		},
		{
			name:  "whitespace only",
			input: `  `,
			want: `  `,
		},
		{
			name:    "no closing paren",
			input:   `callback({`,
			wantErr: true,
		},
		{
			name:  "JSONP with semicolon",
			input: `jQuery123_456({"ok":true});`,
			want:  `{"ok":true}`,
		},
		{
			name:  "nested braces",
			input: `cb({"data":{"items":[1,2]}});`,
			want:  `{"data":{"items":[1,2]}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := stripJSONP([]byte(tt.input))
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.want, string(got))
			}
		})
	}
}

func TestReportFromString(t *testing.T) {
	tests := []struct {
		input string
		want  FinancialReportType
		ok    bool
	}{
		{"balance", ReportBalance, true},
		{"BALANCE", ReportBalance, true},
		{"bs", ReportBalance, true},
		{"income", ReportIncome, true},
		{"is", ReportIncome, true},
		{"cashflow", ReportCashFlow, true},
		{"cf", ReportCashFlow, true},
		{"unknown", "", false},
		{"", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, ok := ReportFromString(tt.input)
			require.Equal(t, tt.ok, ok)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestReportDisplayName(t *testing.T) {
	require.Equal(t, "资产负债表", ReportBalance.DisplayName())
	require.Equal(t, "利润表", ReportIncome.DisplayName())
	require.Equal(t, "现金流量表", ReportCashFlow.DisplayName())
	require.Equal(t, "RPT_UNKNOWN", FinancialReportType("RPT_UNKNOWN").DisplayName())
}

func TestPeriodFromString(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"annual", PeriodAnnual},
		{"yearly", PeriodAnnual},
		{"y", PeriodAnnual},
		{"halfyear", PeriodHalfYear},
		{"half", PeriodHalfYear},
		{"q1", PeriodQ1},
		{"q3", PeriodQ3},
		{"unknown", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			require.Equal(t, tt.want, PeriodFromString(tt.input))
		})
	}
}

func TestPeriodDisplayName(t *testing.T) {
	require.Equal(t, "年报", PeriodDisplayName(PeriodAnnual))
	require.Equal(t, "中报", PeriodDisplayName(PeriodHalfYear))
	require.Equal(t, "一季报", PeriodDisplayName(PeriodQ1))
	require.Equal(t, "三季报", PeriodDisplayName(PeriodQ3))
	require.Equal(t, "unknown", PeriodDisplayName("unknown"))
}

func TestKeyFields(t *testing.T) {
	// Balance sheet should have at least 7 fields
	bf := KeyFields(ReportBalance)
	require.True(t, len(bf) >= 7)
	require.Equal(t, "报告期", bf[0].CN)
	require.Equal(t, "REPORT_DATE", bf[0].API)

	// Income statement
	inf := KeyFields(ReportIncome)
	require.True(t, len(inf) >= 5)

	// Cash flow
	cf := KeyFields(ReportCashFlow)
	require.True(t, len(cf) >= 4)

	// Unknown type
	require.Nil(t, KeyFields(FinancialReportType("unknown")))
}

func TestAllReportTypes(t *testing.T) {
	types := AllReportTypes()
	require.Len(t, types, 3)
}
