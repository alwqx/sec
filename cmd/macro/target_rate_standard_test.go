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

func mockFederalReserve(t *testing.T, paths map[string]string) {
	t.Helper()
	for path, file := range paths {
		raw, err := os.ReadFile("../../provider/federalreserve/testdata/" + file)
		require.NoError(t, err)
		gock.New("https://www.federalreserve.gov").Get(path).Reply(200).BodyString(string(raw))
	}
}

func TestUSTargetRateStandardOutput(t *testing.T) {
	defer gock.Off()
	mockFederalReserve(t, map[string]string{
		"/monetarypolicy/openmarket.htm":                  "openmarket.html",
		"/monetarypolicy/fomccalendars.htm":               "calendar.html",
		"/newsevents/pressreleases/monetary20200303a.htm": "statement-20200303.html",
		"/newsevents/pressreleases/monetary20200315a.htm": "statement-20200315.html",
	})
	cmd := NewMacroCLI()
	cmd.SetArgs([]string{"us", "fed-funds-target", "--begin", "20200301", "--end", "20200316", "--no-cache", "--standard-json"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, cmd.Execute())
	var result macrodata.Dataset
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	require.Len(t, result.Series, 4)
	require.Len(t, result.Observations, 8)
	require.Equal(t, "20200304", result.Observations[0].Period)
	require.Equal(t, "2020-03-03", *result.Observations[0].PublishedDate)
	require.NotNil(t, result.Observations[0].PublishedAt)
	require.Equal(t, "20200316", result.Observations[6].Period)
	require.Equal(t, -100.0, *result.Observations[6].Value)
	require.Equal(t, "20200304", result.Observations[6].Base.Period)
	require.True(t, gock.IsDone())
}

func TestUSTargetRateStandardNoEvents(t *testing.T) {
	defer gock.Off()
	mockFederalReserve(t, map[string]string{
		"/monetarypolicy/openmarket.htm":                  "openmarket.html",
		"/monetarypolicy/fomccalendars.htm":               "calendar.html",
		"/newsevents/pressreleases/monetary20200315a.htm": "statement-20200315.html",
	})
	cmd := NewMacroCLI()
	cmd.SetArgs([]string{"us", "target-rate", "--begin", "20200320", "--end", "20200322", "--no-cache", "--standard-json"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, cmd.Execute())
	var result macrodata.Dataset
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	require.Len(t, result.Series, 4)
	require.Empty(t, result.Observations)
	var original struct {
		Current struct {
			EffectiveDate string `json:"effective_date"`
		} `json:"current"`
	}
	require.NoError(t, json.Unmarshal(result.Original, &original))
	require.Equal(t, "2020-03-16", original.Current.EffectiveDate)
	require.True(t, gock.IsDone())
}

func TestUSTargetRateStandardFailsOnReleaseOutage(t *testing.T) {
	for _, standard := range []bool{true, false} {
		mockFederalReserve(t, map[string]string{
			"/monetarypolicy/openmarket.htm":    "openmarket.html",
			"/monetarypolicy/fomccalendars.htm": "calendar.html",
		})
		gock.New("https://www.federalreserve.gov").Get("/newsevents/pressreleases/monetary20200315a.htm").Reply(503)
		flag := "--json"
		if standard {
			flag = "--standard-json"
		}
		cmd := NewMacroCLI()
		cmd.SetArgs([]string{"us", "target-rate", "--begin", "20200320", "--end", "20200322", "--no-cache", flag})
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&bytes.Buffer{})
		cmd.SilenceUsage = true
		err := cmd.Execute()
		if standard {
			require.ErrorContains(t, err, "公布信息下载失败")
			require.ErrorContains(t, err, "503")
			require.Empty(t, out.String())
		} else {
			require.NoError(t, err, "原JSON仍保留调整表结果")
			require.Contains(t, out.String(), `"release_fetch_failed": true`)
		}
		require.True(t, gock.IsDone())
		gock.Off()
	}
}

func TestUSTargetRateStandardErrors(t *testing.T) {
	for _, mode := range []string{"flags", "cancel", "http", "writer"} {
		t.Run(mode, func(t *testing.T) {
			defer gock.Off()
			args := []string{"us", "target-rate", "--begin", "20200320", "--end", "20200322", "--no-cache", "--standard-json"}
			ctx := context.Background()
			switch mode {
			case "flags":
				args = append(args, "--json")
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "http":
				gock.New("https://www.federalreserve.gov").Get("/monetarypolicy/openmarket.htm").Reply(503)
			case "writer":
				mockFederalReserve(t, map[string]string{
					"/monetarypolicy/openmarket.htm":                  "openmarket.html",
					"/monetarypolicy/fomccalendars.htm":               "calendar.html",
					"/newsevents/pressreleases/monetary20200315a.htm": "statement-20200315.html",
				})
			}
			cmd := NewMacroCLI()
			cmd.SetArgs(args)
			var out bytes.Buffer
			cmd.SetOut(&out)
			if mode == "writer" {
				cmd.SetOut(failingMoneyWriter{})
			}
			cmd.SetErr(&bytes.Buffer{})
			cmd.SilenceUsage = true
			err := cmd.ExecuteContext(ctx)
			require.Error(t, err)
			switch mode {
			case "flags":
				require.ErrorContains(t, err, "none of the others can be")
			case "cancel":
				require.ErrorIs(t, err, context.Canceled)
			case "http":
				require.ErrorContains(t, err, "503")
			case "writer":
				require.ErrorContains(t, err, "测试写入失败")
			}
			if mode != "writer" {
				require.Empty(t, out.String())
			}
			require.True(t, gock.IsDone())
		})
	}
}
