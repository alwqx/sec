package macro

import (
	"context"
	"testing"

	"github.com/alwqx/sec/aitool"
	"github.com/stretchr/testify/require"
)

func TestAISource(t *testing.T) {
	source := AISource()
	got := source.Indicators()
	require.Len(t, got, len(indicators))
	require.Equal(t, aitool.MacroIndicator{Region: "cn", Command: "gdp", Title: "实际GDP", Category: "增长与产出", Period: "quarter"}, got[0], "分类换成中文名")
	for _, item := range got {
		if item.Region == "us" && item.Command == "target-rate" {
			require.Equal(t, "20081216", item.DefaultBegin)
		}
		if item.Region == "cn" && item.Command == "investment" {
			require.Equal(t, "202510", item.EarliestBegin)
			require.Equal(t, []aitool.MacroOption{{Flag: "sector", Label: "分项", Values: []string{"total", "manufacturing", "infrastructure", "real-estate"}}}, item.Options)
		}
	}

	_, err := source.Export(context.Background(), aitool.MacroQuery{Region: "cn", Command: "nosuch"})
	require.True(t, aitool.IsInputError(err), "清单外的指标是参数错误")
	require.ErrorContains(t, err, "未知指标")
}
