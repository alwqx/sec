package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHumanCap(t *testing.T) {
	require.EqualValues(t, " - ", humanCap(-1))
	require.EqualValues(t, " - ", humanCap(0.0))
	require.EqualValues(t, "1.00万", humanCap(10000))
	require.EqualValues(t, "10.09万", humanCap(100900))
	require.EqualValues(t, "1000.09亿", humanCap(100009000009))
}
