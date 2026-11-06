package fund

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

// testdata 和 provider/eastmoney 的测试共用。
func readTestdata(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "provider", "eastmoney", "testdata", name))
	require.NoError(t, err)
	return string(data)
}

func mockBasic() *gock.Request {
	return gock.New("https://fundmobapi.eastmoney.com").Get("/FundMNewApi/FundMNBasicInformation")
}

func mockArchive() *gock.Request {
	return gock.New("https://fundf10.eastmoney.com").Get("/jbgk_005827.html")
}

func TestLoad(t *testing.T) {
	defer gock.Off()
	mockBasic().Reply(200).BodyString(readTestdata(t, "fund_basic_005827.json"))
	mockArchive().Reply(200).BodyString(readTestdata(t, "fund_archive_005827.html"))
	info, err := Load(context.Background(), "of005827")
	require.NoError(t, err)
	require.Equal(t, "OF005827", info.ExCode)
	require.Equal(t, "005827", info.Code)
	require.Equal(t, "易方达蓝筹精选混合", info.Name)
	require.Equal(t, "易方达蓝筹精选混合型证券投资基金", info.FullName)
	require.Equal(t, "中国银行", info.Custodian)
	require.Equal(t, "张坤、杨思亮", info.Managers, "网页的经理名单优先")
	require.False(t, info.MoneyMarket)
	require.Equal(t, 1.4164, *info.NAV)
	require.Nil(t, info.Yield10K)
	require.NotNil(t, info.Returns)
	require.Empty(t, info.Notes)
	require.True(t, gock.IsDone())
}

// 货币基金的单位净值、累计净值两列是每万份收益和七日年化收益率。
func TestLoadMoneyMarket(t *testing.T) {
	defer gock.Off()
	mockBasic().Reply(200).BodyString(`{"Datas":{"SHORTNAME":"天弘余额宝货币","FTYPE":"货币型-普通货币","DWJZ":"0.2211","LJJZ":"0.7980","RZDF":"--","FSRQ":"2026-11-05"},"Success":true}`)
	mockArchive().Reply(503)
	info, err := Load(context.Background(), "OF005827")
	require.NoError(t, err)
	require.True(t, info.MoneyMarket)
	require.Nil(t, info.NAV)
	require.Equal(t, 0.2211, *info.Yield10K)
	require.Equal(t, 0.798, *info.Yield7D)
	require.Len(t, info.Notes, 1, "档案失败时说明")
	require.Contains(t, info.Notes[0], "基金档案")
}

func TestLoadFailures(t *testing.T) {
	defer gock.Off()
	mockBasic().Reply(503)
	mockArchive().Reply(200).BodyString(readTestdata(t, "fund_archive_005827.html"))
	info, err := Load(context.Background(), "OF005827")
	require.NoError(t, err)
	require.Equal(t, "易方达蓝筹精选混合", info.Name, "App 接口失败时用网页的简称")
	require.Nil(t, info.Returns)
	require.Contains(t, info.Notes[0], "净值")

	mockBasic().Reply(200).BodyString(`{"Datas":null,"Success":true}`)
	mockArchive().Reply(200).BodyString(`<html></html>`)
	_, err = Load(context.Background(), "OF005827")
	require.ErrorIs(t, err, ErrNotFound)

	mockBasic().Reply(503)
	mockArchive().Reply(503)
	_, err = Load(context.Background(), "OF005827")
	require.ErrorContains(t, err, "503")
	require.NotErrorIs(t, err, ErrNotFound)

	// 一个没有这只基金、另一个失败时分不清是否存在，报请求错误，不返回空的概况。
	mockBasic().Reply(200).BodyString(`{"Datas":null,"Success":true}`)
	mockArchive().Reply(503)
	_, err = Load(context.Background(), "OF005827")
	require.ErrorContains(t, err, "基金档案")
	require.NotErrorIs(t, err, ErrNotFound)

	mockBasic().Reply(503)
	mockArchive().Reply(200).BodyString(readTestdata(t, "fund_archive_unknown.html"))
	_, err = Load(context.Background(), "OF005827")
	require.ErrorContains(t, err, "基金信息")
}

// 新成立的基金没有任何阶段收益率，returns 为 null，CLI 和 Desktop 不显示这一行。
func TestLoadNoReturns(t *testing.T) {
	defer gock.Off()
	mockBasic().Reply(200).BodyString(`{"Datas":{"SHORTNAME":"新基金","FTYPE":"股票型","DWJZ":"1.0000","SYL_Z":"--","SYL_LN":"--"},"Success":true}`)
	mockArchive().Reply(200).BodyString(readTestdata(t, "fund_archive_005827.html"))
	info, err := Load(context.Background(), "OF005827")
	require.NoError(t, err)
	require.Equal(t, "新基金", info.Name, "App 接口的简称优先")
	require.Nil(t, info.Returns)
}
