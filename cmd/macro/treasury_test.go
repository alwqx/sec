package macro

import (
	"bytes"
	"os"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/chinabond"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestTreasuryCLI(t *testing.T) {
	defer gock.Off()
	raw, err := os.ReadFile("../../provider/chinabond/testdata/treasury-20060301-20060307.json")
	require.NoError(t, err)
	for _, asJSON := range []bool{false, true} {
		gock.New("https://yield.chinabond.com.cn").Get("/cbweb-czb-web/czb/historyQuery").MatchParam("startDate", "2006-03-01").MatchParam("endDate", "2006-03-07").Reply(200).BodyString(string(raw))
		cmd := NewMacroCLI()
		args := []string{"cn", "treasury", "--begin", "20060301", "--end", "20060307"}
		if asJSON {
			args = append(args, "--json")
		}
		cmd.SetArgs(args)
		var out bytes.Buffer
		cmd.SetOut(&out)
		require.NoError(t, cmd.Execute())
		if asJSON {
			require.Contains(t, out.String(), `"flag": "0"`)
			require.Contains(t, out.String(), `"oneYear": "1.68"`)
			require.Contains(t, out.String(), `"threeMonth": "1.51"`)
			require.Contains(t, out.String(), `"sixMonth": "1.59"`)
			require.Contains(t, out.String(), `"workTime": "2006-03-01"`)
		} else {
			require.Contains(t, out.String(), "2006-03-01")
			require.Contains(t, out.String(), "2.90")
			require.Contains(t, out.String(), "3个月（%）")
			require.Contains(t, out.String(), "6个月（%）")
			require.Contains(t, out.String(), "1.51")
			require.Contains(t, out.String(), "1.59")
			require.Less(t, bytes.Index(out.Bytes(), []byte("2006-03-01")), bytes.Index(out.Bytes(), []byte("2006-03-07")))
			require.Contains(t, out.String(), "中债国债到期收益率曲线")
		}
	}
	require.True(t, gock.IsDone())
}

func TestTreasuryOutputMissing(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, printTreasury(&out, &chinabond.GetChinaBondResp{HeList: []*chinabond.ChinaBondItem{{Date: "2025-01-01", OneYear: "0.00"}}}))
	require.Contains(t, out.String(), "0.00")
	require.Contains(t, out.String(), "—")
	require.Contains(t, out.String(), "期限缺失")
	out.Reset()
	require.NoError(t, printTreasury(&out, &chinabond.GetChinaBondResp{}))
	require.Contains(t, out.String(), "未返回")
}

func TestTreasuryDatesAndArguments(t *testing.T) {
	cmd := newTreasuryCLI()
	begin, end, err := treasuryDates(cmd, time.Date(2025, 1, 1, 20, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.Equal(t, "20250102", end)
	require.Equal(t, "20241204", begin)
	require.NoError(t, cmd.Flags().Set("end", "20060307"))
	begin, end, err = treasuryDates(cmd, time.Now())
	require.NoError(t, err)
	require.Equal(t, "20060301", begin)
	require.Equal(t, "20060307", end)
	for _, args := range [][]string{{"--begin", "20060228"}, {"--end", "20250230"}, {"--begin", "20250102", "--end", "20250101"}, {"--begin", "20240101", "--end", "20250101"}, {"extra"}} {
		root := NewMacroCLI()
		root.SetArgs(append([]string{"cn", "treasury"}, args...))
		root.SetOut(&bytes.Buffer{})
		root.SetErr(&bytes.Buffer{})
		require.Error(t, root.Execute())
	}
}
