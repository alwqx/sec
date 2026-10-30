package macro

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/alwqx/sec/provider/mof"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestCNFiscalCLI(t *testing.T) {
	defer gock.Off()
	root, err := os.ReadFile("../../provider/mof/testdata/index-0.html")
	require.NoError(t, err)
	release, err := os.ReadFile("../../provider/mof/testdata/fiscal-202609.html")
	require.NoError(t, err)
	for _, asJSON := range []bool{false, true} {
		gock.New("https://www.mof.gov.cn").Get("/zhengwuxinxi/redianzhuanti/quanguocaizhengshouzhiqingkuang/$").Reply(200).BodyString(string(root))
		for i := 1; i < 10; i++ {
			raw, e := os.ReadFile(fmt.Sprintf("../../provider/mof/testdata/index-%d.html", i))
			require.NoError(t, e)
			gock.New("https://www.mof.gov.cn").Get(fmt.Sprintf("/zhengwuxinxi/redianzhuanti/quanguocaizhengshouzhiqingkuang/index_%d.html", i)).Reply(200).BodyString(string(raw))
		}
		gock.New("https://gks.mof.gov.cn").Get("/tongjishuju/202610/t20261023_4003380.htm").Reply(200).BodyString(string(release))
		command := newCNFiscalCLI()
		args := []string{"--begin", "202609", "--end", "202609", "--no-cache", "--budget", "fund"}
		if asJSON {
			args = append(args, "--json")
		}
		command.SetArgs(args)
		var out bytes.Buffer
		command.SetOut(&out)
		require.NoError(t, command.Execute())
		if asJSON {
			var result mof.FiscalResult
			require.NoError(t, json.Unmarshal(out.Bytes(), &result))
			require.Len(t, result.Observations, 1)
			require.Len(t, result.Observations[0].Metrics, 2)
			require.Equal(t, 25010.0, *result.Observations[0].Metrics[0].YTDAmount)
			require.Contains(t, out.String(), "raw_paragraph")
		} else {
			for _, text := range []string{"2026年1—9月", "政府性基金收入", "25010", "62754", "-18.6", "官方同比", "赤字"} {
				require.Contains(t, out.String(), text)
			}
			require.NotContains(t, out.String(), "一般公共预算收入")
		}
		require.True(t, gock.IsDone())
	}
	for _, args := range [][]string{{"extra"}, {"--begin", "201412", "--end", "201501"}, {"--budget", "other"}, {"--begin", "202610", "--end", "202609"}} {
		command := newCNFiscalCLI()
		command.SetArgs(args)
		command.SetOut(&bytes.Buffer{})
		command.SetErr(&bytes.Buffer{})
		require.Error(t, command.Execute())
	}
}
func TestCNFiscalZeroAndComparableRates(t *testing.T) {
	zero, negative := 0.0, -3.2
	result := &mof.FiscalResult{Observations: []mof.FiscalObservation{{Period: "201502", Metrics: []mof.FiscalMetric{{Name: "一般公共预算收入", YTDAmount: &zero, YTDYoY: &negative, ComparableYTDYoY: &zero}, {Name: "一般公共预算支出"}}}}}
	var out bytes.Buffer
	require.NoError(t, printCNFiscal(&out, result))
	for _, text := range []string{"0", "-3.2*", "—", "同口径或扣除留抵退税因素后的同比"} {
		require.Contains(t, out.String(), text)
	}
}

func TestCNFiscalDefaultDates(t *testing.T) {
	begin, end, err := cnFiscalMonths(newCNFiscalCLI(), time.Date(2025, 12, 31, 16, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.Equal(t, "202501", begin)
	require.Equal(t, "202512", end)
	command := newCNFiscalCLI()
	require.NoError(t, command.Flags().Set("end", "201502"))
	begin, end, err = cnFiscalMonths(command, time.Now())
	require.NoError(t, err)
	require.Equal(t, "201501", begin)
	require.Equal(t, "201502", end)
}
