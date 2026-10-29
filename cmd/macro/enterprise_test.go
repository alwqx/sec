package macro

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/alwqx/sec/provider/nbs"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

func TestEnterpriseCLIJSONAndTable(t *testing.T) {
	defer gock.Off()
	for _, asJSON := range []bool{false, true} {
		origin, prefix := "https://data.stats.gov.cn", "/dg/website/publicrelease/web/external"
		mock := func(path, file string, params map[string]string) {
			raw, err := os.ReadFile("../../provider/nbs/testdata/enterprise-" + file + ".json")
			require.NoError(t, err)
			gock.New(origin).Get(prefix + path).MatchParams(params).Reply(200).BodyString(string(raw))
		}
		mock("/new/queryIndexTreeAsync", "root", map[string]string{"code": "1"})
		mock("/new/queryIndexTreeAsync", "categories", map[string]string{"code": "1", "pid": "fc982599aa684be7969d7b90b1bd0e84"})
		mock("/new/queryIndexTreeAsync", "catalogs", map[string]string{"code": "1", "pid": "f484811113a5470892cb52b00c2b35c2"})
		mock("/new/queryIndicatorsByCid", "series", map[string]string{"cid": "d760d189206642ce8afad57640408e09", "pageSize": "1000"})
		raw, err := os.ReadFile("../../provider/nbs/testdata/enterprise-current-values.json")
		require.NoError(t, err)
		body := map[string]any{"cid": "d760d189206642ce8afad57640408e09", "rootId": "f484811113a5470892cb52b00c2b35c2", "indicatorIds": []string{"5d98d24036dd4dbebf7f5c0a80753df6", "7f30595d047445e0a3880ab928795892", "9e0537fd94264be0a0f0c1d239dac6fd", "9b524e0c28224feaad7f1f008890a0dc", "f67df29d7067437f88bcc2dbce9ef677", "e00c23ba6ffc45ae86757ce81f1c1997", "46196614c8a64cfdbde9c2e5661083fe", "01d4fd4cd8ec4be5bf836d89cb2ebe0e"}, "daCatalogId": "", "das": []map[string]string{{"text": "全国", "value": "000000000000"}}, "dts": []string{"202601MM-202609MM"}, "showType": 1}
		gock.New(origin).Post(prefix + "/stream/esData").JSON(body).Reply(200).BodyString(string(raw))
		command := newEnterpriseCLI()
		args := []string{"--begin", "202601", "--end", "202609", "--no-cache", "--refresh"}
		if asJSON {
			args = append(args, "--json")
		}
		command.SetArgs(args)
		var out bytes.Buffer
		command.SetOut(&out)
		command.SetErr(&bytes.Buffer{})
		require.NoError(t, command.Execute())
		if asJSON {
			var result nbs.EnterpriseResult
			require.NoError(t, json.Unmarshal(out.Bytes(), &result))
			require.Len(t, result.Metrics, 4)
			require.Len(t, result.Observations, 36)
			require.Equal(t, "year_to_date_flow", result.Observations[4].Measurement)
			require.Equal(t, "202601-202602", *result.Observations[4].YTDPeriod)
			require.Nil(t, result.Observations[5].YTDPeriod)
			require.Equal(t, "2026-02-28", *result.Observations[5].AsOfDate)
			for _, text := range []string{`"value": null`, `"published_at": null`, `"historical_version": null`, `"yoy_percent": 15.7`, `"status": "no_value"`, `"survey_scope":`, `"note":`, "1—2月合并累计利润", "不等同于官方未发布"} {
				require.Contains(t, out.String(), text)
			}
		} else {
			for _, text := range []string{"所属月", "利润总额", "存货", "产成品存货", "应收账款", "2026-08", "52719.80", "15.70", "2026-02-28余额", "2026-01至2026-02累计", "未季调", "—"} {
				require.Contains(t, out.String(), text)
			}
			require.NotContains(t, out.String(), "202601-202602")
			require.NotContains(t, out.String(), "1—2月合并累计利润")
			require.NotContains(t, out.String(), "2月末余额不是1—2月累计")
			require.Contains(t, out.String(), "--json")
		}
	}
	require.True(t, gock.IsDone())
}

func TestEnterpriseOutputZeroAndLoss(t *testing.T) {
	zero, loss, drop := 0.0, -12.5, -140.0
	asOf := "2026-02-28"
	result := &nbs.EnterpriseResult{Metrics: []nbs.EnterpriseMetric{{Metric: "profit", Name: "利润总额"}, {Metric: "inventory", Name: "存货"}}, Observations: []nbs.EnterpriseObservation{{Metric: "profit", Period: "202602", Measurement: "year_to_date_flow", Value: &loss, YoY: &drop}, {Metric: "inventory", Period: "202602", Measurement: "month_end_stock", AsOfDate: &asOf, Value: &zero, YoY: &zero}, {Metric: "profit", Period: "202601", Measurement: "year_to_date_flow"}}}
	var out bytes.Buffer
	require.NoError(t, printEnterprise(&out, result))
	for _, text := range []string{"0.00", "-12.50", "-140.00", "—", "2026-02-28余额"} {
		require.Contains(t, out.String(), text)
	}
	out.Reset()
	require.NoError(t, printEnterprise(&out, nil))
	require.Contains(t, out.String(), "暂无")
}

func TestEnterpriseCLIInvalidArguments(t *testing.T) {
	for _, args := range [][]string{{"--begin", "202613"}, {"--begin", "202609", "--end", "202601"}, {"--begin", "199812", "--end", "199901"}, {"--metric", "combined-receivables"}, {"extra"}} {
		command := newEnterpriseCLI()
		command.SetArgs(args)
		command.SetOut(&bytes.Buffer{})
		command.SetErr(&bytes.Buffer{})
		require.Error(t, command.Execute())
	}
	command := newEnterpriseCLI()
	for _, flag := range []string{"begin", "end", "metric", "json", "refresh", "no-cache"} {
		require.NotNil(t, command.Flags().Lookup(flag))
	}
	require.Equal(t, "all", command.Flags().Lookup("metric").DefValue)
}

func TestEnterpriseCLIEarliestEndClampsDefaultBegin(t *testing.T) {
	defer gock.Off()
	origin, prefix := "https://data.stats.gov.cn", "/dg/website/publicrelease/web/external"
	for _, mock := range []struct {
		path, fixture string
		params        map[string]string
	}{
		{"/new/queryIndexTreeAsync", "root", map[string]string{"code": "1"}},
		{"/new/queryIndexTreeAsync", "categories", map[string]string{"code": "1", "pid": "fc982599aa684be7969d7b90b1bd0e84"}},
		{"/new/queryIndexTreeAsync", "catalogs", map[string]string{"code": "1", "pid": "f484811113a5470892cb52b00c2b35c2"}},
		{"/new/queryIndicatorsByCid", "series", map[string]string{"cid": "d760d189206642ce8afad57640408e09", "pageSize": "1000"}},
	} {
		raw, err := os.ReadFile("../../provider/nbs/testdata/enterprise-" + mock.fixture + ".json")
		require.NoError(t, err)
		gock.New(origin).Get(prefix + mock.path).MatchParams(mock.params).Reply(200).BodyString(string(raw))
	}
	var response struct {
		Success bool              `json:"success"`
		Data    []json.RawMessage `json:"data"`
	}
	raw, err := os.ReadFile("../../provider/nbs/testdata/enterprise-earliest-values.json")
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &response))
	var rows []json.RawMessage
	for _, row := range response.Data {
		var month struct {
			Code string `json:"code"`
		}
		require.NoError(t, json.Unmarshal(row, &month))
		if month.Code <= "199902MM" {
			rows = append(rows, row)
		}
	}
	response.Data = rows
	body := map[string]any{"cid": "d760d189206642ce8afad57640408e09", "rootId": "f484811113a5470892cb52b00c2b35c2", "indicatorIds": []string{"5d98d24036dd4dbebf7f5c0a80753df6", "7f30595d047445e0a3880ab928795892"}, "daCatalogId": "", "das": []map[string]string{{"text": "全国", "value": "000000000000"}}, "dts": []string{"199901MM-199902MM"}, "showType": 1}
	gock.New(origin).Post(prefix + "/stream/esData").JSON(body).Reply(200).JSON(response)
	command := newEnterpriseCLI()
	command.SetArgs([]string{"--end", "199902", "--metric", "profit", "--no-cache", "--json"})
	var out bytes.Buffer
	command.SetOut(&out)
	command.SetErr(&bytes.Buffer{})
	require.NoError(t, command.Execute())
	var result nbs.EnterpriseResult
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	require.Len(t, result.Observations, 2)
	require.Equal(t, "199901", result.Observations[0].Period)
	require.Equal(t, 112.7, *result.Observations[1].Value)
	require.True(t, gock.IsDone())
}
