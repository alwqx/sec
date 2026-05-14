# sec kline — Terminal Candlestick Chart

## Overview

`sec kline` renders OHLCV (Open/High/Low/Close/Volume) data as candlestick charts directly in the terminal, using Unicode block characters and ANSI color codes. No external charting library required.

## Architecture

```text
cmd/kline/kline.go          CLI command handler
    ↓
provider/sina/              Search security code → exchange mapping
    ↓
provider/eastmoney/         Fetch K-line history from East Money API
    ↓
render/candlestick.go       Core rendering engine (framework-agnostic)
    ↓
terminal stdout             Unicode + ANSI color output
```

### Layer Separation

- **`render/`** — Pure rendering engine, no dependencies on providers or CLI framework
  - `Candle` struct: minimal OHLCV interface (`Date`, `Open`, `Close`, `High`, `Low`, `Volume`)
  - `CandlestickConfig`: rendering parameters (height, width, paging, half-block, volume)
  - `Render(w io.Writer, candles []Candle, cfg CandlestickConfig) error`
- **`cmd/kline/`** — Cobra command, data fetching, `[]*eastmoney.Quote` → `[]render.Candle` adapter
  - `toCandles()` converts provider-specific types to the generic `Candle` type

This separation allows future data sources (e.g., bond yields) to reuse the rendering engine by implementing a `→ []render.Candle` adapter.

## Usage

```bash
# Basic usage: last 90 days of daily candles
sec kline 600036

# Custom date range
sec kline 600036 -b 20260101 -e 20260430

# Custom chart height
sec kline 600036 -H 30

# Half-block precision (2x vertical resolution via ▀/▄ characters)
sec kline 600036 --half-block

# Fixed candle width (paging mode), instead of auto-scaling
sec kline 600036 --paging

# Hide volume subgraph
sec kline 600036 --no-volume

# With 复权 type
sec kline 600036 -f qfq

# Alias
sec kl 600036
```

## Flags

| Flag           | Short | Default     | Description                                              |
| -------------- | ----- | ----------- | -------------------------------------------------------- |
| `--begin`      | `-b`  | 90 days ago | Start date `20260101`                                    |
| `--end`        | `-e`  | today       | End date `20260430`                                      |
| `--height`     | `-H`  | 20          | Price chart height in rows                               |
| `--half-block` |       | false       | Use `▀`/`▄` half-block chars for 2x vertical resolution  |
| `--paging`     |       | false       | Fixed 5-col candle width; navigate via `--begin`/`--end` |
| `--no-volume`  |       | false       | Hide volume subgraph                                     |
| `--fq`         | `-f`  | bfq         | 复权：bfq (none), qfq (front), hfq (post)                |

## Rendering Techniques

### Character Set

| Element                     | Character | Unicode | ANSI Color              |
| --------------------------- | --------- | ------- | ----------------------- |
| Bullish body (close ≥ open) | `█`       | U+2588  | Green (32)              |
| Bearish body (close < open) | `█`       | U+2588  | Red (31)                |
| Doji body (open == close)   | `━`       | U+2501  | Yellow (33)             |
| Wick / shadow               | `│`       | U+2502  | Green/Red per direction |
| Upper half block            | `▀`       | U+2580  | FG=upper, BG=lower      |
| Lower half block            | `▄`       | U+2584  | FG=lower, BG=upper      |
| Y-axis tick                 | `┤`       | U+2524  | Dim                     |
| Separator                   | `─`       | U+2500  | Dim                     |
| Volume bar                  | `█`       | U+2588  | Dim                     |

### Price → Row Mapping

```
row = chartHeight - 1 - round((price - minLow) / (maxHigh - minLow) * (chartHeight - 1))
```

- Row 0 = max price (highest)
- Row `chartHeight-1` = min price (lowest)
- Values clamped to [0, chartHeight-1]

### Half-Block Precision

When `--half-block` is enabled, the rendering engine builds the chart at 2x logical height, then collapses pairs of logical rows into single physical rows using Unicode half-block characters:

- `▀` (U+2580): Foreground color fills upper half, background color fills lower half
- `▄` (U+2584): Foreground color fills lower half, background color fills upper half

This gives 2x effective vertical resolution without consuming additional terminal rows.

The `mergePair()` function handles all combinations:

- Both body → `█`
- Both wick → `│`
- Body + wick → `▀` with mixed foreground/background colors
- Body/wick + empty → `▀` or `▄`

### Zoom vs Paging

**Zoom mode (default):** All candles scaled to fit terminal width.

- `candleWidth = chartAreaWidth / numCandles` (min 1)
- With narrow candles, wick `│` and body `█` share the same column

**Paging mode (`--paging`):** Fixed 5-column candle width.

- Shows as many candles as fit on one screen
- Use `--begin`/`--end` to navigate through time ranges

### Volume Subgraph

- 4 rows below the price chart, separated by a dotted line (`─`)
- Each bar height is proportional to `volume / maxVolume`
- Bars rendered in dim white via `█` characters

### Edge Cases Handled

| Case                       | Behavior                                  |
| -------------------------- | ----------------------------------------- |
| Zero candles               | No output                                 |
| Single candle              | Single column, centered                   |
| All same price (range = 0) | 2% padding added to price range           |
| Zero volume                | Volume subgraph empty (no crash)          |
| Doji (open == close)       | Yellow `━` dash character                 |
| Negative prices            | Supported via yAxisLabelWidth calculation |

## Output Example

```ansi
  49.20 ┤                 │
  48.80 ┤     │   ██       │
  48.40 ┤     │   ██       │       ██        ██
  48.00 ┤     │   ██   ██  │       ██        ██
  47.60 ┤     ██  ██   ██  │   ██  ██    ██  ██      ██
  47.20 ┤     ██  ██   ██  │   ██  ██    ██  ██  ██  ██
  46.80 ┤ ██  ██  ██   ██  ██  ██  ██    ██  ██  ██  ██
  46.40 ┤ ██  ██  ││   ██  ██  ██  ██  █ ││  ██  ██  ██
  46.00 ┤ ██  ││  ││   ││  ██  ││  ││  █ ││  ██  ││  ││
  45.60 ┤ ││  ││  ││   ││  ││  ││  ││  │ ││  ││  ││  ││
  45.20 ┤ ││  ││  ││   ││  ││  ││  ││  │ ││  ││  ││  ││
        └──┴──┴──┴──┴──┴──┴──┴──┴──┴──┴──┴──┴──┴──┴──┴──
          01/05  08    10    14    16    18    20    22
  ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─ ─
           ██    ██    ██    ██    ██    ██    ██    ██
```

Green `█` = bullish (close ≥ open), Red `█` = bearish (close < open).

## X-Axis Date Labels

Date labels use an adaptive format to remain readable at any data density:

| Condition                       | Format  | Example          | Width   |
| ------------------------------- | ------- | ---------------- | ------- |
| First label or month transition | `MM/DD` | `01/05`, `02/03` | 5 chars |
| Within same month               | `DD`    | `05`, `12`       | 2 chars |

Label spacing is calculated automatically:

- `step = ceil(numCandles / maxLabels)` where `maxLabels = totalWidth / (labelWidth + minGap)`
- Minimum 2-space gap between adjacent labels prevents crowding
- Dense data (100+ candles at 1-col width) shows roughly weekly labels (every 4-6 trading days)

## Design Decisions

1. **No external charting dependency.** Terminal candlestick rendering uses only Unicode block characters and ANSI colors from the Go standard library. Alternatives considered:
   - `ntcharts` (Bubble Tea) — too heavy for a CLI tool, requires TUI architecture
   - `candlePrintGo` — too limited (no axes, no volume, no half-block)
   - `termui` / `termdash` — require full terminal dashboard framework

2. **Modular `render` package.** The rendering engine accepts a generic `Candle` struct, making it reusable for any OHLCV data source (stocks via eastmoney, bonds, metals, etc.).

3. **Terminal width auto-detection.** Reads `$COLUMNS` env var; falls back to 120 columns.

4. **Conservative default height.** 20 rows for price chart + 6 rows for volume/x-axis = 26 total rows. Fits comfortably in most terminal windows.

## Future Enhancements

- True half-block rendering with mixed background colors (currently doubles height internally, then combines)
- Interactive paging (arrow keys to scroll through pages)
- Technical indicator overlays (MA, MACD)
- Multi-security overlay for comparison
- Bond yield candlestick support via `sec bond-history --kline`
