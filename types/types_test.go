package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHumanNum(t *testing.T) {
	require.EqualValues(t, " - ", HumanNum(-1))
	require.EqualValues(t, " - ", HumanNum(0.0))
	require.EqualValues(t, "1.00万", HumanNum(10000))
	require.EqualValues(t, "10.09万", HumanNum(100900))
	require.EqualValues(t, "1000.09亿", HumanNum(100009000009))
}
