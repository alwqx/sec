package balancesheet

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractYear(t *testing.T) {
	testCases := []struct {
		title string
		want  string
	}{
		{"2024年年度报告", "2024"},
		{"2024年年度报告（更新后）", "2024"},
		{"2023年年度报告全文", "2023"},
		{"2022年年度报告（英文版）", "2022"},
		{"招商银行股份有限公司2021年年度报告", "2021"},
		{"2020年年度报告摘要", "2020"},
		{"无年份的公告标题", "unknown"},
		{"年份19年报告", "unknown"},   // less than 4 digits
		{"", "unknown"},          // empty title
		{"abc2025def", "2025"},   // year embedded in text
		{"20001231年度报告", "2000"}, // first 4-digit match wins
	}

	for _, tc := range testCases {
		t.Run(tc.title, func(t *testing.T) {
			got := extractYear(tc.title)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestGenStartEndDate(t *testing.T) {
	testCases := []struct {
		name         string
		yearStr      string
		startYearStr string
		endYearStr   string
		wantStart    string
		wantEnd      string
		wantErr      bool
	}{
		{
			name:      "single year",
			yearStr:   "2024",
			wantStart: "2024-01-01",
			wantEnd:   "2024-12-31",
		},
		{
			name:         "year range",
			startYearStr: "2020",
			endYearStr:   "2023",
			wantStart:    "2020-01-01",
			wantEnd:      "2023-12-31",
		},
		{
			name:         "invalid year range",
			startYearStr: "2029",
			endYearStr:   "2023",
			wantErr:      true,
		},
		{
			name:    "invalid year",
			yearStr: "abc",
			wantErr: true,
		},
		{
			name:         "invalid start-year",
			startYearStr: "xyz",
			wantErr:      true,
		},
		{
			name:         "valid start-year invalid end-year",
			startYearStr: "2020",
			endYearStr:   "abc",
			wantErr:      true,
		},
		{
			name:      "empty: defaults to current year",
			wantStart: "",
			wantEnd:   "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			start, end, err := genStartEndDate(tc.yearStr, tc.startYearStr, tc.endYearStr)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			if tc.wantStart != "" {
				require.Equal(t, tc.wantStart, start)
			} else {
				// empty args defaults to current year, just check format
				require.Regexp(t, `^\d{4}-01-01$`, start)
			}
			if tc.wantEnd != "" {
				require.Equal(t, tc.wantEnd, end)
			} else {
				require.Regexp(t, `^\d{4}-12-31$`, end)
			}

			// Verify start < end
			require.True(t, start <= end, "start %s should be <= end %s", start, end)
		})
	}
}
