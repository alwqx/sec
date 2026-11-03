package metal

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/metal"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestPrintDailyJSON(t *testing.T) {
	cmd := NewMetalCLI()
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, printDailyJSON(cmd, metal.Instruments[0], []*metal.DailyHQItem{
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
	require.NoError(t, printDailyJSON(cmd, metal.Instruments[0], nil))
	require.Contains(t, out.String(), `"results": []`)
}

func runMetal(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := &cobra.Command{Use: "sec"}
	cmd.AddCommand(NewMetalCLI())
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func mockDaily(instRegexp, body string) *gock.Response {
	return gock.New("https://www.sge.com.cn").Get("/graph/Dailyhq").MatchParam("instid", instRegexp).Reply(200).BodyString(body)
}

// 默认只打印 Au99.99 最近一个交易日。
func TestMetalLatest(t *testing.T) {
	defer gock.Off()
	day := time.Now().Format("2006-01-02")
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	mockDaily("^Au99\\.99$", fmt.Sprintf(`{"time":[["%s",830,836,829,837],["%s",836,840.01,834,849.99]]}`, yesterday, day))
	output, err := runMetal(t, "metal")
	require.NoError(t, err)
	require.Contains(t, output, day)
	require.NotContains(t, output, yesterday)
	require.Contains(t, output, "Au99.99 黄金现货")
	require.True(t, gock.IsDone())
}

// --inst 选白银，-b/-e 输出区间内每一天；metal-history 已删除。
func TestMetalRange(t *testing.T) {
	defer gock.Off()
	body := `{"time":[["2026-10-30",12335,12244,12150,12335],["2026-11-02",11866,11876,11866,11955],["2026-11-03",11747,11747,11747,11747]]}`
	mockDaily("^Ag99\\.99$", body)
	output, err := runMetal(t, "metal", "--inst", "ag999", "-b", "20261101", "-e", "20261103", "--json")
	require.NoError(t, err)
	var data struct {
		Inst    string           `json:"inst"`
		Unit    string           `json:"unit"`
		Results []map[string]any `json:"results"`
	}
	require.NoError(t, json.Unmarshal([]byte(output), &data))
	require.Equal(t, "Ag99.99", data.Inst)
	require.Equal(t, "元/千克", data.Unit)
	require.Len(t, data.Results, 2)
	require.InDelta(t, -368.0, data.Results[0]["change"], 1e-9, "区间第一天的涨跌用区间之前的收盘价")

	mockDaily("^Ag99\\.99$", body)
	output, err = runMetal(t, "metal", "-i", "ag999", "-e", "20261103")
	require.NoError(t, err)
	require.Contains(t, output, "2026-10-30", "只给 --end 时往前取 30 天")
	require.Contains(t, output, "2026-11-03")
	require.True(t, gock.IsDone())

	_, err = runMetal(t, "metal-history")
	require.ErrorContains(t, err, "unknown command")
}

func TestMetalArgs(t *testing.T) {
	defer gock.Off()
	_, err := runMetal(t, "metal", "--inst", "cu")
	require.ErrorContains(t, err, "不支持的品种")
	_, err = runMetal(t, "metal", "-b", "20261103", "-e", "20261101")
	require.ErrorContains(t, err, "晚于")
	_, err = runMetal(t, "metal", "-b", "2026-11-01")
	require.ErrorContains(t, err, "YYYYMMDD")

	gock.New("https://www.sge.com.cn").Get("/graph/Dailyhq").Reply(403)
	_, err = runMetal(t, "metal")
	require.ErrorContains(t, err, "访问过于频繁")
}
