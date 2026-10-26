package macro

import (
	"bytes"
	"testing"

	"github.com/alwqx/sec/provider/nbs"
	"github.com/stretchr/testify/require"
)

func TestNonManufacturingOutput(t *testing.T) {
	value, service, construction, zero := 50.2, 49.3, 50.3, 0.0
	items := []nbs.PMIObservation{
		{Period: "202609", Indicator: "business-activity", Index: &value},
		{Period: "202609", Indicator: "services-business-activity", Index: &service},
		{Period: "202609", Indicator: "construction-business-activity", Index: &construction},
		{Period: "202609", Indicator: "new-orders", SeriesName: "新订单指数 (%) ", Index: &zero},
		{Period: "202609", Indicator: "employment", SeriesName: "从业人员指数 (%) ", Index: nil},
	}
	var out bytes.Buffer
	require.NoError(t, printNonManufacturing(&out, items, false))
	for _, text := range []string{"非制造业商务活动", "服务业商务活动", "建筑业商务活动", "50.2", "49.3", "50.3"} {
		require.Contains(t, out.String(), text)
	}
	require.NotContains(t, out.String(), "新订单")
	out.Reset()
	require.NoError(t, printNonManufacturing(&out, items, true))
	for _, text := range []string{"全国分项", "新订单指数", "0.0", "从业人员指数", "—", "配送时间指数高于 50 表示交货加快"} {
		require.Contains(t, out.String(), text)
	}
	out.Reset()
	require.NoError(t, printNonManufacturing(&out, nil, true))
	require.Contains(t, out.String(), "暂无")
}

func TestNonManufacturingArguments(t *testing.T) {
	for _, args := range [][]string{
		{"cn", "nonmanufacturing-pmi", "--begin", "202613"},
		{"cn", "npmi", "--begin", "202609", "--end", "202601"},
		{"cn", "npmi", "extra"},
	} {
		cmd := NewMacroCLI()
		cmd.SetArgs(args)
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		require.Error(t, cmd.Execute())
	}
}
