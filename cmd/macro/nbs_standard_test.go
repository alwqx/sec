package macro

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/alwqx/sec/provider/macrodata"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func mockStandardRetail(t *testing.T) {
	t.Helper()
	for _, spec := range []struct {
		path, fixture string
		params        map[string]string
	}{
		{"/new/queryIndexTreeAsync", "root", map[string]string{"code": "1"}},
		{"/new/queryIndexTreeAsync", "categories", map[string]string{"code": "1", "pid": "fc982599aa684be7969d7b90b1bd0e84"}},
		{"/new/queryIndexTreeAsync", "retail-catalogs", map[string]string{"code": "1", "pid": "3913ce1309d04eb1bdf7d7b622b1d07c"}},
		{"/new/queryIndicatorsByCid", "retail-series", map[string]string{"cid": "d0cb882c7f27443ab6b3ef9421901961", "pageSize": "1000"}},
	} {
		raw, err := os.ReadFile("../../provider/nbs/testdata/" + spec.fixture + ".json")
		require.NoError(t, err)
		gock.New("https://data.stats.gov.cn").Get("/dg/website/publicrelease/web/external" + spec.path).MatchParams(spec.params).Reply(200).BodyString(string(raw))
	}
	raw, err := os.ReadFile("../../provider/nbs/testdata/retail-values.json")
	require.NoError(t, err)
	gock.New("https://data.stats.gov.cn").Post("/dg/website/publicrelease/web/external/stream/esData").Reply(200).BodyString(string(raw))
}

func TestRetailStandardCLI(t *testing.T) {
	defer gock.Off()
	mockStandardRetail(t)
	cmd := NewMacroCLI()
	cmd.SetArgs([]string{"cn", "retail", "--begin", "202601", "--end", "202603", "--standard-json", "--no-cache", "--refresh"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, cmd.Execute())
	var d macrodata.Dataset
	require.NoError(t, json.Unmarshal(out.Bytes(), &d))
	require.Equal(t, "1", d.SchemaVersion)
	require.Equal(t, "CN", d.Region)
	require.Len(t, d.Series, 4)
	require.Len(t, d.Observations, 12)
	require.Nil(t, d.Observations[4].Value)
	require.Equal(t, "missing", d.Observations[4].Status)
	require.Equal(t, "source_value_missing", *d.Observations[4].MissingReason)
	require.Equal(t, 86078.9, *d.Observations[6].Value)
	require.Equal(t, "202601", d.Observations[6].PeriodBegin)
	require.Equal(t, "202602", d.Observations[6].PeriodEnd)
	require.NotNil(t, d.Observations[6].FetchedAt)
	require.Nil(t, d.Observations[6].PublishedAt)
	require.Contains(t, d.Observations[6].Note, "1—2月")
	require.True(t, gock.IsDone())
}

func TestNBSStandardCLIErrors(t *testing.T) {
	defer gock.Off()
	for _, name := range []string{"retail", "property"} {
		for _, mode := range []string{"flags", "cancel", "http"} {
			cmd := NewMacroCLI()
			args := []string{"cn", name, "--begin", "202601", "--end", "202603", "--standard-json", "--no-cache"}
			ctx := context.Background()
			switch mode {
			case "flags":
				args = append(args, "--json")
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "http":
				gock.New("https://data.stats.gov.cn").Get("/dg/website/publicrelease/web/external/new/queryIndexTreeAsync").Reply(503)
			}
			cmd.SetArgs(args)
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&bytes.Buffer{})
			cmd.SilenceUsage = true
			err := cmd.ExecuteContext(ctx)
			require.Error(t, err, name+"/"+mode)
			if mode == "cancel" {
				require.ErrorIs(t, err, context.Canceled)
			}
			require.Empty(t, out.String())
			require.True(t, gock.IsDone())
		}
	}
	mockStandardRetail(t)
	cmd := newRetailCLI()
	cmd.SetArgs([]string{"--begin", "202601", "--end", "202603", "--standard-json", "--no-cache"})
	cmd.SetOut(failingMoneyWriter{})
	cmd.SetErr(&bytes.Buffer{})
	require.ErrorContains(t, cmd.Execute(), "测试写入失败")
	require.True(t, gock.IsDone())
}
