package bond

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseMissingAndZeroYields(t *testing.T) {
	data := []byte(`<feed xmlns="http://www.w3.org/2005/Atom"><entry><content><m:properties xmlns:m="http://schemas.microsoft.com/ado/2007/08/dataservices/metadata" xmlns:d="http://schemas.microsoft.com/ado/2007/08/dataservices"><d:NEW_DATE>2026-01-02T00:00:00</d:NEW_DATE><d:BC_1MONTH m:null="true"/><d:BC_3MONTH>0.00</d:BC_3MONTH><d:BC_6MONTH></d:BC_6MONTH><d:BC_10YEAR>4.25</d:BC_10YEAR></m:properties></content></entry></feed>`)
	items, err := parseAtomFeed(data)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.True(t, items[0].MissingTenors["1m"])
	require.True(t, items[0].MissingTenors["6m"])
	require.True(t, items[0].MissingTenors["30y"])
	require.False(t, items[0].MissingTenors["3m"])
	require.Equal(t, 0.0, items[0].BC3Month)
	require.False(t, items[0].MissingTenors["10y"])
	require.Equal(t, 4.25, items[0].BC10Year)
}

func TestParseFeedStrictOnlyAffectsStrictCallers(t *testing.T) {
	data := []byte(`<feed xmlns="http://www.w3.org/2005/Atom"><entry><content><m:properties xmlns:m="m" xmlns:d="d"><d:NEW_DATE>2026-01-02T00:00:00</d:NEW_DATE><d:BC_10YEAR>N/A</d:BC_10YEAR></m:properties></content></entry><entry><content><m:properties xmlns:m="m" xmlns:d="d"><d:NEW_DATE>bad</d:NEW_DATE></m:properties></content></entry></feed>`)
	items, err := parseAtomFeed(data)
	require.NoError(t, err, "bond、bond-history 的解析行为不变")
	require.Len(t, items, 1)
	require.True(t, items[0].MissingTenors["10y"])
	_, err = parseFeed(data, true)
	require.Error(t, err)
}

func TestCurveDays(t *testing.T) {
	days := CurveDays([]*BondYieldItem{{Date: "2026-01-02", BC3Month: 0, BC10Year: 4.25, MissingTenors: map[string]bool{"1m": true}}})
	require.Len(t, days, 1)
	require.Nil(t, days[0].Month1)
	require.Equal(t, 0.0, *days[0].Month3, "真实0%不是缺失")
	require.Equal(t, 4.25, *days[0].Year10)
}
