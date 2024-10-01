package provider

import (
	"testing"

	"github.com/alwqx/sec/types"
	"github.com/stretchr/testify/require"
)

func TestParseBasicSecuritys(t *testing.T) {
	body := `var suggestvalue="龙芯中科,11,688047,sh688047,龙芯中科,,龙芯中科,99,1,,;绿叶制药,31,02186,02186,绿叶制药,,绿叶制药,99,1,ESG,";`
	res := parseBasicSecurity(body)
	require.Equal(t, 2, len(res))
	require.Equal(t, "龙芯中科", res[0].Name)
	require.Equal(t, "SH688047", res[0].ExCode)
	require.Equal(t, "sh", res[0].ExChange)
	require.Equal(t, types.SecurityTypeStock, res[0].SecurityType)

	require.Equal(t, "绿叶制药", res[1].Name)
	require.Equal(t, "HK02186", res[1].ExCode)
	require.Equal(t, "hk", res[1].ExChange)
	require.Equal(t, types.SecurityTypeStock, res[1].SecurityType)
}

func TestProfile(t *testing.T) {
	// Profile("688047")
	quota, part, err := Info("SH688047")
	require.Nil(t, err)
	require.NotNil(t, quota)
	require.NotNil(t, part)
}

func TestParseSinaInfoQuote(t *testing.T) {
	body := "龙芯中科,106.000,99.680,119.620,119.620,104.500,119.620,0.000,8256723,938310086.000,25600,119.620,7255,119.610,3033,119.600,1767,119.570,6300,119.550,0,0.000,0,0.000,0,0.000,0,0.000,0,0.000,2024-09-30,15:00:01,00,"
	parseSinaInfoQuote(body)
}
