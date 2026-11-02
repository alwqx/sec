package metal

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/alwqx/sec/provider/metal"
	"github.com/stretchr/testify/require"
)

func TestPrintDailyJSON(t *testing.T) {
	cmd := NewMetalCLI()
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, printDailyJSON(cmd, []*metal.DailyHQItem{
		{Date: "2026-10-30", Close: 100},
		{Date: "2026-11-02", Close: 98, YClose: 100, Change: -2, ChangeRate: -0.02},
	}))
	var data struct {
		Results []map[string]any `json:"results"`
	}
	require.NoError(t, json.Unmarshal(out.Bytes(), &data))
	require.Nil(t, data.Results[0]["change_rate"], "没有前一日收盘价时是 null")
	require.InDelta(t, -2.0, data.Results[1]["change_rate"], 1e-9, "单位是 %")

	out.Reset()
	require.NoError(t, printDailyJSON(cmd, nil))
	require.Contains(t, out.String(), `"results": []`)
}
