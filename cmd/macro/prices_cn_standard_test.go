package macro

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/alwqx/sec/provider/macrodata"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

// priceCatalogs 是各指标2026年同比、环比目录，顺序与provider请求一致。
var priceCatalogs = map[string][2]string{
	"cpi":      {"5c7452825c7c4dcba391db5ca7f335c5", "b4fad2cf9e0e4af7815b7e9e2e95c5c7"},
	"core-cpi": {"5c7452825c7c4dcba391db5ca7f335c5", "b4fad2cf9e0e4af7815b7e9e2e95c5c7"},
	"ppi":      {"60e8b361f11c4a878c652a6487a25561", "677cfbb4f06941af8c1761c4804e58cf"},
}

// priceSeriesID 从目录样本读取指标ID，避免测试和样本各存一份。
func priceSeriesID(t *testing.T, catalog, name string) string {
	t.Helper()
	var response struct {
		Data struct {
			List []struct {
				ID   string `json:"_id"`
				Name string `json:"i_showname"`
			} `json:"list"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(nbsTestdata(t, catalog+".json")), &response))
	for _, s := range response.Data.List {
		if strings.Join(strings.Fields(s.Name), "") == name {
			return s.ID
		}
	}
	require.FailNow(t, "样本缺少指标", name)
	return ""
}

func mockPriceTree(t *testing.T) {
	t.Helper()
	gock.New(nbsHost).Get(nbsAPIPath+"/new/queryIndexTreeAsync").MatchParam("code", "1").Reply(200).BodyString(nbsTestdata(t, "root.json"))
	for pid, fixture := range map[string]string{"fc982599aa684be7969d7b90b1bd0e84": "categories", "3c9c459384c74f578f3541b2198aac70": "prices", "5b434e4d5e634a39b27a95f8251e9aae": "cpi-yoy-catalogs", "7318f3bae40b4b4badbf519bcd2c79c9": "cpi-mom-catalogs", "55de73673b3d440585cff051fb2e8694": "ppi-catalogs"} {
		gock.New(nbsHost).Get(nbsAPIPath+"/new/queryIndexTreeAsync").MatchParam("pid", pid).Persist().Reply(200).BodyString(nbsTestdata(t, fixture+".json"))
	}
}

func mockCNPrices(indicator string) func(*testing.T) {
	return func(t *testing.T) {
		t.Helper()
		name := map[string]string{"cpi": "居民消费价格指数", "core-cpi": "不包括食品和能源居民消费价格指数", "ppi": "工业生产者出厂价格指数"}[indicator]
		mockPriceTree(t)
		for i, basis := range []string{"上年同月=100", "上月=100"} {
			catalog := priceCatalogs[indicator][i]
			id := priceSeriesID(t, catalog, name+"("+basis+")")
			gock.New(nbsHost).Get(nbsAPIPath+"/new/queryIndicatorsByCid").MatchParam("cid", catalog).Reply(200).BodyString(nbsTestdata(t, catalog+".json"))
			gock.New(nbsHost).Post(nbsAPIPath + "/stream/esData").BodyString(id).Reply(200).BodyString(fmt.Sprintf(`{"success":true,"data":[{"code":"202608MM","values":[{"_id":%q,"da":"000000000000","value":"100.0"}]},{"code":"202609MM","values":[{"_id":%q,"da":"000000000000","value":"100.9"}]}]}`, id, id))
		}
	}
}

func TestCNPricesStandardOutput(t *testing.T) {
	for _, indicator := range []string{"cpi", "core-cpi", "ppi"} {
		t.Run(indicator, func(t *testing.T) {
			defer gock.Off()
			mockCNPrices(indicator)(t)
			cmd := NewMacroCLI()
			cmd.SetArgs([]string{"cn", indicator, "--begin", "202608", "--end", "202609", "--standard-json", "--no-cache"})
			var out bytes.Buffer
			cmd.SetOut(&out)
			require.NoError(t, cmd.Execute())
			var result macrodata.Dataset
			require.NoError(t, json.Unmarshal(out.Bytes(), &result))
			require.Len(t, result.Series, 4)
			require.Len(t, result.Observations, 8)
			values := map[string]float64{}
			for _, o := range result.Observations {
				require.NotNil(t, o.Value)
				require.NotEmpty(t, o.SourceSeriesID)
				values[o.SeriesID+"/"+o.Period] = *o.Value
			}
			require.Equal(t, 100.9, values["cn."+indicator+".yoy_index/202609"])
			require.Equal(t, 0.9, values["cn."+indicator+".mom_percent/202609"])
			require.Equal(t, 0.0, values["cn."+indicator+".yoy_percent/202608"])
		})
	}
}

// 2020年12月在没有核心CPI的目录，2021年1月在有该指标的目录，同一序列跨两个目录。
func TestCoreCPIStandardKeepsCatalogGap(t *testing.T) {
	defer gock.Off()
	mockPriceTree(t)
	for _, catalog := range []struct{ old, current, basis, id string }{
		{"9d4eec43537742a7ab5d63db97fa2f51", "809d2522b0fe4be89142650341b19083", "上年同月=100", "core-yoy"},
		{"42132fae9f2244818f0480b4c422615c", "e6664817f0cd427783cd397770695634", "上月=100", "core-mom"},
	} {
		gock.New(nbsHost).Get(nbsAPIPath+"/new/queryIndicatorsByCid").MatchParam("cid", catalog.old).Reply(200).BodyString(`{"success":true,"data":{"total":0,"list":[]}}`)
		gock.New(nbsHost).Get(nbsAPIPath+"/new/queryIndicatorsByCid").MatchParam("cid", catalog.current).Reply(200).BodyString(fmt.Sprintf(`{"success":true,"data":{"total":1,"list":[{"_id":%q,"i_showname":"不包括食品和能源居民消费价格指数 (%s) "}]}}`, catalog.id, catalog.basis))
		gock.New(nbsHost).Post(nbsAPIPath + "/stream/esData").BodyString(catalog.id).Reply(200).BodyString(fmt.Sprintf(`{"success":true,"data":[{"code":"202101MM","values":[{"_id":%q,"da":"000000000000","value":"99.7"}]}]}`, catalog.id))
	}
	cmd := NewMacroCLI()
	cmd.SetArgs([]string{"cn", "core-cpi", "--begin", "202012", "--end", "202101", "--standard-json", "--no-cache"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, cmd.Execute())
	var result macrodata.Dataset
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	require.Len(t, result.Series, 4)
	require.Len(t, result.Observations, 8)
	for _, o := range result.Observations {
		if o.Period == "202012" {
			require.Nil(t, o.Value)
			require.Equal(t, "source_series_unavailable", *o.MissingReason)
			require.Contains(t, o.Note, "未提供核心")
			require.Empty(t, o.SourceSeriesID)
			require.NotNil(t, o.Source)
			require.NotNil(t, o.FetchedAt)
		} else {
			require.Equal(t, "available", o.Status)
			require.Contains(t, o.SourceSeriesID, "core-")
		}
	}
}

func TestCNPricesStandardErrors(t *testing.T) {
	for _, indicator := range []string{"cpi", "core-cpi", "ppi"} {
		for _, mode := range []string{"flags", "cancel", "http", "writer"} {
			t.Run(indicator+"/"+mode, func(t *testing.T) {
				defer gock.Off()
				args := []string{"cn", indicator, "--begin", "202608", "--end", "202609", "--standard-json", "--no-cache"}
				ctx := context.Background()
				switch mode {
				case "flags":
					args = append(args, "--json")
				case "cancel":
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(ctx)
					cancel()
				case "http":
					gock.New(nbsHost).Get(nbsAPIPath + "/new/queryIndexTreeAsync").Persist().Reply(503)
				case "writer":
					mockCNPrices(indicator)(t)
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
			})
		}
	}
}
