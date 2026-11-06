package eastmoney

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

// testdata 里是易方达蓝筹的接口和网页片段，fund 包和 Desktop 的测试也用。
func readTestdata(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return string(data)
}

func mockFundBasic() *gock.Request {
	return gock.New("https://fundmobapi.eastmoney.com").Get("/FundMNewApi/FundMNBasicInformation").MatchParam("FCODE", "^005827$").MatchParam("deviceid", ".+")
}

func TestGetFundBasic(t *testing.T) {
	defer gock.Off()
	mockFundBasic().Reply(200).BodyString(readTestdata(t, "fund_basic_005827.json"))
	b, err := GetFundBasic(context.Background(), "005827")
	require.NoError(t, err)
	require.Equal(t, "易方达蓝筹精选混合", b.Name)
	require.Equal(t, "混合型-偏股", b.Type)
	require.Equal(t, 1.4164, *b.NAV)
	require.Equal(t, 1.11, *b.DayChange)
	require.Equal(t, "中高", b.RiskLevel)
	require.False(t, b.Listed)
	require.Equal(t, 3.90, *b.Returns.Week)
	require.Nil(t, b.Returns.Quarter, "-- 表示没有")
	require.Nil(t, b.Returns.TwoYears, "缺失表示没有")
	require.Equal(t, 18932663743.51, *b.NetAssets)

	// 接口一般把数值写成字符串，写成 JSON 数字、布尔值时也要读出来。
	mockFundBasic().Reply(200).BodyString(`{"Datas":{"SHORTNAME":"某基金","RISKLEVEL":4,"ISEXCHG":1,"DWJZ":1.5,"ENDNAV":100000000,"SYL_Z":"---"},"Success":true}`)
	b, err = GetFundBasic(context.Background(), "005827")
	require.NoError(t, err)
	require.Equal(t, "中高", b.RiskLevel)
	require.True(t, b.Listed)
	require.Equal(t, 1.5, *b.NAV)
	require.Equal(t, 1e8, *b.NetAssets)
	require.Nil(t, b.Returns.Week, "--- 表示没有")

	mockFundBasic().Reply(200).BodyString(`{"Datas":null,"ErrCode":0,"Success":true}`)
	b, err = GetFundBasic(context.Background(), "005827")
	require.NoError(t, err)
	require.Nil(t, b, "没有这只基金")

	mockFundBasic().Reply(200).BodyString(`{"Datas":null,"ErrCode":61136,"Success":false,"ErrMsg":"网络繁忙，请稍后重试！"}`)
	_, err = GetFundBasic(context.Background(), "005827")
	require.ErrorContains(t, err, "网络繁忙")
}

func TestParseFundArchive(t *testing.T) {
	a, err := parseFundArchive([]byte(readTestdata(t, "fund_archive_005827.html")))
	require.NoError(t, err)
	require.Equal(t, "易方达蓝筹精选混合型证券投资基金", a.FullName)
	require.Equal(t, "易方达蓝筹精选混合", a.ShortName)
	require.Equal(t, "混合型-偏股", a.Type, "网页的表格少了 </td>，仍能按表头取值")
	require.Equal(t, "2018年09月05日", a.LaunchDate)
	require.Equal(t, "26.142亿份", a.LaunchSize)
	require.Equal(t, "189.33亿元（截止至：2026年09月30日）", a.NetAssets)
	require.Equal(t, "中国银行", a.Custodian)
	require.Equal(t, "张坤、杨思亮", a.Managers)
	require.Equal(t, "1.20%（每年）", a.ManagementFee)
	require.Equal(t, "本基金在控制风险的前提下,追求超越业绩比较基准的投资回报。", a.Objective)

	a, err = parseFundArchive([]byte(`<html><body><p>没有这只基金</p></body></html>`))
	require.NoError(t, err)
	require.Nil(t, a)

	// 网页对不存在的代码也返回一张档案表，内容是别的基金、管理人为空，不能当成这只基金的档案。
	a, err = parseFundArchive([]byte(readTestdata(t, "fund_archive_unknown.html")))
	require.NoError(t, err)
	require.Nil(t, a)
}
