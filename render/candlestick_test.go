package render

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func makeTestCandles(n int) []Candle {
	base := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	candles := make([]Candle, n)
	for i := 0; i < n; i++ {
		open := 40.0 + float64(i%5)*0.5
		close := open + 0.3
		if i%3 == 0 {
			close = open - 0.2 // bearish
		}
		candles[i] = Candle{
			Date:   base.AddDate(0, 0, i),
			Open:   open,
			Close:  close,
			High:   open + 0.5,
			Low:    open - 0.3,
			Volume: int64(10000 + i*5000),
		}
	}
	return candles
}

func TestRenderEmpty(t *testing.T) {
	var buf bytes.Buffer
	err := Render(&buf, nil, DefaultConfig())
	require.Nil(t, err)
	require.Equal(t, "", buf.String())
}

func TestRenderSingleCandle(t *testing.T) {
	candles := makeTestCandles(1)
	var buf bytes.Buffer
	cfg := DefaultConfig()
	cfg.Width = 100
	err := Render(&buf, candles, cfg)
	require.Nil(t, err)

	out := buf.String()
	require.NotEmpty(t, out)
	// Should contain date label
	require.Contains(t, out, "01/05")
}

func TestRenderMultipleCandles(t *testing.T) {
	candles := makeTestCandles(20)
	var buf bytes.Buffer
	cfg := DefaultConfig()
	cfg.Width = 120
	err := Render(&buf, candles, cfg)
	require.Nil(t, err)

	out := buf.String()
	require.NotEmpty(t, out)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	// Height (20) + date labels (1) + separator (1) + volume (4) = 26 min
	require.True(t, len(lines) >= 26, "expected at least 26 lines, got %d", len(lines))
}

func TestRenderWithoutVolume(t *testing.T) {
	candles := makeTestCandles(10)
	var buf bytes.Buffer
	cfg := DefaultConfig()
	cfg.Volume = false
	cfg.Width = 120
	err := Render(&buf, candles, cfg)
	require.Nil(t, err)

	out := buf.String()
	require.NotEmpty(t, out)
	// Volume bars use '█' in dim color — should NOT contain volume bars
	lines := strings.Split(strings.TrimSpace(out), "\n")
	// Height (20) + date labels (1) = 21
	require.True(t, len(lines) <= 22, "expected ~21 lines without volume, got %d", len(lines))
}

func TestRenderHalfBlock(t *testing.T) {
	candles := makeTestCandles(10)
	var buf bytes.Buffer
	cfg := DefaultConfig()
	cfg.Width = 120
	cfg.HalfBlock = true
	err := Render(&buf, candles, cfg)
	require.Nil(t, err)

	out := buf.String()
	require.NotEmpty(t, out)
	// Half-block should produce half-block characters
	require.Contains(t, out, "▀")
}

func TestRenderPaging(t *testing.T) {
	candles := makeTestCandles(50)
	var buf bytes.Buffer
	cfg := DefaultConfig()
	cfg.Width = 80
	cfg.Paging = true
	err := Render(&buf, candles, cfg)
	require.Nil(t, err)

	out := buf.String()
	require.NotEmpty(t, out)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	// With candleWidth=5 and width=80, should show ~16 candles per page
	require.True(t, len(lines) <= 30, "expected at most 30 lines in paging mode, got %d", len(lines))
}

func TestAdaptiveDateFormat(t *testing.T) {
	// Create candles spanning two months to verify adaptive date format:
	// "MM/DD" at month boundaries, "DD" within a month
	candles := []Candle{
		{Date: time.Date(2026, 1, 28, 0, 0, 0, 0, time.UTC), Open: 40, Close: 41, High: 42, Low: 39, Volume: 1000},
		{Date: time.Date(2026, 1, 29, 0, 0, 0, 0, time.UTC), Open: 41, Close: 40, High: 42, Low: 39, Volume: 2000},
		{Date: time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC), Open: 40, Close: 41, High: 42, Low: 39, Volume: 3000},
		{Date: time.Date(2026, 2, 3, 0, 0, 0, 0, time.UTC), Open: 41, Close: 42, High: 43, Low: 40, Volume: 4000},
	}
	var buf bytes.Buffer
	cfg := DefaultConfig()
	cfg.Width = 120
	err := Render(&buf, candles, cfg)
	require.Nil(t, err)

	out := buf.String()
	// First label of month 1: "01/28"
	require.Contains(t, out, "01/28")
	// Month 2 boundary: "02/02"
	require.Contains(t, out, "02/02")
	// Intra-month day 3: "03" without month prefix
	require.Contains(t, out, "03")
}

func TestRenderAllSamePrice(t *testing.T) {
	candles := []Candle{
		{
			Date:   time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC),
			Open:   10.0,
			Close:  10.0,
			High:   10.0,
			Low:    10.0,
			Volume: 1000,
		},
		{
			Date:   time.Date(2026, 1, 6, 0, 0, 0, 0, time.UTC),
			Open:   10.0,
			Close:  10.0,
			High:   10.0,
			Low:    10.0,
			Volume: 2000,
		},
	}
	var buf bytes.Buffer
	cfg := DefaultConfig()
	cfg.Width = 80
	err := Render(&buf, candles, cfg)
	require.Nil(t, err)
	require.NotEmpty(t, buf.String())
}

func TestRenderZeroVolume(t *testing.T) {
	candles := []Candle{
		{
			Date:   time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC),
			Open:   40.0,
			Close:  41.0,
			High:   41.5,
			Low:    39.5,
			Volume: 0,
		},
	}
	var buf bytes.Buffer
	cfg := DefaultConfig()
	cfg.Width = 80
	err := Render(&buf, candles, cfg)
	require.Nil(t, err)
	require.NotEmpty(t, buf.String())
}

func TestPriceToRow(t *testing.T) {
	row := priceToRow(50.0, 0.0, 100.0, 10)
	require.Equal(t, 4, row) // near middle (9 - round(0.5*9))

	row = priceToRow(100.0, 0.0, 100.0, 10)
	require.Equal(t, 0, row) // top

	row = priceToRow(0.0, 0.0, 100.0, 10)
	require.Equal(t, 9, row) // bottom
}

func TestPriceToRowSingleValue(t *testing.T) {
	row := priceToRow(100.0, 100.0, 100.0, 20)
	require.Equal(t, 10, row) // middle when range is zero
}

func TestYAxisLabelWidth(t *testing.T) {
	w := yAxisLabelWidth(123.45)
	require.True(t, w >= 8) // " 123.45 ┤"
}

func TestMergePairBothEmpty(t *testing.T) {
	c := mergePair(cell{r: ' '}, cell{r: ' '})
	require.Equal(t, ' ', c.r)
}

func TestMergePairBothBody(t *testing.T) {
	c := mergePair(cell{r: '█', fg: ansiGreen}, cell{r: '█', fg: ansiGreen})
	require.Equal(t, '█', c.r)
	require.Equal(t, ansiGreen, c.fg)
}

func TestMergePairBodyPlusWick(t *testing.T) {
	c := mergePair(cell{r: '█', fg: ansiGreen}, cell{r: '│', fg: ansiRed})
	require.Equal(t, '▀', c.r)
	require.Equal(t, ansiGreen, c.fg)
	require.Equal(t, ansiBgRed, c.bg)
}

func TestMergePairBodyPlusEmpty(t *testing.T) {
	c := mergePair(cell{r: '█', fg: ansiGreen}, cell{r: ' '})
	require.Equal(t, '▀', c.r)
	require.Equal(t, ansiGreen, c.fg)

	c = mergePair(cell{r: ' '}, cell{r: '█', fg: ansiRed})
	require.Equal(t, '▄', c.r)
	require.Equal(t, ansiRed, c.fg)
}

func TestMergePairWickPlusEmpty(t *testing.T) {
	c := mergePair(cell{r: '│', fg: ansiGreen}, cell{r: ' '})
	require.Equal(t, '▀', c.r)
	require.Equal(t, ansiGreen, c.fg)

	c = mergePair(cell{r: ' '}, cell{r: '│', fg: ansiRed})
	require.Equal(t, '▄', c.r)
	require.Equal(t, ansiRed, c.fg)
}

func TestFgToBG(t *testing.T) {
	require.Equal(t, ansiBgRed, fgToBG(ansiRed))
	require.Equal(t, ansiBgGreen, fgToBG(ansiGreen))
	require.Equal(t, ansiBgYellow, fgToBG(ansiYellow))
	require.Equal(t, ansiBgDim, fgToBG(ansiDim))
	require.Equal(t, "", fgToBG("unknown"))
}

func TestMergeCandleGroup(t *testing.T) {
	group := []Candle{
		{Date: time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC), Open: 40, Close: 41, High: 42, Low: 39, Volume: 1000},
		{Date: time.Date(2026, 1, 6, 0, 0, 0, 0, time.UTC), Open: 41, Close: 40, High: 43, Low: 38, Volume: 2000},
		{Date: time.Date(2026, 1, 7, 0, 0, 0, 0, time.UTC), Open: 40, Close: 42, High: 44, Low: 37, Volume: 3000},
	}
	c := mergeCandleGroup(group)
	require.Equal(t, time.Date(2026, 1, 7, 0, 0, 0, 0, time.UTC), c.Date)
	require.Equal(t, 40.0, c.Open)          // first open
	require.Equal(t, 42.0, c.Close)         // last close
	require.Equal(t, 44.0, c.High)          // max high
	require.Equal(t, 37.0, c.Low)           // min low
	require.Equal(t, int64(6000), c.Volume) // sum
}

func TestMergeCandleGroupSingle(t *testing.T) {
	original := Candle{
		Date: time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC),
		Open: 40, Close: 41, High: 42, Low: 39, Volume: 1000,
	}
	c := mergeCandleGroup([]Candle{original})
	require.Equal(t, original, c)
}

func TestDownsampleCandles(t *testing.T) {
	// 20 candles → max 5
	candles := makeTestCandles(20)
	result := downsampleCandles(candles, 5)
	require.True(t, len(result) <= 5)
	require.True(t, len(result) >= 4) // 20/5=4, ceil gives 5 groups of 4
}

func TestDownsampleCandlesNoop(t *testing.T) {
	candles := makeTestCandles(5)
	result := downsampleCandles(candles, 10)
	require.Equal(t, 5, len(result)) // no downsampling needed
}

func TestRenderOverflowDownsample(t *testing.T) {
	// 200 candles with narrow terminal — should downsample without overflow
	candles := makeTestCandles(200)
	var buf bytes.Buffer
	cfg := DefaultConfig()
	cfg.Width = 80
	err := Render(&buf, candles, cfg)
	require.Nil(t, err)

	out := buf.String()
	require.NotEmpty(t, out)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	// Should be a reasonable height (not thousands of columns wide)
	require.True(t, len(lines) < 50, "expected reasonable height, got %d lines", len(lines))
}
