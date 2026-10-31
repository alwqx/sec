package macro

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/alwqx/sec/provider/macrodata"
	"github.com/alwqx/sec/provider/pbc"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestMoneyStandardJSON(t *testing.T) {
	defer gock.Off()
	for _, region := range []string{"cn", "us"} {
		t.Run(region, func(t *testing.T) {
			if region == "us" {
				raw, err := os.ReadFile("../../provider/fred/testdata/money.csv")
				require.NoError(t, err)
				gock.New("https://fred.stlouisfed.org").Get("/graph/fredgraph.csv").MatchParam("id", "M2SL").Reply(200).BodyString(string(raw))
			} else {
				raw, err := os.ReadFile("../../provider/pbc/testdata/loan-report-202609.html")
				require.NoError(t, err)
				gock.New("https://www.pbc.gov.cn").Get("/diaochatongjisi/116219/116225/index.html").Reply(200).SetHeader("Content-Type", "text/html; charset=utf-8").BodyString(`<a href="/report.html">2026年前三季度金融统计数据报告</a>`)
				gock.New("https://www.pbc.gov.cn").Get("/report.html").Reply(200).SetHeader("Content-Type", "text/html; charset=utf-8").BodyString(string(raw))
			}
			cmd := NewMacroCLI()
			cmd.SetArgs([]string{region, "money", "--begin", "202609", "--end", "202610", "--no-cache", "--refresh", "--standard-json"})
			var out bytes.Buffer
			cmd.SetOut(&out)
			require.NoError(t, cmd.Execute())
			var d macrodata.Dataset
			require.NoError(t, json.Unmarshal(out.Bytes(), &d))
			require.Equal(t, "1", d.SchemaVersion)
			require.Equal(t, "202609", d.Begin)
			require.Equal(t, "202610", d.End)
			if region == "us" {
				require.Equal(t, "US", d.Region)
				require.Len(t, d.Observations, 6)
				require.Equal(t, 23413.9, *d.Observations[0].Value)
				require.Equal(t, "missing", d.Observations[3].Status)
				require.Equal(t, "current_value_missing", *d.Observations[4].MissingReason)
			} else {
				require.Equal(t, "CN", d.Region)
				require.Len(t, d.Observations, 8)
				require.Equal(t, 3596300.0, *d.Observations[2].Value)
				require.Equal(t, "2026-10-14", *d.Observations[2].PublishedDate)
				require.Equal(t, "report_not_found", *d.Observations[4].MissingReason)
				var original []pbc.MoneySupplyObservation
				require.NoError(t, json.Unmarshal(d.Original, &original))
				require.Len(t, original, 2)
				require.Equal(t, d.Observations[2].Value, original[0].M2Balance)
			}
			require.Nil(t, d.Observations[0].PublishedAt)
			require.Nil(t, d.Observations[0].RevisionID)
			require.Contains(t, out.String(), `"published_at": null`)
			require.Contains(t, out.String(), `"value": null`)
			require.True(t, gock.IsDone())
		})
	}
}

type failingMoneyWriter struct{}

func (failingMoneyWriter) Write([]byte) (int, error) { return 0, errors.New("测试写入失败") }

func TestMoneyStandardErrors(t *testing.T) {
	defer gock.Off()
	for _, region := range []string{"cn", "us"} {
		for _, mode := range []string{"flags", "http", "cancel"} {
			t.Run(region+"/"+mode, func(t *testing.T) {
				cmd := NewMacroCLI()
				args := []string{region, "money", "--begin", "202609", "--end", "202609", "--no-cache", "--standard-json"}
				ctx := context.Background()
				switch mode {
				case "flags":
					args = append(args, "--json")
				case "http":
					if region == "cn" {
						gock.New("https://www.pbc.gov.cn").Get("/diaochatongjisi/116219/116225/index.html").Reply(503)
					} else {
						gock.New("https://fred.stlouisfed.org").Get("/graph/fredgraph.csv").Reply(503)
					}
				case "cancel":
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(ctx)
					cancel()
				}
				cmd.SetArgs(args)
				var out bytes.Buffer
				cmd.SetOut(&out)
				cmd.SetErr(&bytes.Buffer{})
				cmd.SilenceUsage = true
				err := cmd.ExecuteContext(ctx)
				require.Error(t, err)
				if mode == "flags" {
					require.Contains(t, err.Error(), "standard-json")
				}
				if mode == "cancel" {
					require.ErrorIs(t, err, context.Canceled)
				}
				require.Empty(t, out.String(), "失败时不输出部分成功的数据集")
				require.True(t, gock.IsDone())
			})
		}
	}
	raw, err := os.ReadFile("../../provider/fred/testdata/money.csv")
	require.NoError(t, err)
	gock.New("https://fred.stlouisfed.org").Get("/graph/fredgraph.csv").Reply(200).BodyString(string(raw))
	cmd := newUSMoneyCLI()
	cmd.SetArgs([]string{"--begin", "202609", "--end", "202609", "--no-cache", "--standard-json"})
	cmd.SetOut(failingMoneyWriter{})
	cmd.SetErr(&bytes.Buffer{})
	require.ErrorContains(t, cmd.Execute(), "测试写入失败")
	require.True(t, gock.IsDone())
}
