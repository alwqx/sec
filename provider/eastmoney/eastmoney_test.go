package eastmoney

import (
	"encoding/json"
	"testing"

	"github.com/alwqx/sec/utils"
	"github.com/stretchr/testify/require"
)

func TestMarketType_String(t *testing.T) {
	var m1 MarketType = 0
	var m2 MarketType = 1
	var m3 MarketType = 3
	require.Equal(t, "SZ", m1.String())
	require.Equal(t, "SH", m2.String())
	require.Equal(t, "unknown 3", m3.String())
}

func TestGetOriginQuoteHistory(t *testing.T) {
	t.Skip("仅用于开发调试")
	req := &GetQuoteHistoryReq{
		Code:       "600036",
		MarketCode: 1,
		Begin:      "20250106",
		End:        "20250106",
	}
	resp, err := getOriginQuoteHistory(req)
	require.Nil(t, err)
	err = utils.WriteJson(resp, "./quote_history.json")
	require.Nil(t, err)
}

func TestParseKlineItem(t *testing.T) {
	// 1. empty
	_, err := ParseKlineItem("")
	require.Equal(t, ErrInvalidKLine, err)

	// 2. common
	line := "2024-12-26,39.40,39.48,39.54,39.01,539252,2125139425.00,1.35,0.20,0.08,0.26"
	res, err := ParseKlineItem(line)
	require.Nil(t, err)
	require.Equal(t, "2024-12-26", res.Date.Format(utils.LayoutYYMMDD))
	require.EqualValues(t, 39.40, res.Open)
	require.EqualValues(t, 39.48, res.Close)
	require.EqualValues(t, 39.54, res.High)
	require.EqualValues(t, 39.01, res.Low)
	require.EqualValues(t, 539252, res.Volume)
	require.EqualValues(t, 2125139425.00, res.TurnOver)
	require.EqualValues(t, 1.35, res.Amplitude)
	require.EqualValues(t, 0.08, res.Change)
	require.EqualValues(t, 0.20, res.ChangeRate)
	require.EqualValues(t, 0.26, res.Velocity)
}

func TestParseQuoteHistoryResp(t *testing.T) {
	res, err := ParseQuoteHistoryResp(nil)
	require.NotNil(t, err)
	require.Nil(t, res)

	rawJson := `{
    "rc": 0,
    "rt": 17,
    "svr": 177617937,
    "lt": 1,
    "full": 0,
    "dlmkts": "",
    "data": {
        "code": "600036",
        "market": 1,
        "name": "招商银行",
        "decimal": 2,
        "dktotal": 5447,
        "preKPrice": 38.35,
        "prePrice": 39.34,
        "qtMiscType": 3,
        "version": 0,
        "klines": [
            "2024-12-24,38.35,39.22,39.29,38.32,993428,3875223368.00,2.53,2.27,0.87,0.48",
            "2024-12-25,39.23,39.40,39.56,38.99,755913,2972981821.00,1.45,0.46,0.18,0.37",
            "2024-12-26,39.40,39.48,39.54,39.01,539252,2125139425.00,1.35,0.20,0.08,0.26"
        ]
    }
}`
	var resp QuoteHistoryResp
	err = json.Unmarshal([]byte(rawJson), &resp)
	require.Nil(t, err)
	res, err = ParseQuoteHistoryResp(&resp)
	require.Nil(t, err)
	require.NotNil(t, res)
	require.EqualValues(t, 3, len(res))
	require.EqualValues(t, 38.35, res[0].Open)
	require.EqualValues(t, 39.40, res[1].Close)
	require.EqualValues(t, 539252, res[2].Volume)
	require.EqualValues(t, 0.26, res[2].Velocity)

	rawJsonEmptyKline := `{
  "rc": 0,
  "rt": 17,
  "svr": 2887161714,
  "lt": 1,
  "full": 0,
  "dlmkts": "",
  "data": {
    "code": "600036",
    "market": 1,
    "name": "招商银行",
    "decimal": 2,
    "dktotal": 5448,
    "preKPrice": 0,
    "prePrice": 39.62,
    "qtMiscType": 7,
    "version": 0,
    "klines": []
  }
}`
	var resp2 QuoteHistoryResp
	err = json.Unmarshal([]byte(rawJsonEmptyKline), &resp2)
	require.Nil(t, err)
	res, err = ParseQuoteHistoryResp(&resp2)
	require.Nil(t, err)
	require.NotNil(t, res)
	require.EqualValues(t, 0, len(res))
}
