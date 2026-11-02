package ipo

import (
	"encoding/json"
	"testing"

	"github.com/alwqx/sec/provider/eastmoney"
	"github.com/stretchr/testify/require"
)

func TestNewListRows(t *testing.T) {
	require.Equal(t, []listRow{}, newListRows(nil), "结果不为 nil，输出空数组")

	rows := newListRows([]*eastmoney.IListing{
		{Code: "688001", Name: "华兴源创", ListingDate: "20261030"},
		{Code: "688002", Name: "未上市", ListingDate: "-"},
		{Code: "688003", Name: "缺失", ListingDate: ""},
	})
	body, err := json.Marshal(rows)
	require.NoError(t, err)
	require.JSONEq(t, `[
		{"code":"688001","name":"华兴源创","listing_date":"2026-10-30"},
		{"code":"688002","name":"未上市","listing_date":null},
		{"code":"688003","name":"缺失","listing_date":null}
	]`, string(body))
}
