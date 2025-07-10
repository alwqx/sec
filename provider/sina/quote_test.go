package sina

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
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
	exp := []string{"sh600036", "sz000001", "hk00700", "hkHSI", "gb_amd"}
	require.EqualValues(t, strings.Join(exp, ","), strings.Join(res, ","))
}

func TestFormatQuoteListLine(t *testing.T) {
	testCases := []struct {
		Name    string
		Line    string
		ExCode  string
		ResLine string
	}{
		{
			Name: "1 empty",
		},
		{
			Name:    "2 common a stock",
			Line:    `var hq_str_sh688047="龙芯中科,106.000,99.680,119.620,119.620,104.500,119.620,0.000,8256723,938310086.000,25600,119.620,7255,119.610,3033,119.600,1767,119.570,6300,119.550,0,0.000,0,0.000,0,0.000,0,0.000,0,0.000,2024-09-30,15:00:01,00,";`,
			ExCode:  "SH688047",
			ResLine: "龙芯中科,106.000,99.680,119.620,119.620,104.500,119.620,0.000,8256723,938310086.000,25600,119.620,7255,119.610,3033,119.600,1767,119.570,6300,119.550,0,0.000,0,0.000,0,0.000,0,0.000,0,0.000,2024-09-30,15:00:01,00,",
		},
		{
			Name:    "2.1 common h stock",
			Line:    `var hq_str_rt_hk09992="POP MART,泡泡玛特,266.800,266.800,272.000,263.200,265.600,-1.200,-0.450,265.400,265.600,1385751264.400,5192605,103.455,0.000,283.400,36.101,2025/07/10,16:08:15,100|0,N|Y|Y,265.400|252.200|278.600,0|||0.000|0.000|0.000, |0,Y";`,
			ExCode:  "HK09992",
			ResLine: "POP MART,泡泡玛特,266.800,266.800,272.000,263.200,265.600,-1.200,-0.450,265.400,265.600,1385751264.400,5192605,103.455,0.000,283.400,36.101,2025/07/10,16:08:15,100|0,N|Y|Y,265.400|252.200|278.600,0|||0.000|0.000|0.000, |0,Y",
		},
		{
			Name:    "2.2 common m stock",
			Line:    `var hq_str_gb_amd="AMD,144.5500,4.44,2025-07-10 22:55:35,6.1400,143.0000,145.8200,141.8500,187.1100,76.4800,32285637,47105949,234373976387,1.37,105.510000,0.00,0.00,0.00,0.00,1621404195,73,0.0000,0.00,0.00,,Jul 10 10:55AM EDT,138.4100,0,1,2025,4641781226.0000,0.0000,0.0000,0.0000,0.0000,138.4100";`,
			ExCode:  "$AMD",
			ResLine: "AMD,144.5500,4.44,2025-07-10 22:55:35,6.1400,143.0000,145.8200,141.8500,187.1100,76.4800,32285637,47105949,234373976387,1.37,105.510000,0.00,0.00,0.00,0.00,1621404195,73,0.0000,0.00,0.00,,Jul 10 10:55AM EDT,138.4100,0,1,2025,4641781226.0000,0.0000,0.0000,0.0000,0.0000,138.4100",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			exCode, newLine := formatQuoteListLine(tc.Line)
			require.Equal(t, tc.ExCode, exCode, tc.Name)
			require.Equal(t, tc.ResLine, newLine, tc.Name)
		})
	}
}

func TestParseQuoteListBody(t *testing.T) {
	body1 := `var hq_str_sh688047="龙芯中科,131.560,131.600,131.950,132.870,130.820,131.950,132.000,1972190,259739142.000,3237,131.950,1900,131.940,1000,131.930,6331,131.920,430,131.880,200,132.000,600,132.010,300,132.020,2373,132.040,2340,132.050,2025-07-10,15:00:01,00,";`
	res, err := parseQuoteListBody(body1)
	require.Nil(t, err)
	require.EqualValues(t, 1, len(res))
	require.EqualValues(t, "SH688047", res[0].ExCode)
	require.EqualValues(t, "龙芯中科", res[0].Name)

	body2 := `var hq_str_rt_hk09992="POP MART,泡泡玛特,266.800,266.800,272.000,263.200,265.600,-1.200,-0.450,265.400,265.600,1385751264.400,5192605,103.455,0.000,283.400,36.101,2025/07/10,16:08:15,100|0,N|Y|Y,265.400|252.200|278.600,0|||0.000|0.000|0.000, |0,Y";`
	res2, err2 := parseQuoteListBody(body2)
	require.Nil(t, err2)
	require.EqualValues(t, 1, len(res2))
	require.EqualValues(t, "HK09992", res2[0].ExCode)
	require.EqualValues(t, "泡泡玛特", res2[0].Name)

	body3 := `var hq_str_gb_amd="AMD,144.5500,4.44,2025-07-10 22:55:35,6.1400,143.0000,145.8200,141.8500,187.1100,76.4800,32285637,47105949,234373976387,1.37,105.510000,0.00,0.00,0.00,0.00,1621404195,73,0.0000,0.00,0.00,,Jul 10 10:55AM EDT,138.4100,0,1,2025,4641781226.0000,0.0000,0.0000,0.0000,0.0000,138.4100";`
	res3, err3 := parseQuoteListBody(body3)
	require.Nil(t, err3)
	require.EqualValues(t, 1, len(res3))
	require.EqualValues(t, "$AMD", res3[0].ExCode)
	require.EqualValues(t, "AMD", res3[0].Name)

	body4 := `var hq_str_sh688047="龙芯中科,131.560,131.600,131.950,132.870,130.820,131.950,132.000,1972190,259739142.000,3237,131.950,1900,131.940,1000,131.930,6331,131.920,430,131.880,200,132.000,600,132.010,300,132.020,2373,132.040,2340,132.050,2025-07-10,15:00:01,00,";
var hq_str_rt_hk09992="POP MART,泡泡玛特,266.800,266.800,272.000,263.200,265.600,-1.200,-0.450,265.400,265.600,1385751264.400,5192605,103.455,0.000,283.400,36.101,2025/07/10,16:08:15,100|0,N|Y|Y,265.400|252.200|278.600,0|||0.000|0.000|0.000, |0,Y";
var hq_str_gb_amd="AMD,144.5500,4.44,2025-07-10 22:55:35,6.1400,143.0000,145.8200,141.8500,187.1100,76.4800,32285637,47105949,234373976387,1.37,105.510000,0.00,0.00,0.00,0.00,1621404195,73,0.0000,0.00,0.00,,Jul 10 10:55AM EDT,138.4100,0,1,2025,4641781226.0000,0.0000,0.0000,0.0000,0.0000,138.4100";`
	res4, err4 := parseQuoteListBody(body4)
	require.Nil(t, err4)
	require.EqualValues(t, 3, len(res4))
	require.EqualValues(t, "SH688047", res4[0].ExCode)
	require.EqualValues(t, "龙芯中科", res4[0].Name)
	require.EqualValues(t, "HK09992", res4[1].ExCode)
	require.EqualValues(t, "泡泡玛特", res4[1].Name)
	require.EqualValues(t, "$AMD", res4[2].ExCode)
	require.EqualValues(t, "AMD", res4[2].Name)
}

func TestQueryQuoteList(t *testing.T) {
	body1 := `var hq_str_sh688047="龙芯中科,131.560,131.600,131.950,132.870,130.820,131.950,132.000,1972190,259739142.000,3237,131.950,1900,131.940,1000,131.930,6331,131.920,430,131.880,200,132.000,600,132.010,300,132.020,2373,132.040,2340,132.050,2025-07-10,15:00:01,00,";\n`
	defer gock.Off()
	gock.New("https://hq.sinajs.cn").Get("/list=sh688047").
		Reply(200).BodyString(body1).
		Header.Add("content-type", "application/javascript; charset=gbk")

	res, err := QueryQuoteList([]string{"SH688047"})
	require.Nil(t, err)
	require.EqualValues(t, 1, len(res))
	require.EqualValues(t, "SH688047", res[0].ExCode)
	// require.EqualValues(t, "龙芯中科", res[0].Name)

	body4 := `var hq_str_sh688047="龙芯中科,131.560,131.600,131.950,132.870,130.820,131.950,132.000,1972190,259739142.000,3237,131.950,1900,131.940,1000,131.930,6331,131.920,430,131.880,200,132.000,600,132.010,300,132.020,2373,132.040,2340,132.050,2025-07-10,15:00:01,00,";
var hq_str_rt_hk09992="POP MART,泡泡玛特,266.800,266.800,272.000,263.200,265.600,-1.200,-0.450,265.400,265.600,1385751264.400,5192605,103.455,0.000,283.400,36.101,2025/07/10,16:08:15,100|0,N|Y|Y,265.400|252.200|278.600,0|||0.000|0.000|0.000, |0,Y";
var hq_str_gb_amd="AMD,144.5500,4.44,2025-07-10 22:55:35,6.1400,143.0000,145.8200,141.8500,187.1100,76.4800,32285637,47105949,234373976387,1.37,105.510000,0.00,0.00,0.00,0.00,1621404195,73,0.0000,0.00,0.00,,Jul 10 10:55AM EDT,138.4100,0,1,2025,4641781226.0000,0.0000,0.0000,0.0000,0.0000,138.4100";`
	defer gock.Off()
	gock.New("https://hq.sinajs.cn").Get("/list=sh688047,hk09992,gb_amd").
		Reply(200).BodyString(body4).
		Header.Add("content-type", "application/javascript; charset=gbk")

	res4, err4 := QueryQuoteList([]string{"SH688047", "HK09992", "$AMD"})
	require.Nil(t, err4)
	require.EqualValues(t, 3, len(res4))
	require.EqualValues(t, "SH688047", res4[0].ExCode)
	// require.EqualValues(t, "龙芯中科", res4[0].Name)
	require.EqualValues(t, "HK09992", res4[1].ExCode)
	// require.EqualValues(t, "泡泡玛特", res4[1].Name)
	require.EqualValues(t, "$AMD", res4[2].ExCode)
	require.EqualValues(t, "AMD", res4[2].Name)
}
