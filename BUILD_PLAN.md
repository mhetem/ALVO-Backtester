# ALVO Backtester — Build Plan

A web API + chart client for the Brazilian markets. Pulls OHLCV from **brapi.dev**, stores it
in **Postgres**, serves it as candles/bars across five timeframes, computes indicators written
from scratch, and backtests user-built strategies against any symbol and timeframe.

**Go + Postgres + Svelte, shipped as a Docker image.**

---

## Decisions locked

| Decision | Choice |
|---|---|
| Language / runtime | Go (stdlib `net/http` + `ServeMux` method patterns — no router dep) |
| Storage | PostgreSQL, `sqlc` for queries, `goose` for migrations |
| Frontend | Svelte 5 + Vite + TypeScript in `/frontend`, TradingView **Lightweight Charts** (Apache 2.0) |
| Packaging | **Docker.** Multi-stage build → one distroless image holding the Go binary, the built frontend, and the migrations. `docker compose up` is the only way anyone runs this |
| Auth | JWT access token (15 min) + opaque refresh token in Postgres. Same shape as Chirpy |
| Data source | brapi.dev. **Free tier for the whole build; one month of Pro at go-live to seed history** |
| Timeframes | **5m, 15m, 30m, 1h, 1d.** 1m is out of scope |
| Stored vs derived | **5m and 1d are stored.** 15m / 30m / 1h are resampled from 5m on read, never fetched |
| History target | **5 years**, growing forward from there |
| Universe | **Admission list**, not a filter: union of **IBOV + IBrX-100 + SMLL** (~200 tickers), hand-maintained, checked monthly. Admission is one-way — a ticker that leaves an index keeps ingesting forever |
| Deploy | Cheapest viable: one small ARM box running `docker compose`, app + Postgres + Caddy together |
| Price in DB | `NUMERIC(18,6)` |
| Price in the indicator/backtest pipeline | `float64` |
| Cash, P&L, fees | `int64` centavos — never float |
| Backtest jobs | Postgres-backed job rows + in-process worker pool. No Redis, no external queue |
| Strategy definition | Declarative JSON rule tree in `JSONB`, not embedded scripting |

### Why these

**Resample, don't re-fetch.** Five timeframes × N symbols would multiply brapi requests by five
for data that is arithmetically derivable. 15m, 30m and 1h all fall out of aggregating 5m —
they're clean divisors of the 420-minute session (3, 6 and 12 bars). The timeframe switcher in
the UI therefore costs zero API quota.

**5m is the floor, and that's a 5× storage decision.** A 1m base would be 420 bars per symbol per
session; 5m is 84. Over 5 years and ~200 symbols that's the difference between ~100M rows and
~21M — roughly 3.5 GB total with indexes, which is the difference between "fits on the cheapest
box that exists" and "needs a real database plan." Nothing below 5m is a timeframe this project
intends to serve, so the resolution buys nothing.

**The indexes decide what to ingest, and nothing else.** IBOV + IBrX-100 + SMLL is a defensible
definition of "the tradable Brazilian market" that someone else maintains and rebalances for us.
IBOV is largely a subset of IBrX-100 and SMLL picks up the small caps below it, so the union
lands around 200 tickers once the overlap is removed. The alternative — rolling our own
average-volume screen — is a parameter to tune, defend and re-tune forever.

Membership is **not** a backtest dimension. No strategy filters on it, nothing queries "was this
in IBOV on that date," and no interval bookkeeping exists. It is a one-way door: the monthly
check only ever *adds* tickers, leaving is a no-op, and once admitted a symbol is ingested for
good. That makes the whole mechanism a boolean column and a hand-written list — and it still
gets the survivorship property for free, because ex-members never stop producing data.

**1d is stored, not derived.** It's the exception to the resample rule, for three reasons: it's
available on the *free* tier at full depth, so daily backtests keep working without Pro; it saves
folding 103k 5m bars per symbol on every daily run; and the exchange's official daily bar includes
the closing auction, which summing 5m bars does not reproduce exactly. Stored 1d is authoritative.
At 1,230 rows per symbol it costs nothing.

**One image, no runtime dependencies.** The frontend is built at image-build time and embedded
via `embed.FS`; migrations likewise. The container needs nothing on disk and nothing on the host
but a `DATABASE_URL`. Distroless static means no shell and no package manager in the running
image — a smaller attack surface for something that holds an API token and password hashes.

**JSON rule tree over a scripting language.** A strategy has to round-trip through a UI builder,
be stored, diffed, versioned, and executed deterministically. Lua/Starlark gives more power and
buys sandboxing, non-determinism, and a builder UI that can't render arbitrary code. If a rule
tree ever proves too rigid, an `expr` escape-hatch node can be added inside it.

**float64 for indicators, int64 centavos for money.** Indicator math is smoothing and ratios
where float is correct and fast. Account balances are addition of discrete amounts where float
drift becomes a wrong equity curve. The boundary is explicit: `internal/backtest` converts once
at fill time and never converts back.

---

## What brapi actually gives us

Verified against brapi's docs and pricing, July 2026:

| | Free | Startup (R$99.99/mo) | Pro (R$116.66/mo) |
|---|---|---|---|
| Requests / month | 15,000 | 150,000 | 500,000 |
| Tickers per request | 1 | 10 | 20 |
| Timeframes | **daily only** | **daily only** | 1m, 5m, 15m, 30m, 60m, 1d, 1wk, 1mo |
| History depth | short (verify) | 1 year | 15+ years |
| Quote freshness | — | 15 min | 5 min |
| Futures / options | no | no | yes |

Endpoint: `GET https://brapi.dev/api/quote/{tickers}?range=&interval=&startDate=&endDate=&token=`
Ranges: `1d 2d 5d 7d 1mo 3mo 6mo 1y 2y 5y 10y ytd max`.

**The data plan, in order:**

1. **The entire build runs on four tickers, on Free.** PETR4, MGLU3, VALE3 and ITUB4 are free,
   tokenless and unlimited. Phases 0–11 are developed and demoed against those four with daily
   candles, spending zero of the 15k quota. Nothing in the plan blocks on paid data.
2. **Intraday is Pro-only, so 5m stays dark until go-live.** The ingestion and resampling code is
   timeframe-agnostic from Phase 2 and tested against synthetic 5m fixtures. Until the token is
   upgraded the only real series is `1d`. The charts are coarser; no phase is blocked.
3. **One month of Pro seeds the whole history.** This is the cheap path and it works out
   comfortably: Pro is 500k requests/month at 20 tickers per request. Backfilling ~200 symbols ×
   5 years of 5m, chunked conservatively at one request per symbol per month of history, is
   200 × 60 ≈ **12,000 requests** — under 3% of a single month's Pro quota. Even weekly chunking
   at one ticker per request lands around 52k. Buy one month, backfill everything, and the
   history is yours permanently in Postgres.
4. **After that month, the choice is honest.** Downgrade to Free and daily candles keep updating
   forever while the 5m head goes stale — historical intraday backtests still work perfectly,
   they just stop extending. Stay on Pro (R$116/mo) and everything stays current. Decide at
   go-live; the code doesn't care, and Phase 12 is written so either is a config change.
5. **Quota pressure is cadence, not history.** One request returns a whole range, so a daily
   close-of-session sync of 200 symbols is ~4,200 requests/month — comfortably inside Free's 15k,
   which is what makes dropping back to Free after the Pro month actually work. Refreshing 5m
   intraday is what costs real money.

---

## Target structure

```
ALVO-Backtester/
├── main.go                     # entry: config, db, migrate, router, worker pool
├── embed.go                    # embed.FS for frontend/dist and sql/schema
├── internal/
│   ├── config/                 # env parsing, one struct, fail fast
│   ├── brapi/                  # typed client: rate limiter, retry/backoff, quota accounting
│   ├── db/                     # sqlc-generated
│   ├── market/                 # symbols, sessions/holiday calendar, candle service, resampler
│   ├── ingest/                 # backfill + scheduled sync, gap detection
│   ├── indicator/              # the framework + every indicator, one file each
│   ├── strategy/               # rule-tree spec, validation, compilation, evaluation
│   ├── backtest/               # engine, broker sim, portfolio, metrics
│   ├── api/                    # handlers, middleware, DTOs
│   └── auth/                   # jwt, refresh tokens, password hashing
├── sql/
│   ├── schema/                 # goose migrations (embedded)
│   └── queries/                # sqlc source
├── data/
│   ├── b3_holidays.json
│   ├── contracts.json          # futures point values, lot sizes
│   └── indexes/                # IBOV/IBXX/SMLL composition, one file per rebalance
├── testdata/                   # committed OHLCV fixtures for the four free tickers
├── frontend/                   # Svelte: chart, indicator panel, strategy builder, reports
├── Dockerfile                  # multi-stage: node → go → distroless
├── docker-compose.yml          # app + postgres, production-shaped
├── docker-compose.dev.yml      # override: hot reload, exposed db port, bind mounts
├── docker-compose.prod.yml     # override: Caddy for TLS, restart policies, resource limits
├── Caddyfile
├── .dockerignore
├── Makefile                    # up / down / logs / migrate / sqlc / test
├── .github/workflows/          # test / vet / build / image
└── sqlc.yaml
```

---

## Phase 0 — Foundations + Docker

**Goal:** `docker compose up` gives a running server on a migrated database. Nothing installed
on the host but Docker.

- [ ] `go mod init github.com/mhetem/ALVO-Backtester`
- [ ] Deps: `jackc/pgx/v5`, `pressly/goose/v3`, `golang-jwt/jwt/v5`, `golang.org/x/crypto` (bcrypt).
      `sqlc` is a build tool, not a dep
- [ ] `internal/config`: `DATABASE_URL`, `PORT`, `BRAPI_TOKEN`, `JWT_SECRET`, `PLATFORM`.
      Missing required var = refuse to start, loudly
- [ ] `sqlc.yaml`: engine `postgresql`, `sql/schema` + `sql/queries`
- [ ] `GET /api/v1/healthz` → 200 + db ping
- [ ] Structured logging (`log/slog`), request-id middleware, panic recovery
- [ ] CI: `go vet ./...`, `go test ./...`, and a `docker build` so a broken image fails the PR

**Dockerfile — three stages:**

```dockerfile
FROM node:22-alpine AS frontend
WORKDIR /app/frontend
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /app/frontend/dist ./frontend/dist
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /alvo .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /alvo /alvo
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/alvo"]
```

- [ ] Dependency layers (`go mod download`, `npm ci`) copied before source so edits don't
      re-download the world. This is the difference between a 10-second rebuild and a 3-minute one
- [ ] `CGO_ENABLED=0` — pgx is pure Go, so the binary is static and distroless works
- [ ] `embed.go`: `//go:embed all:frontend/dist` and `//go:embed sql/schema/*.sql`. The final
      image copies **only the binary** — no `data/`, no migration files, nothing to go missing
      between build and deploy
- [ ] Goose runs migrations from the embedded `fs.FS` on startup, before the listener opens
- [ ] `.dockerignore`: `.git`, `frontend/node_modules`, `frontend/dist`, `*.md`, `.env`.
      Without `node_modules` in there the build context is hundreds of megabytes

**docker-compose.yml:**

- [ ] `db`: `postgres:17-alpine`, named volume, `POSTGRES_*` from `.env`, and a **healthcheck**
      (`pg_isready`). `app` uses `depends_on: { db: { condition: service_healthy } }` — without
      it the app races the database on every cold start and migrations fail intermittently
- [ ] `app`: built from the Dockerfile, `DATABASE_URL` pointing at `db:5432`, port published
- [ ] The db port is **not** published in the base compose. `docker-compose.dev.yml` overrides it
      to expose 5432 for psql/TablePlus
- [ ] `docker-compose.dev.yml`: bind-mounts the source and runs a hot-reload target so the inner
      loop isn't a rebuild. Vite dev server proxies `/api` to the app container
- [ ] `Makefile` wrapping the incantations: `up`, `up-dev`, `down`, `logs`, `psql`, `migrate`,
      `sqlc`, `test`
- [ ] `.env.example` committed; `.env` gitignored (already is)

**Done when:** a fresh clone with only Docker installed runs `make up` and gets a healthy
`/healthz` against a migrated database.

---

## Phase 1 — brapi client + symbol universe

**Goal:** we can name every tradable symbol and describe its contract mechanics.

```sql
CREATE TABLE symbols (
    id           BIGSERIAL PRIMARY KEY,
    ticker       TEXT NOT NULL UNIQUE,
    short_name   TEXT,
    long_name    TEXT,
    kind         TEXT NOT NULL,
    currency     TEXT NOT NULL DEFAULT 'BRL',
    lot_size     INT NOT NULL DEFAULT 100,
    tick_size    NUMERIC(18,8) NOT NULL DEFAULT 0.01,
    point_value  NUMERIC(18,8),
    active       BOOLEAN NOT NULL DEFAULT TRUE,
    tracked      BOOLEAN NOT NULL DEFAULT FALSE,
    first_seen   DATE,
    last_seen    DATE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

- [ ] `internal/brapi`: one client struct, token from config, `context.Context` everywhere
- [ ] Token-bucket rate limiter + exponential backoff on 429/5xx. A 429 must never become a
      partial write
- [ ] Quota accounting table (`brapi_usage(day DATE PRIMARY KEY, requests INT)`), incremented per
      call, exposed on an admin endpoint. Knowing you are at 14,800/15,000 *before* the backfill
      dies matters
- [ ] `kind`: `stock | fii | bdr | unit | index | future | crypto`
- [ ] `lot_size` 100 for stocks (fracionário is a different ticker, suffix `F`), 1 for FIIs
- [ ] `point_value` for futures: WIN = 0.20 BRL/point, WDO = 10.00 BRL/point. Seeded from
      `data/contracts.json`, not from brapi
- [ ] Sync command: pull the universe, upsert, mark absent tickers `active = false`

**The admission list:**

- [ ] `symbols.tracked BOOLEAN NOT NULL DEFAULT FALSE` — true means "ingest candles for this."
      **It never goes back to false.** That one column is the entire mechanism; there is no
      membership table, no validity intervals, no join on the read path
- [ ] `tracked` is distinct from `active`. `active` says the ticker still exists on brapi;
      `tracked` says we want its history. A delisted ex-member is `tracked = true, active = false`
      and simply stops producing new bars
- [ ] `data/indexes/<index>-<YYYY>-<MM>.json`, one hand-written file per check, committed.
      **Git is the record** — no scraper, no extra service, every change a reviewable diff.
      ~200 tickers three times a year is not worth automating until it is
- [ ] Sync command: union the newest files, set `tracked = true` on anything not already tracked.
      Tickers that disappeared from the files are **not touched**. The operation is idempotent and
      monotonic, which makes it safe to run carelessly
- [ ] Monthly job reports the diff — new admissions, and departures as information only. It does
      not mutate anything on its own
- [ ] **B3 rebalances quarterly** — cycles start the first business day of January, May and
      September, with prévias published beforehand. A monthly check is more often than strictly
      needed, which is the right call: it also catches mid-cycle entries from spin-offs and IPOs
      promoted into an index, which don't wait for the calendar

> Keeping ex-members is what avoids **survivorship bias** in everything ingested from project
> start onward: the losers stay in the sample instead of vanishing when they drop out of SMLL.
> The backfilled 5 years are a different story — see the traps.
- [ ] B3 holiday calendar in `data/b3_holidays.json` + a `sessions` lookup. Regular session
      10:00–17:00 America/Sao_Paulo. Needed by the resampler and by "N bars ago"
- [ ] Commands ship **inside the same image** as subcommands (`/alvo sync-symbols`), run via
      `docker compose run --rm app sync-symbols`. A second binary means a second image to keep in
      sync with the first

**Done when:** `docker compose run --rm app sync-symbols` populates `symbols`, and the four free
tickers resolve with correct lot sizes.

> Never delete a symbol row. A delisted ticker whose candles vanish is **survivorship bias** baked
> into every future backtest. `active = false` and keep the history.

---

## Phase 2 — Candle ingestion + resampling

**Goal:** OHLCV in Postgres, and any of the five timeframes derivable from it.

```sql
CREATE TABLE candles (
    symbol_id  BIGINT NOT NULL REFERENCES symbols(id) ON DELETE CASCADE,
    timeframe  TEXT NOT NULL,
    ts         TIMESTAMPTZ NOT NULL,
    open       NUMERIC(18,6) NOT NULL,
    high       NUMERIC(18,6) NOT NULL,
    low        NUMERIC(18,6) NOT NULL,
    close      NUMERIC(18,6) NOT NULL,
    adj_close  NUMERIC(18,6),
    volume     BIGINT NOT NULL,
    PRIMARY KEY (symbol_id, timeframe, ts)
);
```

- [ ] Column is `timeframe`, **not** `interval` — `interval` is a Postgres type name and using it
      as a column forces quoting everywhere and confuses sqlc
- [ ] `ts` is the **bucket open**, in UTC, always. Bucketing happens in exchange local time; storage
      is UTC. Mixing these is how you get 15m bars that straddle the open
- [ ] Pin the container's `TZ` to UTC in compose. A resampler that behaves differently on your
      machine than in the image is a bug you will chase for a day
- [ ] Only `5m` and `1d` are ever written. A `timeframe` outside those two reaching the insert path
      is a bug, and a CHECK constraint should say so
- [ ] Backfill command: `--symbol --timeframe --from --to`, idempotent, `ON CONFLICT DO UPDATE`,
      resumable — it will eventually run for hours against ~200 symbols and must survive being killed
- [ ] Gap detection against the session calendar: a missing bar on a trading day is a hole to refill;
      a missing bar on a holiday is correct
- [ ] `internal/market/resample.go`: `5m → 15m` (3 bars), `→ 30m` (6), `→ 1h` (12). O = first open,
      H = max, L = min, C = last close, V = sum. Buckets anchored to **session open**, not to
      midnight — the 420-minute session divides evenly by all three, so no partial buckets exist
- [ ] `1d` is read from storage, never folded from 5m
- [ ] Resampled series are computed on read and cached, not stored — until profiling says otherwise
- [ ] `testdata/`: committed OHLCV fixtures for the four free tickers plus a synthetic 5m series.
      Every test below runs offline against these. A test suite that needs a network call to brapi
      is a test suite that fails in CI and burns quota doing it
- [ ] Split/dividend adjustment: `adj_close` populated from brapi's `dividends` data. An
      unadjusted series turns every split into a fake -50% gap and every backtest crossing one
      into fiction
- [ ] Scheduled sync: after session close, refresh the last N bars per active symbol
- [ ] `ingest_runs` audit table: symbol, timeframe, range, status, http status, error, timings

**Done when:** five years of PETR4 daily candles are in Postgres, and the resampler turns the
synthetic 5m fixture into 15m/30m/1h bars matching a hand-checked reference.

> On Free, only `1d` has a source. The resampler is still written and tested now — against
> synthetic 5m fixtures — so that upgrading the token is a config change, not a phase.

**Sizing, so the deploy target isn't a surprise:** ~84 5m bars per session, ~246 sessions/year,
5 years ≈ 103k bars per symbol. At ~200 symbols that's ~21M rows ≈ 2.3 GB heap + ~0.9 GB for the
primary key. Call it **3.5 GB today, ~5 GB in five years** once index churn has added its ~20
tickers a year — plus a quarter-million 1d rows that round to nothing. That fits the cheapest box
in Phase 13 several times over. If it ever stops fitting, the escape hatches
in order are: `PARTITION BY LIST (timeframe)`, then prune inactive symbols to 1d only, then store
prices as `int` centavos instead of `NUMERIC` (~40% off the heap, at the cost of every read path).

---

## Phase 3 — Candle API + chart MVP

**Goal:** a real candlestick chart in a browser, end to end.

- [ ] `GET /api/v1/symbols?q=&kind=&limit=` — search/autocomplete
- [ ] `GET /api/v1/candles?symbol=PETR4&timeframe=15m&from=&to=&limit=` — hard cap on `limit`,
      cursor pagination by `ts`. `timeframe` accepts exactly `5m|15m|30m|1h|1d`; anything else is
      a 400 listing the valid set
- [ ] Response is columnar (`{ts:[], o:[], h:[], l:[], c:[], v:[]}`) not an array of objects —
      roughly 4× smaller over the wire and drops straight into the chart lib
- [ ] `ETag`/`Cache-Control` on closed historical ranges. A closed bar is immutable; say so
- [ ] `frontend/`: Svelte 5 + Vite + TS, Lightweight Charts v5 (`addSeries(CandlestickSeries, …)`)
- [ ] Symbol search, timeframe switcher (5m/15m/30m/1h/1d), candle ⇄ bar toggle
- [ ] Lazy-load older candles on pan-left via `setVisibleRange` subscription
- [ ] Crosshair readout: OHLCV + date
- [ ] Timeframes with no data render a clear empty state, not a broken chart
- [ ] Go serves the embedded `frontend/dist` as a SPA fallback — unknown non-`/api` paths return
      `index.html`. One container serves both; no nginx, no CORS

**Done when:** you open the app, type PETR4, and pan back through a year of daily candles smoothly.

---

## Phase 4 — Auth

**Goal:** users own their strategies and runs.

```sql
CREATE TABLE users (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email           TEXT NOT NULL UNIQUE,
    hashed_password TEXT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE refresh_tokens (
    token      TEXT PRIMARY KEY,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ
);
```

- [ ] `POST /api/v1/auth/register`, `/login`, `/refresh`, `/revoke`
- [ ] bcrypt for passwords, HS256 JWT for access (15 min), 60-day random refresh token
- [ ] `RequireAuth` middleware puts `user_id` in the request context
- [ ] Market data endpoints stay **public** — candles aren't user data and caching them per-user
      is waste. Only strategies, runs and watchlists sit behind auth
- [ ] `JWT_SECRET` and `BRAPI_TOKEN` arrive as env vars, never baked into the image. A secret in a
      layer is in the registry forever

**Done when:** register → login → call a protected endpoint → refresh → revoke all behave, with
tests covering expired and tampered tokens.

---

## Phase 5 — Indicator framework

**Goal:** one interface every indicator implements, streaming, with a name-based registry.

```go
type Candle struct {
	TS     time.Time
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
}

type Indicator interface {
	Update(Candle)
	Values() []float64
	Ready() bool
	Warmup() int
	Reset()
}
```

- [ ] **Streaming, not batch.** `Update` per bar in O(1). A batch API that recomputes from scratch
      per bar makes the backtest loop O(n²) and rules out live updates later. `Compute(series)` is
      a thin fold over `Update`, not a separate implementation
- [ ] Multi-output indicators return positional `Values()` aligned with a static `Outputs() []string`
      on the spec. String lookup per bar in the hot loop is avoidable, so avoid it
- [ ] `Warmup()` is how many bars before `Ready()`. The backtester skips those bars entirely — an
      EMA emitting values during its seeding window is a silent source of fake early trades
- [ ] Registry: `indicator.Register(name, Spec)` where `Spec` declares params (name, type, default,
      min/max) and builds an instance. The `?indicators=` query param, the strategy compiler, and
      the UI's parameter form all read from this one place
- [ ] `source` selector: `close | open | high | low | hl2 | hlc3 | ohlc4`
- [ ] Ring buffers for rolling windows; no reallocation per bar
- [ ] First five, to prove the shape: **SMA, EMA, RSI, MACD, Bollinger Bands**
- [ ] Golden-value tests: a fixed 200-bar fixture with expected outputs to 6 decimals, cross-checked
      against a reference implementation. Every later indicator gets the same treatment

**Done when:** `GET /api/v1/candles?...&indicators=ema:9,ema:21,rsi:14` returns aligned series, and
the golden tests pass.

> Warmup and NaN handling are where hand-written indicator libraries quietly go wrong. Decide once,
> here: values before `Ready()` are **not emitted** — not zero, not NaN. The API returns a `start`
> offset per indicator series so the chart aligns without padding.

---

## Phase 6 — Indicator library

**Goal:** breadth. Each is one file, one test file, registered.

- [ ] **Overlays:** WMA, HMA, DEMA, TEMA, VWAP, Keltner, Donchian, Parabolic SAR, SuperTrend, Ichimoku
- [ ] **Momentum:** Stochastic, Stochastic RSI, CCI, Williams %R, ROC, Momentum, ADX/+DI/−DI, Aroon
- [ ] **Volatility:** ATR, StdDev, Historical Volatility, Chaikin Volatility
- [ ] **Volume:** OBV, MFI, A/D Line, Chaikin Oscillator, Volume MA, VWMA
- [ ] **Structure:** Pivot Points (classic/Fibonacci), Williams Fractals, ZigZag
- [ ] Wilder's smoothing is its own helper — RSI, ATR and ADX all use it and all get it subtly
      wrong independently otherwise

**Done when:** every registered indicator has golden tests, and `GET /api/v1/indicators` lists the
catalogue with parameter schemas.

---

## Phase 7 — Indicators on the chart

- [ ] Indicator picker driven by `GET /api/v1/indicators` — the UI never hardcodes a list
- [ ] Overlay indicators on the price pane; oscillators in their own pane (Lightweight Charts panes)
- [ ] Per-indicator parameter editing, colour, visibility, removal
- [ ] Indicator set persisted per user per symbol (`chart_layouts` table)

**Done when:** you can stack EMA(9)/EMA(21) on price and RSI(14) below, tweak periods live, and it
survives a reload.

---

## Phase 8 — Strategy model

**Goal:** a strategy is data — storable, validatable, renderable in a builder.

```sql
CREATE TABLE strategies (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    spec        JSONB NOT NULL,
    version     INT NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

Spec shape:

```json
{
  "version": 1,
  "inputs": {
    "fast": {"indicator": "ema", "params": {"period": 9},  "source": "close"},
    "slow": {"indicator": "ema", "params": {"period": 21}, "source": "close"},
    "rsi":  {"indicator": "rsi", "params": {"period": 14}}
  },
  "entry": {
    "long": {"all": [
      {"crosses_above": ["fast", "slow"]},
      {"lt": ["rsi", 70]}
    ]}
  },
  "exit": {
    "long": {"any": [
      {"crosses_below": ["fast", "slow"]},
      {"stop_loss":   {"type": "atr", "period": 14, "mult": 2.0}},
      {"take_profit": {"type": "pct", "value": 0.05}}
    ]}
  },
  "sizing": {"type": "pct_equity", "value": 0.95},
  "costs":  {"brokerage_cents": 0, "fee_bps": 3.25, "slippage_bps": 5}
}
```

- [ ] Operands resolve to: a named input, a literal number, a price field (`close`, `high`, …),
      or `{"ref": ["fast", 1]}` for *n* bars back
- [ ] Comparators: `gt lt gte lte eq crosses_above crosses_below rising falling between`
- [ ] Combinators: `all any not`
- [ ] Validation on write: unknown indicator, out-of-range param, unresolvable operand, or a cycle
      is a 400 with a **JSON pointer to the offending node** — the builder highlights it inline
- [ ] Compile step: spec → a flat evaluation plan with indicators instantiated once and operands
      resolved to slot indices. Compile at run start, not per bar
- [ ] Editing a saved strategy bumps `version` and leaves prior backtest runs pointing at the spec
      they actually ran. A run whose strategy silently mutated underneath it is unreproducible
- [ ] CRUD + `POST /api/v1/strategies/validate` (dry-run, no write)
- [ ] Frontend: visual rule builder writing this JSON, plus a raw JSON editor

**Done when:** an EMA-cross strategy round-trips UI → JSON → validate → save → compile without loss.

---

## Phase 9 — Backtest engine

**Goal:** deterministic simulation over any symbol and timeframe.

```sql
CREATE TABLE backtest_runs (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    strategy_id   UUID NOT NULL REFERENCES strategies(id),
    spec          JSONB NOT NULL,
    symbol_id     BIGINT NOT NULL REFERENCES symbols(id),
    timeframe     TEXT NOT NULL,
    start_date    DATE NOT NULL,
    end_date      DATE NOT NULL,
    capital_cents BIGINT NOT NULL,
    status        TEXT NOT NULL,
    metrics       JSONB,
    error         TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at    TIMESTAMPTZ,
    finished_at   TIMESTAMPTZ
);

CREATE TABLE backtest_trades (
    run_id      UUID NOT NULL REFERENCES backtest_runs(id) ON DELETE CASCADE,
    seq         INT NOT NULL,
    side        TEXT NOT NULL,
    qty         BIGINT NOT NULL,
    entry_ts    TIMESTAMPTZ NOT NULL,
    entry_price NUMERIC(18,6) NOT NULL,
    exit_ts     TIMESTAMPTZ,
    exit_price  NUMERIC(18,6),
    pnl_cents   BIGINT,
    fees_cents  BIGINT NOT NULL,
    exit_reason TEXT,
    PRIMARY KEY (run_id, seq)
);

CREATE TABLE backtest_equity (
    run_id       UUID NOT NULL REFERENCES backtest_runs(id) ON DELETE CASCADE,
    ts           TIMESTAMPTZ NOT NULL,
    equity_cents BIGINT NOT NULL,
    PRIMARY KEY (run_id, ts)
);
```

- [ ] **The bar loop, and the one rule that matters:** at bar *i*, feed the candle to indicators,
      evaluate rules using values as of *close of bar i*, emit intents. Intents fill at **bar
      *i+1* open**. The engine must make it structurally impossible for a rule to read bar *i+1* —
      lookahead bias is the single most common way a backtester produces a beautiful lie
- [ ] Broker sim: market / limit / stop orders, plus bracket (stop-loss + take-profit) attached
      to a position
- [ ] **Intrabar ambiguity:** when a bar's high hits the take-profit *and* its low hits the stop,
      OHLC cannot say which came first. Assume the **stop** filled. Pessimistic and consistent;
      record the ambiguity count in the run metrics so you know how often it mattered
- [ ] Slippage: bps on the fill price, applied against you. Fees: `brokerage_cents` fixed +
      `fee_bps` on notional. B3 emolumentos are roughly 3.25 bps — configurable, not hardcoded
- [ ] Position sizing: `fixed_qty | pct_equity | fixed_cash | risk_pct` (the last sized off the
      ATR stop distance)
- [ ] Round to `lot_size` and `tick_size` from the symbol row. A backtest buying 137 shares of a
      100-lot stock is not a trade that existed
- [ ] Long-only first. Short selling in a second pass — borrow cost and shorting restrictions are
      their own problem
- [ ] Worker pool draining `backtest_runs WHERE status = 'queued'` with `FOR UPDATE SKIP LOCKED`.
      `SKIP LOCKED` is what makes `docker compose up --scale app=3` safe without a queue broker
- [ ] Container CPU/memory limits in compose, and a worker count derived from `GOMAXPROCS` — which
      respects the container's CPU quota in Go 1.25, so it needs no manual tuning
- [ ] `POST /api/v1/backtests` → 202 + run id; `GET /api/v1/backtests/{id}` → status/result
- [ ] Determinism: same spec + same candles = byte-identical metrics. A test asserts this

**Done when:** a buy-and-hold strategy returns exactly the underlying's return minus one round
trip of costs. That single test catches most engine bugs.

---

## Phase 10 — Metrics and reports

- [ ] Returns: total, CAGR, annualized vol
- [ ] Risk: max drawdown (value + duration), Sharpe, Sortino, Calmar
- [ ] Trades: count, win rate, profit factor, expectancy, avg win/avg loss, largest win/loss,
      max consecutive losses, avg holding period, time in market
- [ ] Benchmark comparison against buy-and-hold of the same symbol, and against IBOV
- [ ] `GET /api/v1/backtests/{id}/trades` and `/equity`
- [ ] Frontend: equity curve, underwater/drawdown plot, trade table, and **entry/exit markers drawn
      on the price chart** — seeing the trades on the candles is where strategy bugs become obvious
- [ ] Risk-free rate for Sharpe comes from the CDI/Selic, not from 0

**Done when:** a run produces a report you'd actually trust to reject a strategy.

---

## Phase 11 — Beyond a single run *(stretch)*

- [ ] Parameter sweeps: ranges per input, grid execution across the worker pool, results heatmap
- [ ] Walk-forward analysis: rolling in-sample optimize → out-of-sample test
- [ ] Portfolio backtests: one strategy across a basket, shared capital
- [ ] Futures: contract rollover and back-adjusted continuous series (WIN/WDO change contract
      every couple of months; a naive concatenation puts a fake gap at every roll)
- [ ] Strategy sharing / public read-only links

---

## Phase 12 — Go live: the Pro month

**Goal:** buy one month of Pro, turn four tickers into the whole market and five years of 5m,
then decide whether to keep paying.

Everything before this ran on four free tickers. This phase is mostly *operational* — the code
is already written and timeframe-agnostic. Do it against the production box, not a laptop.

- [ ] Upgrade the token. Set `BRAPI_TOKEN`; nothing else changes
- [ ] Write out the current IBOV, IBXX and SMLL compositions into `data/indexes/`, commit them, and
      run the sync to set `tracked = true` on the union. No judgment call, no screen to defend.
      Expect ~200 tickers
- [ ] Sanity-check the union size before spending anything. Every symbol is ~103k rows and a
      permanent share of the daily sync budget; if the number comes back at 400 rather than 200,
      something double-counted the overlap
- [ ] Dry-run the backfill: no writes, just count the requests it *would* make and print the total
      against the 500k quota. Run this before the real one, every time
- [ ] Determine the real chunk size empirically — how much 5m history brapi returns per request.
      The budget above assumed one month per request; measure it, don't assume it
- [ ] Backfill 1d first (cheap, works on any tier, and gives every symbol a usable chart
      immediately), then 5m
- [ ] Run it resumable and rate-limited, in the background, in the container. It will take hours.
      `docker compose run -d --rm app backfill --universe`
- [ ] Verify: row counts per symbol against expected session counts, gap report clean, spot-check
      a known split (e.g. a 1:2) to confirm adjustment landed
- [ ] `pg_dump` immediately afterwards and get it off the box. **This dump is the expensive
      artifact** — it cost a month of Pro. Losing it means paying again
- [ ] Then decide: stay on Pro (~R$116/mo, 5m stays current) or drop to Free (daily keeps
      updating, 5m freezes at the backfill date, historical backtests unaffected). Flip
      `INGEST_INTRADAY=false` and the scheduler stops asking for what Free won't serve

**Done when:** the universe is loaded, verified, dumped off-box, and the daily sync runs inside
the Free quota.

---

## Phase 13 — Hardening and deploy

**Target: one small ARM box, `docker compose`, everything on it.** No managed Postgres — a managed
instance holding 4 GB costs more per month than the entire server. Postgres runs next to the app
on a named volume, and the backup story below is what makes that safe.

| Option | Cost | Notes |
|---|---|---|
| **Oracle Cloud Always Free** (Ampere A1) | **R$0** | 2 OCPU / 12 GB ARM since the June 2026 cut — still 3× what this needs. Has **São Paulo and Vinhedo** regions, so latency from Brazil is single-digit ms. Catch: A1 capacity is frequently unavailable, and free accounts can be reclaimed |
| **Hetzner CAX11** (ARM) | ~€5.99/mo | 2 vCPU / 4 GB / 40 GB. Boringly reliable, but EU-only — ~200 ms from Brazil |

- [ ] Try Oracle first; it's free and it's in Brazil. Keep Hetzner as the fallback for when A1
      capacity won't provision
- [ ] **Both are ARM**, which makes the `linux/arm64` build load-bearing rather than a nicety
- [ ] Multi-arch image build (`linux/amd64`, `linux/arm64`) via buildx in CI, pushed to GHCR on tag
- [ ] `docker-compose.prod.yml`: adds **Caddy** as the only published service, reverse-proxying the
      app. Automatic Let's Encrypt TLS from a two-line Caddyfile — the app still serves the SPA
      itself, Caddy only terminates HTTPS
- [ ] `restart: unless-stopped` on every service; memory limits so a runaway backtest can't OOM
      Postgres
- [ ] Postgres tuning for a small box: `shared_buffers`, `work_mem`, `effective_cache_size` set
      explicitly. Defaults assume far more RAM than 4 GB
- [ ] **Backups.** Nightly `pg_dump | gzip` to object storage (Cloudflare R2 / Backblaze B2, both
      effectively free at this size). Test a restore *once*, for real. The candle store is ~4 GB
      that cost a month of Pro to acquire
- [ ] `HEALTHCHECK` hitting `/healthz` — a distroless image has no `curl`, so this is an
      `/alvo healthcheck` subcommand hitting itself
- [ ] Graceful shutdown on SIGTERM: stop accepting, drain in-flight requests, mark running
      backtests back to `queued`. Docker sends SIGTERM and waits 10s — an orphaned `running` row
      that no worker owns is a job that never finishes
- [ ] Per-IP and per-user rate limiting on the API
- [ ] Response caching for hot candle ranges
- [ ] Metrics + `pprof` behind auth
- [ ] Backtest timeout + memory ceiling per run
- [ ] Pin base images by digest once the build is stable

**Done when:** `docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d` on a fresh
box serves the app over HTTPS, and a restore from last night's dump has been proven to work.

---

## Known traps

Collected here because the code carries no comments — this is where the reasoning lives.

1. **Lookahead bias.** Signal on close of *i*, fill at open of *i+1*. Structural, not by convention.
2. **Survivorship bias — handled going forward, baked into the backfill.** Delisted symbols must
   stay in the DB, and ex-index-members must keep ingesting; `tracked` never flipping back to
   false is what guarantees both. But the 5 years bought in Phase 12 are, unavoidably, *today's*
   index constituents — companies that survived and grew into an index. Nothing recovers the
   ones that dropped out before the backfill, short of historical B3 portfolios. The bias is
   real, bounded, and decays as churn accumulates forward. State it when reporting any result
   that leans on the pre-launch window; don't pretend the sample is clean.
3. **Corporate actions.** Unadjusted prices make every split a fake crash.
4. **Intrabar ambiguity.** OHLC cannot order events inside a bar. Pick pessimistic, count it, report it.
5. **Session-aligned buckets.** Resampling on wall-clock boundaries misaligns every intraday bar
   against the 10:00 open.
6. **Warmup leakage.** Indicators emitting values before they're seeded generate phantom early trades.
7. **Float money.** Equity curves that drift a few centavos per trade over 5,000 trades are wrong
   by a visible amount.
8. **Overfitting.** Phase 11's parameter sweeps make it trivially easy to find a strategy that fits
   noise. Walk-forward is the mitigation, and it belongs in the same phase as the sweeps, not later.
9. **The volume is the asset.** `docker compose down -v` deletes the candle store. Rebuilding it
   costs a month of Pro. Never put `-v` in a Makefile target that isn't named something alarming.
10. **The daily bar is not the sum of the 5m bars.** B3's official daily close comes from the
    closing auction, which intraday bars don't capture. This is why `1d` is stored rather than
    folded. If a reconciliation check ever flags the two disagreeing, the stored `1d` wins.
11. **Development ran on four large caps.** PETR4, VALE3, ITUB4 and MGLU3 are liquid, gap rarely,
    and never halt. The first backfill of illiquid tickers will surface zero-volume bars, missing
    sessions and stale prices that four blue chips never exercised. Expect Phase 12 to find bugs
    in code that looked finished.

---

## Deferred to post-launch

- **Real-time / streaming.** Explicitly out of scope for v1. The groundwork is already here — the
  streaming `Indicator` interface takes bars one at a time, and SSE over the candle service is a
  small addition — but shipping it means intraday polling cadence, which means permanent Pro.
  Revisit once the thing is deployed and actually used.
- **Timeframes below 5m.** Not a resolution this project serves. Reopening it means a 5× row
  count and a re-backfill.
- **Short selling, portfolio backtests, parameter sweeps, futures rollover.** All in Phase 11,
  all optional, none blocking a deploy.

## Open questions

- **Free-tier history depth.** Startup is documented at 1 year, Pro at 15+; the Free number wasn't
  confirmed. Measure it against PETR4 in Phase 2 — it sets how much daily history the whole
  development phase has to work with.
- **5m history depth on Pro, and per-request chunk size.** Pro advertises 15+ years, but
  intraday retention is usually shorter than daily on this class of provider, and how much 5m
  comes back per request is unmeasured. Both feed directly into Phase 12's budget. Measure on
  day one of the Pro month, before launching the full backfill.
- **Futures coverage.** brapi lists futures as Pro-only; whether WIN/WDO come with usable
  intraday history is unverified. Phase 11's rollover work depends on the answer, and it can't
  be checked until the Pro month.
- **Backfilling admissions made after the Pro month.** A ticker promoted into an index later can
  only be given 5m history while a Pro token is live. On Free it gets daily from admission
  onward and nothing intraday, ever. If the plan is to drop to Free, either accept a universe
  where newer members have shallower intraday history, or batch admissions and buy a second Pro
  month once it's worth it.
