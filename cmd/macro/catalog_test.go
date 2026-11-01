package macro

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

// 新增或改名的命令必须同步到指标清单，选项也必须是命令实际支持的参数。
func TestIndicatorsMatchCommands(t *testing.T) {
	root := NewMacroCLI()
	categoryIDs := map[string]bool{}
	for _, category := range Categories() {
		categoryIDs[category.ID] = true
	}
	for _, region := range []string{"cn", "us"} {
		parent, _, err := root.Find([]string{region})
		require.NoError(t, err)
		listed := map[string]bool{}
		for _, indicator := range Indicators() {
			if indicator.Region != region {
				continue
			}
			require.False(t, listed[indicator.Command], "重复指标 %s", indicator.Command)
			listed[indicator.Command] = true
			require.True(t, categoryIDs[indicator.Category], indicator.Command)
			require.Contains(t, []string{"month", "quarter", "date"}, indicator.Period, indicator.Command)
			command, _, err := parent.Find([]string{indicator.Command})
			require.NoError(t, err)
			require.Equal(t, indicator.Command, command.Name())
			require.Equal(t, indicator.Category, command.GroupID)
			require.NotNil(t, command.Flags().Lookup("standard-json"), indicator.Command)
			for _, option := range indicator.Options {
				flag := command.Flags().Lookup(option.Flag)
				require.NotNil(t, flag, "%s --%s", indicator.Command, option.Flag)
				// 页面未改动选项时不传参数，显示的第一项必须就是命令默认值。
				if len(option.Values) > 0 {
					require.Equal(t, flag.DefValue, option.Values[0].Value, "%s --%s", indicator.Command, option.Flag)
				}
			}
		}
		for _, command := range parent.Commands() {
			require.True(t, listed[command.Name()], "%s %s 不在指标清单内", region, command.Name())
		}
	}
}

func TestHelpGroupsIndicatorsByCategory(t *testing.T) {
	root := NewMacroCLI()
	parent, _, err := root.Find([]string{"us"})
	require.NoError(t, err)
	require.Contains(t, parent.UsageString(), "利率与收益率:")
}

func TestExportRunsStandardJSON(t *testing.T) {
	defer gock.Off()
	mockUSTreasuryYear("2026", 200, usTreasuryFeed)
	raw, err := Export(context.Background(), ExportQuery{Region: "us", Command: "treasury", Begin: "20260101", End: "20260131", Refresh: true})
	require.NoError(t, err)
	var dataset struct {
		Region       string           `json:"region"`
		Observations []map[string]any `json:"observations"`
	}
	require.NoError(t, json.Unmarshal(raw, &dataset))
	require.Equal(t, "US", dataset.Region)
	require.Len(t, dataset.Observations, 11)
}

func TestExportRejectsUnlistedQuery(t *testing.T) {
	for name, query := range map[string]ExportQuery{
		"unknown region":  {Region: "uk", Command: "cpi"},
		"unknown command": {Region: "us", Command: "nope"},
		"bad value":       {Region: "us", Command: "trade", Options: map[string]string{"component": "--json"}},
		"bad switch":      {Region: "cn", Command: "unemployment", Options: map[string]string{"age-groups": "yes"}},
		"bad month":       {Region: "cn", Command: "cpi", Begin: "2026/01"},
		"month 13":        {Region: "cn", Command: "cpi", End: "202613"},
		"bad quarter":     {Region: "us", Command: "gdp", Begin: "2026Q5"},
		"bad date":        {Region: "us", Command: "treasury", Begin: "20260230"},
		"reversed":        {Region: "us", Command: "treasury", Begin: "20260201", End: "20260101"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Export(context.Background(), query)
			require.ErrorIs(t, err, ErrInvalidQuery)
		})
	}
}

// 未列入清单的选项不会传给命令。
func TestExportIgnoresUnlistedOptions(t *testing.T) {
	defer gock.Off()
	mockUSTreasuryYear("2026", 200, usTreasuryFeed)
	raw, err := Export(context.Background(), ExportQuery{Region: "us", Command: "treasury", Begin: "20260101", End: "20260131", Options: map[string]string{"json": "true"}})
	require.NoError(t, err)
	require.Contains(t, string(raw), `"schema_version"`)
}
