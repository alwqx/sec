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
		gock.New("https://yield.chinabond.com.cn").Post("/cbweb-mn/yc/queryYz").MatchParam("startTime", "2006-03-01").MatchParam("endTime", "2006-12-31").Reply(200).BodyString(string(raw))
		cmd := NewMacroCLI()
		args := []string{"cn", "treasury", "--begin", "20060301", "--end", "20060307", "--no-cache"}
		if asJSON {
			args = append(args, "--json")
		}
		cmd.SetArgs(args)
		var out bytes.Buffer
		cmd.SetOut(&out)
		require.NoError(t, cmd.Execute())
		if asJSON {
			require.Contains(t, out.String(), `"curve_type": "yield-to-maturity"`)
			require.Contains(t, out.String(), `"published_at": null`)
			require.Contains(t, out.String(), `"date": "20060301"`)
		} else {
			require.Contains(t, out.String(), "2006-03-01")
			require.Contains(t, out.String(), "2.9000")
			require.Contains(t, out.String(), "中债国债到期收益率曲线")
		}
	}
	require.True(t, gock.IsDone())
}

func TestTreasuryOutputMissing(t *testing.T) {
	zero := 0.0
	var out bytes.Buffer
	require.NoError(t, printTreasury(&out, &chinabond.TreasuryResult{Observations: []chinabond.TreasuryObservation{{Date: "20250101", Yields: map[string]*float64{"1y": &zero}, MissingTenors: []string{"2y", "5y", "10y"}}}}))
	require.Contains(t, out.String(), "0.0000")
	require.Contains(t, out.String(), "—")
	require.Contains(t, out.String(), "期限缺失")
	out.Reset()
	require.NoError(t, printTreasury(&out, &chinabond.TreasuryResult{}))
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
	for _, args := range [][]string{{"--begin", "20060228"}, {"--end", "20250230"}, {"--begin", "20250102", "--end", "20250101"}, {"extra"}} {
		root := NewMacroCLI()
		root.SetArgs(append([]string{"cn", "treasury"}, args...))
		root.SetOut(&bytes.Buffer{})
		root.SetErr(&bytes.Buffer{})
		require.Error(t, root.Execute())
	}
}
