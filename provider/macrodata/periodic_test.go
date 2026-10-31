package macrodata

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPeriodicNoteOnlyOnMissingObservations(t *testing.T) {
	r := creditSample("debt-service")
	r.Observations[0].YearAgoBase.Level, r.Observations[0].YearAgoChangePP = nil, nil
	r.Observations[0].Note = "严格上年同期基期水平缺失"
	d, err := Credit(r)
	require.NoError(t, err)
	require.Equal(t, "available", d.Observations[0].Status)
	require.Empty(t, d.Observations[0].Note, "水平值不带其他序列的基期说明")
	require.Equal(t, "baseline_missing", *d.Observations[1].MissingReason)
	require.Equal(t, r.Observations[0].Note, d.Observations[1].Note)
	require.Equal(t, "available", d.Observations[2].Status)
	require.Empty(t, d.Observations[2].Note, "上期变化不带上年基期说明")
}

func TestPeriodicRuleBaselines(t *testing.T) {
	item := func(base float64, change *float64) periodicObservation {
		return periodicObservation{current: periodicValue{"202601", value(2), "2"}, yearAgo: periodicValue{"202501", value(1), "1"}, previous: periodicValue{"202512", value(base), "b"}, yoy: value(100), change: change}
	}
	for _, test := range []struct {
		name   string
		rule   periodicRule
		base   float64
		change *float64
		reason string
		fails  bool
	}{
		{"percent zero base", periodicRule{}, 0, nil, "zero_baseline", false},
		{"percent zero base with value", periodicRule{}, 0, value(1), "", true},
		{"positive base rejects negative", periodicRule{positiveBase: true}, -1, nil, "nonpositive_baseline", false},
		{"positive base rejects zero", periodicRule{positiveBase: true}, 0, nil, "nonpositive_baseline", false},
		{"positive base with value", periodicRule{positiveBase: true}, -1, value(1), "", true},
		{"points allow zero base", periodicRule{kind: pointsChange}, 0, value(2), "", false},
		{"points allow negative base", periodicRule{kind: pointsChange}, -1, value(3), "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			d, err := newDatasetFrequency("US", "202601", "202601", "monthly", map[string]string{})
			require.NoError(t, err)
			err = appendPeriodicChanges(d, Series{ID: "us.test", Measure: "level", Frequency: "monthly"}, monthlyProvenance{}, []periodicObservation{item(test.base, test.change)}, test.rule)
			if test.fails {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if test.reason == "" {
				require.Equal(t, "available", d.Observations[2].Status)
			} else {
				require.Equal(t, test.reason, *d.Observations[2].MissingReason)
			}
		})
	}
}
