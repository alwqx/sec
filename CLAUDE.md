# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

`sec` is a terminal CLI for querying Chinese (A-share, HK) and US stock/financial market data, rendering results as tables and in-terminal candlestick (K-line) charts. Data is scraped from Chinese financial websites (Sina, East Money, CNINFO). Go module: `github.com/alwqx/sec`.

All user-facing output (table headers, error messages, dividend descriptions) is in **Chinese by design**. Code comments and docs are Chinese.

## Common Commands

```bash
# Build (produces `main` binary in repo root)
go build

# Run directly
go run .

# Run all tests
go test -v ./...

# Run tests for a single package
go test -v ./provider/sina/...

# Run a single test
go test -v -run TestName ./render/

# Format / vet
gofmt -w .
go vet ./...
staticcheck ./...   # optional, not in CI

# Smoke-test a command (network required)
go run . bond
go run . ipo list
```

CI only runs `go test -v ./...` on every push. Release builds (linux/windows/darwin × amd64/arm64) trigger on GitHub Release creation via ldflags-injected version.

## Architecture

Layered design, one package per concern:

- **`cmd/`** — Cobra command handlers. Each feature exposes a `NewXxxCLI()` constructor; all are registered in `cmd/cmd.go` `NewCLI()`. New commands are added here.
- **`provider/`** — Data fetching & parsing, one subpackage per upstream API:
  - `sina/` — search, quote, profile, dividends, corporate info. Handles GBK/GB18030 decoding. Quote parsing differs for A-share / HK / US (fixed-width comma-delimited formats).
  - `eastmoney/` — OHLCV kline history, IPO calendar, financial reports (balancesheet). Wraps East Money push2/push2his JSON APIs.
  - `cninfo/` — CNINFO disclosure platform (巨潮资讯网): announcements, IPO prospectus PDF. Caches JSON + stock-list under `~/.sec/cache/`.
  - `bond/`, `metal/` — US Treasury yields, precious metals.
- **`render/`** — Pure terminal candlestick chart (`candlestick.go`): ANSI-colored OHLCV with volume subgraph, half-block resolution, optional overlay lines.
- **`types/`** — Shared domain types: `SecurityType`, exchange constants (sse/szse/bse/hk/ny/nasdaq), `InfoOptions`.
- **`utils/`** — Shared: `MakeRequest` (HTTP + User-Agent + timeout), `ParseBeginEnd` (--begin/--end flags), `SecDir` (~/.sec/...), `HumanByte`/`HumanNum`, time layouts, `WriteJson`.
- **`version/`** — Build-time linker-injected `Version`, `BuildTime`, `GitCommit`. `Version` also feeds the HTTP User-Agent via `utils.UserAgent()`.

## Key Conventions

- **Context propagation**: provider funcs take `context.Context` and use `slog.DebugContext`/`slog.ErrorContext` for structured logging.
- **Output**: commands write to `cmd.OutOrStdout()` / `io.Writer`, not direct `fmt.Print`.
- **Testing**: `testify/require` for assertions; `gock` for HTTP mocking. Live/integration tests are guarded with `t.Skip("仅用于开发调试")`.
- **No Makefile / linter config committed** — formatting is standard `gofmt`; `staticcheck` is recommended but not enforced in CI.
- **Data bugs** most often live in per-market string parsing (A vs HK vs US formats differ in `provider/sina/quote.go`).

## CHANGELOG

每次创建 PR 时，必须将本次改动汇总到 `CHANGELOG.md` 中。格式遵循现有约定：以 `### vX.Y.Z` 版本标题开头，下接编号列表，用中文逐条描述变更（新功能、修复、重构等），与现有条目风格保持一致。

## Adding a New Command

1. Create a package under `cmd/` exporting `NewXxxCLI() *cobra.Command`.
2. Implement data fetching in `provider/<source>/` if it needs a new upstream API.
3. Register the command in `cmd/cmd.go` `NewCLI()`.
4. Add a design doc under `docs/` if behavior is non-trivial.
