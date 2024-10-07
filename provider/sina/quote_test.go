package sina

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQuoteWs(t *testing.T) {
	keys := []string{"sh600036", "sh688047"}
	res, err := QuoteWs(keys)
	require.Nil(t, err)
	require.Equal(t, len(keys), len(res))
}

func TestParseQuoteWsBody(t *testing.T) {
	// 1. nil or empty
	res := parseQuoteWsBody("")
	require.Equal(t, 0, len(res))

	// 2. common
	body := `sh600036=招商银行,36.350,35.630,37.610,38.000,35.920,37.610,37.620,256101260,9443438268.000,690801,37.610,286600,37.600,17000,37.590,55400,37.580,12200,37.570,161925,37.620,90600,37.630,50400,37.640,104100,37.650,126000,37.660,2024-09-30,15:00:00,00,
sh688047=龙芯中科,106.000,99.680,119.620,119.620,104.500,119.620,0.000,8256723,938310086.000,25600,119.620,7255,119.610,3033,119.600,1767,119.570,6300,119.550,0,0.000,0,0.000,0,0.000,0,0.000,0,0.000,2024-09-30,15:00:01,00,
sys_nxkey=SH688047,SZ002475`
	res = parseQuoteWsBody(body)
	require.Equal(t, 2, len(res))
	require.EqualValues(t, "招商银行", res[0].Name)
}

func TestFormatQuoteKeys(t *testing.T) {
	// 1. nil or empty
	res := formatQuoteKeys(nil)
	require.Equal(t, 0, len(res))
	res = formatQuoteKeys([]string{})
	require.Equal(t, 0, len(res))

	// 2. common
	res = formatQuoteKeys([]string{"SH600036", "SZ000001", "hk00700", "hkhsi", "$AMD"})
	exp := []string{"sh600036", "sz000001", "rt_hk00700", "rt_hkHSI", "gb_AMD"}
	require.EqualValues(t, strings.Join(exp, ","), strings.Join(res, ","))
}
