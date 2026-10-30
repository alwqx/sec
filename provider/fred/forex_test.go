package fred

import (
	"context"
	"testing"

	"github.com/alwqx/sec/provider/forex"
	"github.com/stretchr/testify/require"
	"gopkg.in/h2non/gock.v1"
)

const forexSample = "observation_date,DEXCHUS,DEXHKUS\n1981-01-01,.,5.10\n1981-01-02,1.5518,5.145\n1981-01-05,1.5540,.\n"

func TestParseForexCSVUnitsAndMissingValues(t *testing.T) {
	items, bounds, err := parseForexCSV(forexSample)
	require.NoError(t, err)
	require.Len(t, items, 6)
	require.Nil(t, items[0].Value)
	require.Equal(t, 5.1, *items[1].Value)
	require.Equal(t, "USD/HKD", items[1].Pair)
	require.Equal(t, [2]string{"1981-01-02", "1981-01-05"}, bounds["USD/CNY"])
	require.Equal(t, [2]string{"1981-01-01", "1981-01-02"}, bounds["USD/HKD"])
	require.Equal(t, ".", items[0].RawValues["DEXCHUS"])
	for _, body := range []string{
		"DATE,DEXHKUS,DEXCHUS\n1981-01-02,5,1\n",
		"DATE,DEXCHUS,DEXHKUS\n1981-02-30,1,5\n",
		"DATE,DEXCHUS,DEXHKUS\n1981-01-02,-1,5\n",
		"DATE,DEXCHUS,DEXHKUS\n1981-01-02,NaN,5\n",
		"DATE,DEXCHUS,DEXHKUS\n1981-01-02,1,5\n1981-01-02,1,5\n",
		"DATE,DEXCHUS,DEXHKUS\n1981-01-02,1\n",
		"DATE,DEXCHUS,DEXHKUS\n",
	} {
		_, _, err := parseForexCSV(body)
		require.Error(t, err)
	}
}

func TestForexHistorySelectionCachingAndSource(t *testing.T) {
	defer gock.Off()
	gock.New("https://fred.stlouisfed.org").Get("/graph/fredgraph.csv").MatchParam("id", "DEXCHUS,DEXHKUS").Reply(200).BodyString(forexSample)
	client := NewClient(t.TempDir())
	req := &forex.Request{Pairs: []string{"USD/CNY"}, Begin: "19810101", End: "19810102"}
	result, err := client.ForexHistory(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, result.Observations, 2)
	require.Nil(t, result.Observations[0].Value)
	require.Equal(t, 1.5518, *result.Observations[1].Value)
	require.Equal(t, "America/New_York", result.Timezone)
	require.Equal(t, "12:00", result.ObservationTime)
	require.Equal(t, "noon-buying-rate", result.PriceType)
	require.Len(t, result.SnapshotCoverage, 1)
	again, err := client.ForexHistory(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, result, again)
	req.Pairs = []string{"USD/CNH"}
	_, err = client.ForexHistory(context.Background(), req)
	require.Error(t, err)
	require.True(t, gock.IsDone())
}
