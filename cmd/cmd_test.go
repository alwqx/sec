package cmd

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHumanNum(t *testing.T) {
	require.EqualValues(t, " - ", humanNum(-1))
	require.EqualValues(t, " - ", humanNum(0.0))
	require.EqualValues(t, "1.00万", humanNum(10000))
	require.EqualValues(t, "10.09万", humanNum(100900))
	require.EqualValues(t, "1000.09亿", humanNum(100009000009))
}
