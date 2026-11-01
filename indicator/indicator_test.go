package indicator

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSMA(t *testing.T) {
	require.Equal(t, []float64{0, 0, 2, 3, 4}, SMA([]float64{1, 2, 3, 4, 5}, 3))
	require.Equal(t, []float64{1, 2}, SMA([]float64{1, 2}, 1))
	require.Equal(t, []float64{0, 0}, SMA([]float64{1, 2}, 3), "数据不足")
	require.Equal(t, []float64{0, 0}, SMA([]float64{1, 2}, 0), "周期不合法")
	require.Empty(t, SMA(nil, 3))
}

func TestBollinger(t *testing.T) {
	// 2、4、4、4、5、5、7、9 的总体标准差是 2，均值是 5。
	values := []float64{2, 4, 4, 4, 5, 5, 7, 9}
	mid, upper, lower := Bollinger(values, 8, 2)
	require.Equal(t, 5.0, mid[7])
	require.Equal(t, 9.0, upper[7])
	require.Equal(t, 1.0, lower[7])
	require.Zero(t, mid[6], "窗口不足")
	require.Zero(t, upper[6])
	require.Zero(t, lower[6])

	mid, upper, lower = Bollinger([]float64{1}, 3, 2)
	require.Equal(t, []float64{0}, mid)
	require.Equal(t, []float64{0}, upper)
	require.Equal(t, []float64{0}, lower)
}
