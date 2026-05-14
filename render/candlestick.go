// Package render provides terminal-based candlestick chart rendering for OHLCV data.
package render

import (
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

// Candle represents a single OHLCV candlestick data point.
type Candle struct {
	Date   time.Time
	Open   float64
	Close  float64
	High   float64
	Low    float64
	Volume int64
}

// CandlestickConfig holds configuration for candlestick chart rendering.
type CandlestickConfig struct {
	Width     int  // chart width in columns, 0 = auto-detect terminal width
	Height    int  // price chart height in rows, default 20
	Volume    bool // show volume subgraph below the chart
	Paging    bool // fixed candle width instead of scaling to fit
	HalfBlock bool // use half-block characters for 2x vertical resolution
}

// DefaultConfig returns a sensible default configuration.
func DefaultConfig() CandlestickConfig {
	return CandlestickConfig{
		Width:  0,
		Height: 20,
		Volume: true,
	}
}

// ANSI color codes
const (
	ansiReset  = "\033[0m"
	ansiRed    = "\033[31m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiDim    = "\033[2m"

	// Background color equivalents for half-block rendering
	ansiBgRed    = "\033[41m"
	ansiBgGreen  = "\033[42m"
	ansiBgYellow = "\033[43m"
	ansiBgDim    = "\033[47m" // white bg for dim
	ansiBgNone   = "\033[49m"
)

// fgToBG maps foreground colors to background equivalent for half-block pairs.
func fgToBG(fg string) string {
	switch fg {
	case ansiRed:
		return ansiBgRed
	case ansiGreen:
		return ansiBgGreen
	case ansiYellow:
		return ansiBgYellow
	case ansiDim:
		return ansiBgDim
	default:
		return ""
	}
}

type cell struct {
	r  rune   // character
	fg string // foreground ANSI
	bg string // background ANSI (used in half-block mode)
}

// Render renders a candlestick chart to the writer.
func Render(w io.Writer, candles []Candle, cfg CandlestickConfig) error {
	if len(candles) == 0 {
		return nil
	}

	minLow, maxHigh := candles[0].Low, candles[0].High
	maxVol := int64(0)
	for _, c := range candles {
		if c.Low < minLow {
			minLow = c.Low
		}
		if c.High > maxHigh {
			maxHigh = c.High
		}
		if c.Volume > maxVol {
			maxVol = c.Volume
		}
	}

	priceRange := maxHigh - minLow
	if priceRange == 0 {
		padding := maxHigh * 0.02
		if padding == 0 {
			padding = 1.0
		}
		minLow -= padding
		maxHigh += padding
	}

	chartHeight := cfg.Height
	if chartHeight <= 0 {
		chartHeight = 20
	}
	// Half-block: build grid at 2x logical height, then collapse pairs
	logicalHeight := chartHeight
	if cfg.HalfBlock {
		logicalHeight = chartHeight * 2
	}

	volHeight := 0
	if cfg.Volume {
		volHeight = 4
	}

	termWidth := cfg.Width
	if termWidth <= 0 {
		termWidth = getTerminalWidth()
	}

	yaWidth := yAxisLabelWidth(maxHigh)
	leftMargin := 1

	minWidth := leftMargin + yaWidth + 10
	if termWidth < minWidth {
		termWidth = minWidth
	}

	chartAreaWidth := termWidth - leftMargin - yaWidth
	if chartAreaWidth < 10 {
		chartAreaWidth = 80 - leftMargin - yaWidth
	}

	numCandles := len(candles)
	displayCandles := candles
	var candleWidth int = 3

	if cfg.Paging {
		candleWidth = 5
		perPage := chartAreaWidth / candleWidth
		if perPage < numCandles {
			if perPage <= 0 {
				perPage = 1
			}
			displayCandles = candles[:perPage]
			numCandles = perPage
		}
	} else {
		candleWidth = chartAreaWidth / numCandles
		if candleWidth < 1 {
			candleWidth = 1
		}
		// When more candles than available columns, downsample by merging
		// consecutive candles into synthetic bars (e.g. daily → weekly).
		if numCandles*candleWidth > chartAreaWidth {
			displayCandles = downsampleCandles(candles, chartAreaWidth/candleWidth)
			numCandles = len(displayCandles)
		}
	}

	// Use full terminal width so the chart fills the screen. Extra space
	// between the last candle and the Y-axis is left blank.
	gridWidth := termWidth

	// Build grid at logical height
	gridRows := logicalHeight + 1 // +1 for x-axis labels
	if volHeight > 0 {
		gridRows += 1 + volHeight // separator + volume bars
	}
	grid := makeGrid(gridRows, gridWidth)

	// Draw Y-axis (tick every logical row, label every N ticks)
	drawYAxis(grid, logicalHeight, gridWidth-yaWidth, yaWidth, minLow, maxHigh)

	// Draw candles at logical resolution
	for i, c := range displayCandles {
		col := leftMargin + i*candleWidth + candleWidth/2
		drawCandle(grid, logicalHeight, col, c, minLow, maxHigh)
	}

	// Collapse half-block pairs before drawing labels/volume
	if cfg.HalfBlock {
		grid = combineHalfBlock(grid, logicalHeight)
		logicalHeight = chartHeight
	}

	// Draw X-axis date labels
	drawDateLabels(grid, logicalHeight, displayCandles, leftMargin, candleWidth)

	// Draw volume subgraph
	if volHeight > 0 {
		sepRow := logicalHeight + 1
		drawSeparator(grid, sepRow, leftMargin, numCandles*candleWidth)
		volStartRow := sepRow + 1
		drawVolume(grid, volStartRow, volHeight, displayCandles, leftMargin, candleWidth, maxVol)
	}

	renderGrid(w, grid)
	return nil
}

// downsampleCandles merges consecutive candles into at most maxCandles synthetic
// bars by grouping (n/maxCandles) candles together.
func downsampleCandles(candles []Candle, maxCandles int) []Candle {
	n := len(candles)
	if n <= maxCandles || maxCandles <= 0 {
		return candles
	}

	// Ceil division to get group size. Each group becomes one synthetic candle.
	step := (n + maxCandles - 1) / maxCandles
	result := make([]Candle, 0, maxCandles)
	for i := 0; i < n; i += step {
		end := i + step
		if end > n {
			end = n
		}
		result = append(result, mergeCandleGroup(candles[i:end]))
	}
	return result
}

// mergeCandleGroup merges a group of consecutive candles into a single OHLCV bar.
func mergeCandleGroup(group []Candle) Candle {
	if len(group) == 1 {
		return group[0]
	}
	c := Candle{
		Date:  group[len(group)-1].Date,
		Open:  group[0].Open,
		Close: group[len(group)-1].Close,
		High:  group[0].High,
		Low:   group[0].Low,
	}
	for i := range group {
		if group[i].High > c.High {
			c.High = group[i].High
		}
		if group[i].Low < c.Low {
			c.Low = group[i].Low
		}
		c.Volume += group[i].Volume
	}
	return c
}

// combineHalfBlock collapses pairs of logical chart rows into single physical rows
// using Unicode half-block characters (▀/▄) with foreground+background colors.
func combineHalfBlock(grid [][]cell, logicalChartHeight int) [][]cell {
	physicalChartRows := logicalChartHeight / 2
	nonChartRows := len(grid) - logicalChartHeight
	cols := len(grid[0])

	result := make([][]cell, physicalChartRows+nonChartRows)

	for i := 0; i < physicalChartRows; i++ {
		result[i] = make([]cell, cols)
		upper := grid[2*i]
		lower := grid[2*i+1]
		for j := 0; j < cols; j++ {
			result[i][j] = mergePair(upper[j], lower[j])
		}
	}

	// Copy non-chart rows (x-axis, separator, volume) unchanged
	for i := logicalChartHeight; i < len(grid); i++ {
		result[physicalChartRows+(i-logicalChartHeight)] = grid[i]
	}

	return result
}

// mergePair combines two logical cells (upper/lower) into one physical cell.
func mergePair(upper, lower cell) cell {
	uBody := upper.r == '█'
	lBody := lower.r == '█'
	uWick := upper.r == '│'
	lWick := lower.r == '│'
	uEmpty := upper.r == ' '
	lEmpty := lower.r == ' '

	switch {
	case uEmpty && lEmpty:
		return cell{r: ' '}

	// Same character in both halves
	case uBody && lBody:
		return cell{r: '█', fg: upper.fg}
	case uWick && lWick:
		return cell{r: '│', fg: upper.fg}

	// Body + wick
	case uBody && lWick:
		return cell{r: '▀', fg: upper.fg, bg: fgToBG(lower.fg)}
	case uWick && lBody:
		return cell{r: '▀', fg: upper.fg, bg: fgToBG(lower.fg)}

	// Body + empty
	case uBody && lEmpty:
		return cell{r: '▀', fg: upper.fg}
	case lBody && uEmpty:
		return cell{r: '▄', fg: lower.fg}

	// Wick + empty
	case uWick && lEmpty:
		return cell{r: '▀', fg: upper.fg}
	case lWick && uEmpty:
		return cell{r: '▄', fg: lower.fg}

	// Any other combination: use upper
	default:
		if !uEmpty {
			return cell{r: upper.r, fg: upper.fg}
		}
		return cell{r: lower.r, fg: lower.fg}
	}
}

func makeGrid(rows, cols int) [][]cell {
	g := make([][]cell, rows)
	for i := range g {
		g[i] = make([]cell, cols)
		for j := range g[i] {
			g[i][j] = cell{r: ' '}
		}
	}
	return g
}

// drawYAxis draws price labels and tick marks.
func drawYAxis(grid [][]cell, chartHeight, axisCol, axisWidth int, minLow, maxHigh float64) {
	priceRange := maxHigh - minLow
	if priceRange == 0 {
		return
	}

	labelInterval := 1
	if chartHeight > 20 {
		labelInterval = 2
	}
	if chartHeight > 40 {
		labelInterval = 4
	}

	labelWidth := axisWidth - 2
	denom := chartHeight - 1
	if denom <= 0 {
		denom = 1
	}

	for row := 0; row < chartHeight; row++ {
		grid[row][axisCol] = cell{r: '┤', fg: ansiDim}

		if row%labelInterval == 0 {
			// price := maxHigh - (float64(row)/float64(chartHeight-1))*priceRange
			price := maxHigh - (float64(row)/float64(denom))*priceRange
			label := fmt.Sprintf("%*.2f", labelWidth, price)
			col := axisCol + 2
			for i, ch := range label {
				if col+i < len(grid[row]) {
					grid[row][col+i] = cell{r: ch, fg: ansiDim}
				}
			}
		}
	}
}

// drawCandle draws a single candle (wick + body) at the given column.
func drawCandle(grid [][]cell, chartHeight, col int, c Candle, minLow, maxHigh float64) {
	highRow := priceToRow(c.High, minLow, maxHigh, chartHeight)
	lowRow := priceToRow(c.Low, minLow, maxHigh, chartHeight)
	openRow := priceToRow(c.Open, minLow, maxHigh, chartHeight)
	closeRow := priceToRow(c.Close, minLow, maxHigh, chartHeight)

	bodyTop := min(openRow, closeRow)
	bodyBot := max(openRow, closeRow)

	isBullish := c.Close >= c.Open
	var wickColor, bodyColor string
	if isBullish {
		wickColor = ansiGreen
		bodyColor = ansiGreen
	} else {
		wickColor = ansiRed
		bodyColor = ansiRed
	}

	for row := highRow; row <= lowRow; row++ {
		if col >= 0 && col < len(grid[row]) {
			grid[row][col] = cell{r: '│', fg: wickColor}
		}
	}

	bodyRune := '█'
	if bodyTop == bodyBot && c.Open == c.Close {
		bodyRune = '━'
		bodyColor = ansiYellow
	}
	for row := bodyTop; row <= bodyBot; row++ {
		if col >= 0 && col < len(grid[row]) {
			grid[row][col] = cell{r: bodyRune, fg: bodyColor}
		}
	}
}

// drawDateLabels draws date labels below the price chart.
// Uses adaptive formatting: "MM/DD" at month boundaries and first label,
// "DD" within a month to reduce crowding.
func drawDateLabels(grid [][]cell, labelRow int, candles []Candle, leftMargin, candleWidth int) {
	if labelRow >= len(grid) {
		return
	}
	n := len(candles)
	if n == 0 {
		return
	}

	// Calculate step to ensure labels don't overlap.
	// Use worst-case label width (5 for "MM/DD") + minimum gap of 2 spaces.
	maxLabelWidth := 5
	minGap := 2
	totalWidth := n * candleWidth
	maxLabels := totalWidth / (maxLabelWidth + minGap)
	if maxLabels < 1 {
		maxLabels = 1
	}
	// Ceil division so step distributes labels across the full range.
	step := (n + maxLabels - 1) / maxLabels
	if step < 1 {
		step = 1
	}

	lastMonth := -1
	for i := 0; i < n; i += step {
		col := leftMargin + i*candleWidth + candleWidth/2

		month := int(candles[i].Date.Month())
		var label string
		if month != lastMonth {
			label = candles[i].Date.Format("01/02")
			lastMonth = month
		} else {
			label = candles[i].Date.Format("02")
		}

		startCol := col - len(label)/2
		for j, ch := range label {
			c := startCol + j
			if c >= 0 && c < len(grid[labelRow]) {
				grid[labelRow][c] = cell{r: ch, fg: ansiDim}
			}
		}
	}
}

// drawSeparator draws a horizontal dotted line between chart and volume.
func drawSeparator(grid [][]cell, row, leftMargin, chartWidth int) {
	if row >= len(grid) {
		return
	}
	endCol := leftMargin + chartWidth
	for col := leftMargin; col < endCol && col < len(grid[row]); col++ {
		if col%2 == 0 {
			grid[row][col] = cell{r: '─', fg: ansiDim}
		}
	}
}

// drawVolume draws volume bars below the separator.
func drawVolume(grid [][]cell, startRow, volHeight int, candles []Candle, leftMargin, candleWidth int, maxVol int64) {
	if maxVol == 0 {
		return
	}
	for i, c := range candles {
		col := leftMargin + i*candleWidth + candleWidth/2
		ratio := float64(c.Volume) / float64(maxVol)
		fill := int(ratio*float64(volHeight) + 0.5)
		if fill == 0 && c.Volume > 0 {
			fill = 1
		}
		for j := 0; j < fill; j++ {
			row := startRow + volHeight - 1 - j
			if row < len(grid) && col < len(grid[row]) {
				grid[row][col] = cell{r: '█', fg: ansiDim}
			}
		}
	}
}

// renderGrid writes the grid to the writer, merging consecutive same-style cells.
func renderGrid(w io.Writer, grid [][]cell) {
	var sb strings.Builder
	for _, row := range grid {
		sb.Reset()
		var curFG, curBG string
		for _, c := range row {
			if c.fg != curFG || c.bg != curBG {
				if curFG != "" || curBG != "" {
					sb.WriteString(ansiReset)
				}
				curFG = c.fg
				curBG = c.bg
				if curFG != "" {
					sb.WriteString(curFG)
				}
				if curBG != "" {
					sb.WriteString(curBG)
				}
			}
			sb.WriteRune(c.r)
		}
		if curFG != "" || curBG != "" {
			sb.WriteString(ansiReset)
		}
		line := strings.TrimRight(sb.String(), " ")
		fmt.Fprintln(w, line)
	}
}

// priceToRow maps a price value to a grid row (0 = top, chartHeight-1 = bottom).
func priceToRow(price, minLow, maxHigh float64, chartHeight int) int {
	priceRange := maxHigh - minLow
	if priceRange == 0 {
		return chartHeight / 2
	}
	ratio := (price - minLow) / priceRange
	row := chartHeight - 1 - int(ratio*float64(chartHeight-1)+0.5)
	if row < 0 {
		row = 0
	}
	if row >= chartHeight {
		row = chartHeight - 1
	}
	return row
}

// yAxisLabelWidth returns the width needed for Y-axis price labels.
func yAxisLabelWidth(maxPrice float64) int {
	absPrice := math.Abs(maxPrice)
	intPart := 1
	if absPrice >= 1 {
		intPart = int(math.Log10(absPrice)) + 1
	}
	if maxPrice < 0 {
		intPart++
	}
	return intPart + 1 + 2 + 2 // int + "." + 2dec + " ┤"
}

func getTerminalWidth() int {
	if ws := os.Getenv("COLUMNS"); ws != "" {
		if w, err := strconv.Atoi(ws); err == nil && w > 0 {
			return w
		}
	}
	return 120
}
