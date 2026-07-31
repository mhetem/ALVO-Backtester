# ALVO Backtester

> [!WARNING]
> **Work in progress — pre-alpha, nothing runs yet.**
> This repository currently contains a build plan and an empty directory skeleton. There is no
> working code, no installable artifact, and no API to call. Everything below describes what is
> being built, not what exists.

A web API and charting client for the Brazilian markets. It pulls OHLCV data from
[brapi.dev](https://brapi.dev), stores it in PostgreSQL, and serves it as candlestick or bar
charts across five timeframes — **5m, 15m, 30m, 1h and 1d**.

On top of that sit two things: a library of technical indicators written from scratch, and a
backtesting engine that runs strategies built from those indicators against any tracked symbol
over five years of history.

## What it will do

**Charts.** Any symbol in the tracked universe, any of the five timeframes, panned and zoomed
back through five years. Only 5m and 1d are ever fetched and stored — 15m, 30m and 1h are
resampled on read, so switching timeframes costs no API quota.

**Indicators.** Around forty of them, each implemented from scratch as a streaming O(1)-per-bar
update rather than a batch recompute: moving averages, oscillators, volatility and volume
measures, and price structure. Stack them on the chart, tune their parameters live.

**Backtests.** Compose indicators into a strategy through a rule builder — entry and exit
conditions, position sizing, stops and targets — then run it over any symbol and timeframe. Out
comes an equity curve, a drawdown plot, a trade list drawn onto the price chart, and the usual
risk statistics.

The engine is built to avoid the ways backtests flatter themselves: signals are evaluated on bar
close and filled at the *next* bar's open, costs and slippage are modelled, orders round to real
B3 lot and tick sizes, and symbols that leave an index keep being ingested rather than quietly
disappearing from the sample.

## Universe

Symbols are admitted from the union of the **IBOV**, **IBrX-100** and **SMLL** index
compositions — roughly 200 tickers. Admission is one-way: once a symbol is tracked it keeps
being ingested whether or not it stays in an index.

## Stack

| | |
|---|---|
| Backend | Go, standard-library `net/http` |
| Database | PostgreSQL, with `sqlc` and `goose` |
| Frontend | Svelte + TypeScript, [Lightweight Charts](https://github.com/tradingview/lightweight-charts) |
| Data | [brapi.dev](https://brapi.dev) |
| Deployment | Docker — one image, `docker compose up` |

## Status

See **[BUILD_PLAN.md](BUILD_PLAN.md)** for the full design, the phase breakdown, and the
reasoning behind each decision.

| Phase | |
|---|---|
| 0 · Foundations + Docker | not started |
| 1 · brapi client + symbol universe | not started |
| 2 · Candle ingestion + resampling | not started |
| 3 · Candle API + chart | not started |
| 4 · Auth | not started |
| 5–7 · Indicators | not started |
| 8 · Strategy model | not started |
| 9 · Backtest engine | not started |
| 10 · Metrics and reports | not started |
| 11 · Sweeps and walk-forward *(stretch)* | not started |
| 12–13 · Go live and deploy | not started |

## License

[Apache 2.0](LICENSE).
