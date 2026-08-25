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
| Timeframes | ~~daily only~~ **all of them** (measured) | **daily only** | 1m, 5m, 15m, 30m, 60m, 1d, 1wk, 1mo |
| History depth | ~~short (verify)~~ **10y daily, ~60d intraday** (measured) | 1 year | 15+ years |
| Quote freshness | — | 15 min | 5 min |
| Futures / options | no | no | yes |

Endpoint: `GET https://brapi.dev/api/quote/{tickers}?range=&interval=&startDate=&endDate=&token=`
Ranges: `1d 2d 5d 7d 1mo 3mo 6mo 1y 2y 5y 10y ytd max`.

**The data plan, in order:**

1. **The entire build runs on four tickers, on Free.** PETR4, MGLU3, VALE3 and ITUB4 are free,
   tokenless and unlimited. Phases 0–11 are developed and demoed against those four with daily
   candles, spending zero of the 15k quota. Nothing in the plan blocks on paid data.
2. ~~**Intraday is Pro-only, so 5m stays dark until go-live.**~~ **Corrected in Phase 2: the free
   tier serves 5m for the free tickers, about 60 days deep.** The ingestion and resampling code is
   timeframe-agnostic and tested against both a synthetic 5m fixture and a real one. What Pro buys
   is *depth*, not access: five years of 5m instead of two months. No phase is blocked either way,
   and the 5m path is now exercisable against real bars before the Pro month rather than after.
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

- [x] `go mod init github.com/mhetem/ALVO-Backtester`
- [x] Deps: `jackc/pgx/v5`, `pressly/goose/v3`. `sqlc` is a build tool, not a dep.
      `golang-jwt/jwt/v5` and `golang.org/x/crypto` land in Phase 4 — `go mod tidy` drops a
      require nothing imports, so adding them early is fiction. *Phase 4 note: it landed as
      `golang-jwt/jwt/v5`, `google/uuid` and `alexedwards/argon2id`, with `x/crypto` arriving
      indirectly underneath argon2id rather than as a direct import*
- [x] `internal/config`: `DATABASE_URL`, `PORT`, `BRAPI_TOKEN`, `JWT_SECRET`, `PLATFORM`.
      Missing required var = refuse to start, loudly
- [x] `sqlc.yaml`: engine `postgresql`, `sql/schema` + `sql/queries`
- [x] `GET /api/v1/healthz` → 200 + db ping
- [x] Structured logging (`log/slog`), request-id middleware, panic recovery
- [x] CI: `gofmt`, `go vet ./...`, `staticcheck ./...`, `gosec ./...`, `go test -race ./...`, and a
      `docker build` so a broken image fails the PR. Linters are installed with `go install
      <module>@latest` rather than marketplace actions — one less mutable third party holding a
      workflow token, and the tool is always built with the toolchain `setup-go` resolved

**Dockerfile — three stages, plus a `dev` stage the compose override targets:**

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

- [x] Dependency layers (`go mod download`, `npm ci`) copied before source so edits don't
      re-download the world. This is the difference between a 10-second rebuild and a 3-minute one
- [x] `CGO_ENABLED=0` — pgx is pure Go, so the binary is static and distroless works
- [x] `embed.go`: `//go:embed all:frontend/dist` and `//go:embed sql/schema/*.sql`. The final
      image copies **only the binary** — no `data/`, no migration files, nothing to go missing
      between build and deploy
- [x] Goose runs migrations from the embedded `fs.FS` on startup, before the listener opens
- [x] `.dockerignore`: `.git`, `frontend/node_modules`, `frontend/dist`, `*.md`, `.env`.
      Without `node_modules` in there the build context is hundreds of megabytes

**docker-compose.yml:**

- [x] `db`: `postgres:17-alpine`, named volume, `POSTGRES_*` from `.env`, and a **healthcheck**
      (`pg_isready`). `app` uses `depends_on: { db: { condition: service_healthy } }` — without
      it the app races the database on every cold start and migrations fail intermittently
- [x] `app`: built from the Dockerfile, `DATABASE_URL` pointing at `db:5432`, port published
- [x] The db port is **not** published in the base compose. `docker-compose.dev.yml` overrides it
      to expose 5432 for psql/TablePlus
- [x] `docker-compose.dev.yml`: bind-mounts the source and targets the `dev` stage (`go run .`,
      `make restart` to pick up changes — no watcher dependency in Phase 0). Vite dev server
      proxies `/api` to the app container
- [x] `Makefile` wrapping the incantations: `up`, `up-dev`, `down`, `logs`, `psql`, `migrate`,
      `sqlc`, `test`. *Phase 2 note: `check` type-checks the frontend inside the `web` container
      rather than on the host. The dev override mounts a named volume over
      `frontend/node_modules`, so the host copy of that directory is an empty mount point and a
      host-side `npm run check` finds no `svelte-check`. CI is unaffected — a fresh checkout has
      no volume shadowing it and runs `npm ci` on the runner*
- [x] `.env.example` committed; `.env` gitignored (already is)

**Done when:** a fresh clone with only Docker installed runs `make up` and gets a healthy
`/healthz` against a migrated database.

> **Status: done and verified end to end.** `make up` on a clean volume brings up Postgres, waits
> on its healthcheck, runs `00001_init.sql` via goose and answers `/api/v1/healthz` with
> `{"status":"ok","db":"up"}`. Restarting reports "no migrations to run" — the migration path is
> idempotent. The runtime image is **4.8 MB**, runs as `nonroot`, and has no shell (`exec sh`
> fails, as it should). `TZ` and the database timezone both resolve to UTC. The db port is
> unpublished in the base compose and reachable only under the dev override. `docker-compose.prod.yml`
> resolves with `app.ports = []` behind Caddy on 80/443, and the Caddyfile passes `caddy validate`.
>
> Two things are deliberately not done yet: `make sqlc` fails with "no queries contained in
> /src/sql/queries" until Phase 1 writes some — it parses the schema fine — and multi-arch
> (`linux/arm64`) image builds belong to Phase 13.

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

- [x] `internal/brapi`: one client struct, token from config, `context.Context` everywhere
- [x] Token-bucket rate limiter + exponential backoff on 429/5xx. A 429 must never become a
      partial write
- [x] Quota accounting table (`brapi_usage(day DATE PRIMARY KEY, requests INT)`), incremented per
      call, exposed on an admin endpoint. Knowing you are at 14,800/15,000 *before* the backfill
      dies matters
- [x] `kind`: `stock | fii | bdr | unit | index | future | crypto`
- [x] `lot_size` 100 for stocks (fracionário is a different ticker, suffix `F`), 1 for FIIs
- [x] `point_value` for futures: WIN = 0.20 BRL/point, WDO = 10.00 BRL/point. Seeded from
      `data/contracts.json`, not from brapi
- [x] Sync command: pull the universe, upsert, mark absent tickers `active = false`

**The admission list:**

- [x] `symbols.tracked BOOLEAN NOT NULL DEFAULT FALSE` — true means "ingest candles for this."
      **It never goes back to false.** That one column is the entire mechanism; there is no
      membership table, no validity intervals, no join on the read path
- [x] `tracked` is distinct from `active`. `active` says the ticker still exists on brapi;
      `tracked` says we want its history. A delisted ex-member is `tracked = true, active = false`
      and simply stops producing new bars
- [x] `data/indexes/<index>-<YYYY>-<MM>.json`, one hand-written file per check, committed.
      **Git is the record** — no scraper, no extra service, every change a reviewable diff.
      ~200 tickers three times a year is not worth automating until it is
- [x] Sync command: union the newest files, set `tracked = true` on anything not already tracked.
      Tickers that disappeared from the files are **not touched**. The operation is idempotent and
      monotonic, which makes it safe to run carelessly
- [x] Monthly job reports the diff — new admissions, and departures as information only. It does
      not mutate anything on its own
- [x] **B3 rebalances quarterly** — cycles start the first business day of January, May and
      September, with prévias published beforehand. A monthly check is more often than strictly
      needed, which is the right call: it also catches mid-cycle entries from spin-offs and IPOs
      promoted into an index, which don't wait for the calendar

> Keeping ex-members is what avoids **survivorship bias** in everything ingested from project
> start onward: the losers stay in the sample instead of vanishing when they drop out of SMLL.
> The backfilled 5 years are a different story — see the traps.
- [x] B3 holiday calendar in `data/b3_holidays.json` + a `sessions` lookup. Regular session
      10:00–17:00 America/Sao_Paulo. Needed by the resampler and by "N bars ago"
- [x] Commands ship **inside the same image** as subcommands (`/alvo sync-symbols`), run via
      `docker compose run --rm app sync-symbols`. A second binary means a second image to keep in
      sync with the first. *Phase 2 note: the Makefile targets pass `--build`, because
      `compose run` otherwise reuses whatever image exists and silently runs last week's binary —
      which reads as "my change did nothing" rather than as a stale build*

**Done when:** `docker compose run --rm app sync-symbols` populates `symbols`, and the four free
tickers resolve with correct lot sizes.

> Never delete a symbol row. A delisted ticker whose candles vanish is **survivorship bias** baked
> into every future backtest. `active = false` and keep the history.

### Decisions this phase forced

**The kind enum has no `etf`.** BOVA11 and friends are structurally units to the classifier —
`XXXX11` cannot be told apart from a FII or a unit by ticker alone. It costs nothing today because
IBOV/IBrX/SMLL are equity indexes containing neither, and the per-ticker override list in
`data/contracts.json` handles any exception by hand. Reopen it if FIIs ever get admitted; that's a
migration on one CHECK constraint.

**`ClassifyTicker` refuses rather than guesses.** An unclassifiable ticker in an admission file
fails the whole sync instead of landing as a default `stock`. The files are hand-written and
committed, so a typo is exactly the thing a loud failure should catch. The trap it avoids is real:
`KLBN11` matches the futures pattern (`KLB` + month code `N` + `11`), so futures are recognised by
an explicit root list, never by shape alone.

**Contract roots are seeded as symbols, `tracked = false`.** `WIN` as a row is the continuous
back-adjusted series Phase 11 will build; the dated contracts (`WINZ25`) are separate symbols that
only exist once a Pro token can enumerate them.

**Every HTTP response counts against quota, including retries.** A 429 that gets retried three
times is recorded as three requests. Pessimistic on purpose — the number exists to stop a backfill
before it dies, and overcounting fails safe in that direction.

**Enrichment is bounded by the token.** Without `BRAPI_TOKEN` the sync only asks brapi about the
four free tickers and derives everything else locally, so `sync-symbols` is offline-safe and costs
zero quota by default. `--dry-run` touches neither the network nor the database and is what the
monthly admission check runs.

**The names come from brapi, the mechanics never do.** `lot_size`, `tick_size` and `point_value`
are derived from the ticker shape or read from `data/contracts.json`. brapi only ever fills in
`short_name`, `long_name` and `currency`. A backtest's fill arithmetic does not depend on a
third-party field that might change shape.

> **Status: done and verified.** `make sqlc` generates cleanly — the `sqlc.yaml` overrides land
> `float64` on the numerics and `*time.Time` on the nullable dates, so no `pgtype` reaches the
> service layer. `make up` applies `00002` and `00003`, and `docker compose run --rm app
> sync-symbols` populates `symbols` and `brapi_usage`. Unit tests cover the client, the limiter,
> the classifier, the calendar and the admission diff, all offline against fixtures.
>
> Carried into later phases, deliberately:
> - **The admission list is a placeholder.** `data/indexes/dev-2026-08.json` holds the four free
>   tickers and nothing else. Real IBOV/IBrX-100/SMLL compositions are Phase 12's first task, by
>   the plan's own sequencing — inventing a 200-ticker list now would commit a wrong file to the
>   record that git is supposed to keep.
> - ~~**`/api/v1/admin/brapi-usage` is unauthenticated.**~~ **Closed in Phase 4** — it is now the
>   one endpoint behind `RequireAuth`, and was the only non-public one that existed to put there.
> - **The 2020–2023 holidays need checking against B3's published calendar.** The generated file
>   assumes B3 dropped the São Paulo municipal holidays (25/01, 09/07) from 2022 and picked up
>   20/11 again in 2024 as a national holiday. Easter-derived dates are computed, not typed, so
>   Carnival, Good Friday and Corpus Christi are safe. Gap detection in Phase 2 depends on the
>   rest being right for the backfill window.
> - **`/available` response shape is still unobserved.** `/quote` is confirmed against the four
>   free tickers — `longName` and `currency` come back correctly, and quota accounting recorded
>   exactly four requests for four tickers at one per request. `/available` returning
>   `{"indexes":[],"stocks":[]}` as arrays of strings remains modelled from the docs, and only
>   affects `--prune`, which is off by default.
> - **brapi's `shortName` echoes the ticker on the free tier**, so `symbols.short_name` duplicates
>   `symbols.ticker` for anything synced from the API; `long_name` carries the real name. Phase 3's
>   autocomplete should fall back to `long_name` rather than trusting `short_name` to be a display
>   name.

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

- [x] Column is `timeframe`, **not** `interval` — `interval` is a Postgres type name and using it
      as a column forces quoting everywhere and confuses sqlc
- [x] `ts` is the **bucket open**, in UTC, always. Bucketing happens in exchange local time; storage
      is UTC. Mixing these is how you get 15m bars that straddle the open
- [x] Pin the container's `TZ` to UTC in compose. A resampler that behaves differently on your
      machine than in the image is a bug you will chase for a day
- [x] Only `5m` and `1d` are ever written. A `timeframe` outside those two reaching the insert path
      is a bug, and a CHECK constraint should say so
- [x] Backfill command: `--symbol --timeframe --from --to`, idempotent, `ON CONFLICT DO UPDATE`,
      resumable — it will eventually run for hours against ~200 symbols and must survive being killed
- [x] Gap detection against the session calendar: a missing bar on a trading day is a hole to refill;
      a missing bar on a holiday is correct
- [x] `internal/market/resample.go`: `5m → 15m` (3 bars), `→ 30m` (6), `→ 1h` (12). O = first open,
      H = max, L = min, C = last close, V = sum. Buckets anchored to **session open**, not to
      midnight — the 420-minute session divides evenly by all three, so no partial buckets exist
- [x] `1d` is read from storage, never folded from 5m
- [x] Resampled series are computed on read and cached, not stored — until profiling says otherwise
      *(computed on read by `market.CandleService.Load`, which Phase 3's handler wraps rather than
      reimplements; the cache waits for profiling, per the bullet)*
- [x] `testdata/`: committed OHLCV fixtures for the four free tickers plus a synthetic 5m series.
      Every test below runs offline against these. A test suite that needs a network call to brapi
      is a test suite that fails in CI and burns quota doing it
- [x] Split/dividend adjustment: `adj_close` populated from brapi's `dividends` data. An
      unadjusted series turns every split into a fake -50% gap and every backtest crossing one
      into fiction *(brapi returns `adjustedClose` inline on each daily bar; a separate
      `dividends` fetch turned out to be unnecessary — see the notes)*
- [x] Scheduled sync: after session close, refresh the last N bars per active symbol
- [x] `ingest_runs` audit table: symbol, timeframe, range, status, http status, error, timings

**Done when:** five years of PETR4 daily candles are in Postgres, and the resampler turns the
synthetic 5m fixture into 15m/30m/1h bars matching a hand-checked reference.

`/alvo candles --symbol PETR4 --timeframe 1h` is how you watch that happen against the real store:
it reads 5m through the same service the API will use, folds it, prints the candles, and closes
with a reconciliation of volume, high and low against the base bars it folded. A resampler that
loses or invents volume says so on the last line.

> On Free, only `1d` has a source. The resampler is still written and tested now — against
> synthetic 5m fixtures — so that upgrading the token is a config change, not a phase.

**Sizing, so the deploy target isn't a surprise:** ~84 5m bars per session, ~246 sessions/year,
5 years ≈ 103k bars per symbol. At ~200 symbols that's ~21M rows ≈ 2.3 GB heap + ~0.9 GB for the
primary key. Call it **3.5 GB today, ~5 GB in five years** once index churn has added its ~20
tickers a year — plus a quarter-million 1d rows that round to nothing. That fits the cheapest box
in Phase 13 several times over. If it ever stops fitting, the escape hatches
in order are: `PARTITION BY LIST (timeframe)`, then prune inactive symbols to 1d only, then store
prices as `int` centavos instead of `NUMERIC` (~40% off the heap, at the cost of every read path).

### What measuring brapi actually changed

Four things were assumed in Phase 0 and turned out to be wrong. All were measured against the
free tickers on 2026-08-20, tokenless, and the raw responses are committed under `testdata/`.

**1. Intraday is not Pro-only.** `?interval=5m` returns real 5m bars on the free tier for the four
free tickers. Retention is the catch: `range=3mo`, `range=1y` and `range=max` all return the same
~60 days of history and stop. So the plan's "5m stays dark until go-live" is too pessimistic —
5m is developable and testable against *real* data now, it just cannot reach back more than two
months. The Pro month is still what buys five years of it.

**2. Free daily history is 10 years, not "short (verify)".** PETR4 at `range=10y&interval=1d`
returns 2,482 bars back to 2016-08-22. That closes the open question and means the entire
development phase has more daily history than the 5-year target needs.

**3. brapi serves two different 5m series depending on the requested range, and the short one is
wrong.** This is the finding that changes the ingest design. Requesting the *same day* for the
*same ticker* with `range=5d` or `range=1mo` returns one series; `range=3mo` or `range=1y` returns
another. They are not a time shift or an adjustment factor — not one bar of 291 matched between
the groups. Folding each back into a daily bar and comparing against the official stored `1d`
settles which is right:

| request window | sessions checked | folded O/H/L == official daily O/H/L |
|---|---|---|
| `<3mo` (`5d`, `1mo`) | 22 | **0** |
| `>=3mo` (`3mo`, `1y`) | 43 | **39** |

The short window silently drops the opening bars — the `<3mo` series for 2026-08-19 begins at
10:15 and misses the day's open *and* its low, which is why its fold never reconciles.

**And `startDate`/`endDate` cannot be used to escape it, because brapi ignores those parameters for
intraday intervals entirely.** Measured the same day:

| request | `usedRange` echoed | bars | which series |
|---|---|---|---|
| `interval=5m&startDate=`(-90d)`&endDate=`(today) | `1mo` | **0** | — |
| `interval=5m&startDate=`(-30d)`&endDate=`(today) | `1mo` | 1798 | **the bad one** (81/81 bars identical to `range=1mo`, 0/80 to `range=3mo`) |
| `interval=1d&startDate=2022-01-01&endDate=2022-12-31` | `1mo` | 250 | correct, dates honoured |

So intraday is **range-token-only**: dates are silently discarded, the fallback is the defective
one-month series, and asking for a window older than that returns nothing at all. Daily is
unaffected — it honours the dates exactly, and the `usedRange: 1mo` echo is meaningless noise
there.

That makes the constraint structural rather than advisory. For `5m` the backfill picks the
smallest range token that covers `--from..now` with a floor of `3mo` (`ingest.IntradayRange`),
issues **one** request per symbol, and trims the response to the requested window client-side
(`ChunkRequest.KeepFrom`/`KeepTo`). `--chunk` is rejected outright for 5m, and an explicit
`--range` below three months is rejected by `ValidateIntradayRange`. `sync-candles` had the same
trap in its tail refresh — refreshing yesterday through a narrow window would upsert the bad
variant over good backfilled bars — and takes the same route: fetch `range=3mo`, keep only the
tail. Same quota, no corruption.

**4. The same measurement confirms trap #10 rather than refuting it.** Across those 43 sessions the
folded close matched the official close **4** times. O/H/L reconcile; the close does not, because
the closing auction is not in the intraday tape. Stored `1d` remains authoritative, and
`TestFoldedIntradayReproducesTheOfficialDailyRange` asserts both halves — that O/H/L match and that
the close does *not*.

### Decisions this phase forced

**A daily bar's `ts` is the session open, not midnight.** brapi dates daily bars at local midnight
São Paulo (`03:00Z`). Storing that verbatim would mean `1d` and `5m` anchor differently, and any
UTC-side date arithmetic would sit three hours from the day boundary. Instead `Calendar.BucketOpen`
maps every bar — daily included — onto its session open, so PETR4's 2026-08-20 daily bar is stored
at `13:00Z`. One rule for both timeframes, and "the bar for day D" is unambiguous.

**Sessions are ragged, so the resampler folds what is there rather than what should be.** A full
B3 session is 84 five-minute buckets, and 84 divides evenly by 3, 6 and 12 — that part of the plan
holds. What does not hold is the assumption that all 84 are present: PETR4, the most liquid stock
on the exchange, delivers 83 on a typical day, and VALE3 drops to 79. Bars with no trades simply
do not exist. So a bucket aggregates whichever base bars fall inside it, and a bucket with none is
not emitted at all. The synthetic fixture holes the second session deliberately (its opening bar
and two mid-session bars) to keep that behaviour pinned.

**A gap report is only meaningful against history that could exist.** `gaps` first defaulted to a
five-year window whatever the timeframe, which for 5m on Free means 1,204 of 1,248 sessions report
as missing — every one of them simply older than brapi's ~60-day intraday retention. That is 96%
noise hiding the 44 sessions worth looking at. The default is now **each symbol's earliest stored
bar**, so the question the report answers is "is the history I have complete?" rather than "is the
history I have five years long?". `--from` still forces an explicit window when the second question
is the one being asked, and a symbol with nothing stored prints one `EMPTY` line instead of
enumerating every session it lacks.

**Gap detection reports at session granularity, not bar granularity.** Treating every absent 5m
slot as a hole would flag five bars a day on a blue chip and hundreds on an illiquid one — noise
that hides the thing worth seeing. `GapReport` separates *missing* (a trading day with zero bars,
a real hole to refill) from *short* (a session present but under-covered, which is normal), and
`Clean()` only fails on the former. A bar that lands on no session at all is a third category,
`Unexpected`, and it does fail the report.

**Bad bars are rejected individually; only the request failing kills the chunk.** A backfill that
runs for hours against 200 symbols must not die because one ticker returned a bar with `high`
below `low`. `Normalize` drops such bars with a reason, the count lands in `ingest_runs.rejected`,
and a sample is logged. The database keeps the same invariants as `CHECK` constraints, so a bug
that gets past `Normalize` fails loudly at the write rather than storing fiction.

**"Tokenless" is four tickers, not the free tier.** `^BVSP` is seeded `tracked = true` because
Phase 10 benchmarks against IBOV, but brapi answers `/quote/^BVSP` with `401 Token de autenticação
não fornecido` — the free *tier* is not the same thing as the four *tokenless* symbols. So
`Ingester.Reachable` gates every symbol on `HasToken() || IsFreeTicker(...)`, the same guard Phase 1
already applied to name enrichment, and an unreachable symbol is reported as a **skip, not a
failure**: a `--universe` run on Free must exit 0, because a token-only ticker in the universe is
the expected state until Phase 12, not an error anyone can act on.

**Resume is a coverage question, with one escape hatch for holes that will never fill.** The
backfill has no cursor to lose: before fetching a chunk it asks whether that window's trading days
already have bars, and skips if they do. That is safe to kill, restart and re-run. But pure
coverage has a failure mode that the first real gap report exposed — brapi is simply missing
2026-06-05 for all four tickers, on a day B3's published calendar confirms was a normal session.
No number of refetches will produce a bar that the provider does not have, so that chunk would be
re-requested on every run forever: invisible at four tickers, ~200x the cost at full universe. So
a chunk is also skipped when a prior `ingest_runs` row with status `ok` or `empty` covers it —
**but only if the window ends before today**, so the current chunk always re-checks coverage and
keeps picking up new sessions. `--force` overrides both.



> **Status: done, and exercised end to end against the live database.**
>
> The resampler is tested at three levels. Against `testdata/synthetic_15m|30m|1h.json`, computed
> by an implementation written independently of the Go code, with one bucket verified by hand.
> Against the committed *real* PETR4 5m fixture, where the assertions are the invariants rather
> than fixed numbers: total volume, high and low survive the fold unchanged, every bucket is
> aligned to its own session open, every bucket re-folds to exactly the base bars inside it, and
> the per-session bucket counts land on 28/14/7. And against the live store through
> `/alvo candles`, which reconciles the same three quantities on real data.
>
> `Normalize` is checked against the committed raw brapi responses for all four free tickers.
> `go test -race ./...`, `go vet ./...` and `gofmt` are clean.
>
> **The daily backfill has since run for real.** `make backfill ARGS="--universe --timeframe 1d"`
> stored 4,487 bars across the four free tickers over 2021-08-20..2026-08-20 — ~249 bars a year
> per symbol, which is the right shape for a ~246-session year. Resume works: a second run skipped
> the chunks already covered. The token-bucket limiter and backoff earned their keep too — brapi
> answered one request with `429 Limite do sandbox excedido`, the client waited 52s and the chunk
> then stored normally.
>
> **`gaps --universe --timeframe 1d` has run**, and did its job: 1,247/1,250 sessions per ticker,
> with the three shortfalls traced to the calendar defects in the table above rather than to the
> ingest path. After the fix the only remaining hole over 2021-2026 should be 2026-06-05, which is
> brapi's, not ours.
>
> **5m is populated too**: ~3,590 bars per ticker over 44 sessions, ~81.6 bars a session, which is
> the shape real B3 intraday has (79-83 of a possible 84). 43 of the 44 sessions are "short" and
> that is normal, not a defect. The history starts 2026-06-22 and cannot be extended without a Pro
> token.
>
> Carried into Phase 3, deliberately:
> - **Resampled series are not cached.** `CandleService.Load` folds on every call. The plan says to
>   wait for profiling and Phase 3 owns the read path, so that is where an `ETag` on closed ranges
>   and any caching belong.
> - **`Load` bounds the *base* fetch, not the output.** A 5m read capped at 50,000 rows can end in
>   a partially-folded final bucket. Harmless for a whole-session window, wrong for the cursor
>   pagination Phase 3 needs — that endpoint has to page on bucket boundaries, not base rows.
> - **The 2016-2019 holidays are absent from the calendar file**, which does not matter at a
>   5-year window but would if the backfill range is ever widened past 2020.
> - **2026-06-05 stays missing forever.** brapi lacks the bar on a session B3 held. It is the
>   proof case for the `ingest_runs` resume shortcut, and the reason `gaps` can legitimately
>   report a hole that no action will close.
> - **Watch for chunks that can never come back clean.** Resume skips a chunk only when its window
>   has no missing session, so a window containing a session brapi genuinely lacks — or one the
>   holiday file gets wrong — is re-requested on every run, forever. Harmless at four tickers,
>   ~200x more expensive at full universe. If the `gaps` report shows persistent holes, switch the
>   resume check to also honour a prior successful `ingest_runs` row covering the window.
> - **`--prune`-style verification of `adj_close` against a known split is untested.** No split
>   falls inside the committed fixtures' window. Phase 12 spot-checks a real one.
> - ~~**The 2020-2023 holidays still need checking against B3's published calendar**~~ — **done**,
>   by cross-checking the file against ten years of brapi bars rather than by reading the calendar.
>   Five discrepancies, four of them ours; see the table above. That technique is the one to reuse
>   whenever the file is extended: a day all four blue chips are missing is a closure, and a day
>   the file calls closed but that has bars is a typo.

---

## Phase 3 — Candle API + chart MVP

**Goal:** a real candlestick chart in a browser, end to end.

- [x] `GET /api/v1/symbols?q=&kind=&limit=` — search/autocomplete
- [x] `GET /api/v1/candles?symbol=PETR4&timeframe=15m&from=&to=&limit=` — hard cap on `limit`,
      cursor pagination by `ts`. `timeframe` accepts exactly `5m|15m|30m|1h|1d`; anything else is
      a 400 listing the valid set
- [x] Response is columnar (`{ts:[], o:[], h:[], l:[], c:[], v:[]}`) not an array of objects —
      roughly 4× smaller over the wire and drops straight into the chart lib
- [x] `ETag`/`Cache-Control` on closed historical ranges. A closed bar is immutable; say so
      *(with a one-day ceiling rather than a year — see the decisions below)*
- [x] `frontend/`: Svelte 5 + Vite + TS, Lightweight Charts v5 (`addSeries(CandlestickSeries, …)`)
- [x] Symbol search, timeframe switcher (5m/15m/30m/1h/1d), candle ⇄ bar toggle
- [x] Lazy-load older candles on pan-left via `setVisibleRange` subscription
      *(`subscribeVisibleLogicalRangeChange` — the logical range is the one that answers "how many
      bars are left to the left of the viewport", which is the actual trigger condition)*
- [x] Crosshair readout: OHLCV + date
- [x] Timeframes with no data render a clear empty state, not a broken chart
- [x] Go serves the embedded `frontend/dist` as a SPA fallback — unknown non-`/api` paths return
      `index.html`. One container serves both; no nginx, no CORS *(landed in Phase 0)*

**Done when:** you open the app, type PETR4, and pan back through a year of daily candles smoothly.

### Decisions this phase forced

**Paging runs backwards, and it pages on fold boundaries rather than on base rows.** This is the
carry-over Phase 2 flagged: `CandleService.Load` caps the *base* fetch, so a 5m read that hits the
cap can end mid-bucket and hand back a 1h candle folded from four bars instead of twelve. A chart
that pans left is asking for *older* data, so `CandleService.Page` reads `ORDER BY ts DESC` from the
window's top edge and takes `limit × BaseBars() + 1` base rows. Ragged sessions only help here —
missing bars mean fewer base rows per bucket, so N rows always fold into *at least* as many buckets
as a full session would. The `+ 1` is a probe: if the fetch came back full, the fetch was truncated,
the oldest bucket may be partial, and it is dropped. Then the newest `limit` buckets are kept. The
failure mode is one wasted round trip when the window happens to hold exactly the cap — the page is
one bucket short and the client fetches it next. Over-reporting "there is more", never under.

**The cursor is an exclusive upper bound on the *base* timestamp, which is why it composes with
folding at all.** A fold bucket's `ts` is by construction the timestamp of the first base bar inside
it, so `ts < cursor` excludes exactly the buckets already delivered and nothing else. Pages neither
overlap nor gap, no matter which timeframe they were folded to, and the client never has to know
that 15m is derived. When a page ends up empty but truncated, the cursor falls back to the oldest
base row read, so paging always advances instead of spinning on the same window.

**`immutable` is capped at a day, not a year.** The plan says a closed bar is immutable and to say
so, and the response does — but this project ships with known holes that a later backfill is
supposed to close (2026-06-05, and anything the holiday file gets wrong). `max-age=31536000,
immutable` would mean a browser that cached a gap keeps showing the gap for a year with no way to
ask again. `public, max-age=86400, immutable` bounds that to a day while still costing zero
revalidations during a session. Windows that reach into the current session get `no-cache` and lean
on the `ETag` instead. "Closed" is decided on the effective window end — `min(to + 1 day, cursor)` —
not on the newest bar returned, so an empty range is cacheable on the same terms as a full one.

**The chart is fed exchange-local timestamps; the API is not.** Lightweight Charts renders a
`UTCTimestamp` in UTC and has no timezone setting, so B3's 13:00Z session open would draw on the
axis as 13:00. `toChartTime` shifts each bar by its America/São_Paulo offset, computed through
`Intl` and cached per UTC day rather than hardcoded to −03:00, so the 2016–2018 daily bars that
predate Brazil abolishing DST don't drift. The API keeps speaking UTC seconds; the shift is a
rendering concern and lives entirely in the frontend. The crosshair readout formats the *unshifted*
timestamp through `Intl` in the exchange timezone, so it and the axis agree without sharing a path.

**Search ranks with `strpos`, not `ILIKE '%q%'`.** Same matching, but the query is a literal rather
than a pattern, so a user typing `%` into the box gets no matches instead of the whole universe, and
there is no escaping to get wrong. Empty `q` degenerates to a browse listing rather than an error,
ordered `tracked` first, then `active`, then alphabetically — which is what an empty autocomplete
box should show. Exact-prefix matches sort above substring matches, so typing `PET` puts PETR4 above
anything merely containing "pet" in its long name.

**`long_name` is the display name, per Phase 1's warning.** brapi's `shortName` echoes the ticker on
the free tier, so `displayName` prefers `long_name`, falls back to `short_name` only when it differs
from the ticker, and otherwise shows the ticker alone. A symbol seeded from `contracts.json` with no
brapi enrichment renders as its ticker rather than as an empty string.

**The palette lives in CSS and the chart reads it from there.** Lightweight Charts needs concrete
colour strings, not custom properties, so the obvious shape is a TypeScript palette object — which
means every brand colour written twice, once for the DOM and once for the canvas, drifting the first
time one is edited. Instead `app.css` owns the tokens and `theme.ts` pulls the `--chart-*` ones back
out with `getComputedStyle`. Uncached, deliberately: the values are re-read on every
`prefers-color-scheme` change, so the light/dark swap needs no parallel palette in TS at all. The
brand hexes are eyeballed from *Template apresentação ALVO.pdf*; if there are canonical values they
land in one block at the top of `app.css`.

**Down candles are the brand orange.** The deck's whole vocabulary is ink, cream and one orange, and
that orange already reads as a sell tone — using it for down bars makes the chart unmistakably ALVO
instead of generic-terminal, and leaves cream and ink to carry the chrome. Up stays green, because
inverting a convention every Brazilian trader has internalised would cost more than it buys. The
same orange doubles as the UI accent on active controls; different enough in context.

**Dark is the primary theme, light is the cream one.** Slides 1, 3 and 10 are ink with cream type,
which is what a chart wants anyway. `prefers-color-scheme: light` gets the cream world of slides 4
and 6 rather than plain white — lightened slightly from the deck's `#efe7d9` for a full-viewport
background, with the deck value kept for panels.

**Montserrat, self-hosted, and it is a match rather than the real thing.** The deck's geometric sans
with its double-storey `a` and single-storey `g` is closest to Montserrat among faces that can
actually be vendored; if the brand font is something licensed like Gilroy or Cera, swapping it is
the `--font-ui` line. It ships as `@fontsource-variable/montserrat` rather than a Google Fonts
`<link>` because a stylesheet fetched from `fonts.googleapis.com` would be exactly the runtime
dependency Phase 0 built the whole embedded-frontend story to avoid — and the box is in Brazil while
that CDN may not be. Vite bundles all five `unicode-range` subsets into `dist` (~171 KB on the image,
against a 4.8 MB baseline); a `pt-BR` browser downloads only the 38 KB latin one, since every
accented character Portuguese uses lives under U+00FF.

**The time axis is two rows, and only the top one belongs to the chart library.** Lightweight Charts
draws a single row of tick marks and mixes granularities into it — a daily chart ends up alternating
bare day numbers with month names, so "20" gives no clue which month it sits in. The library has no
two-row axis and its labels are canvas-drawn, so a newline in `tickMarkFormatter` buys nothing. The
split: `tickMarkFormatter` makes the top row uniform — day-of-month on `1d`, `HH:mm` intraday, with
the day number substituted at day boundaries because that is where an intraday reader needs it — and
a plain DOM strip underneath carries the period, `Aug 2026` on daily and `20 Aug 2026` intraday,
positioned with `timeToCoordinate` at each period's first visible bar. It stays aligned because the
left price scale is hidden, so the pane's x origin and the strip's are the same. Labels closer than
64 px are dropped, which is what keeps an intraday chart spanning forty sessions from stacking forty
date labels on top of each other. The first visible bar always emits a label whether or not it
starts a period, so the current month is never off-screen. A `ResizeObserver` re-runs the placement
because a width change moves every coordinate without changing the logical range, so neither of the
library's range subscriptions fires for it.

**The interface is English end to end, including its dates and numbers.** Everything else in the
project — plan, code, UI strings — is already English, so `pt-BR` date and number formatting was the
only Portuguese left and it made the axis read half-translated. Dates are now built from one English
month table rather than from `Intl`, which also removes the split where the axis formatted wall-clock
fields by hand while the crosshair readout went through a locale: both now render `20 Aug 2026` from
the same array, and the day-month-year order sidesteps the `mm/dd` ambiguity `en-US` would introduce.
**Numbers are the deliberate exception, and they follow B3.** Prices read `31,25` and volume
`1.234,5 mi`, because a decimal comma is what a quote costs on this exchange, not a translation of
it — the same reason `America/Sao_Paulo` stays put regardless of what language the interface speaks.
That split has one consequence worth naming: Lightweight Charts formats its own price scale and
crosshair labels with a `.` decimal and ignores `localization.locale` for prices entirely, so the
axis would have disagreed with the readout sitting directly above it. Both series therefore carry a
`priceFormat: { type: 'custom' }` pointing at the same `formatPrice`, which puts every number on the
screen — readout, price scale, crosshair — through one function. `minMove` is `0.01`, which is the
stock `tick_size` Phase 1 already stores; when Phase 7 charts something with a different tick, that
value should come from the symbol row rather than the constant.

**The target mark is CSS, not the logo.** The header's ring-and-dot is two `border-radius: 50%`
boxes standing in for the real `⊙` wordmark, so the layout is already the right shape when the
actual asset arrives. Same for the arc behind the empty state — a bordered circle pushed mostly
off-canvas, which is the deck's recurring motif.

**`timeframe` defaults to `1d`, and `from` defaults to 1990.** Daily is the only timeframe with real
depth on Free, so it is the one that makes the app look like it works on first load. The wide `from`
default is free: the query is a backwards index scan on `(symbol_id, timeframe, ts)` with a `LIMIT`,
so the window's lower bound never costs anything and paging is genuinely unbounded.

> **Status: written, not yet run.** The Go code does not compile until `sqlc` regenerates
> `internal/db` — `SearchSymbols` and `ListCandlesDesc` are new queries — and the frontend does not
> build until `package-lock.json` picks up `lightweight-charts`. In order:
>
> ```
> make sqlc
> cd frontend && npm install && cd ..
> make check
> make up
> ```
>
> Tests added are offline and hit no database: the paging/trim arithmetic and the cursor's
> non-overlap property in `internal/market/page_test.go`, and every 400 path, the columnar encoding
> and the `ETag`/304 handshake in `internal/api`. The parts that need a live store — that a 15m page
> boundary reconciles against the 5m bars under it, and that panning left across a page boundary
> leaves no seam — are only checkable against the real database.
>
> Carried into later phases, deliberately:
> - **`adj_close` is not in the candle response.** The chart draws raw prices, so a split inside the
>   window will draw as a gap. Which series a *chart* should show is a different question from which
>   series a *backtest* should run on; Phase 9 has to answer the second one, and the first should
>   follow it rather than lead it.
> - **Resampled series are still not cached.** `Page` folds on every call, as `Load` does. The `ETag`
>   moves the cost off repeat requests, which was the cheap half; a server-side cache still waits for
>   profiling.
> - **`/api/v1/symbols` and `/api/v1/candles` are public**, which is the plan's own call — market
>   data is not user data. ~~Phase 4 puts only strategies, runs and watchlists behind `RequireAuth`,
>   plus `/admin/brapi-usage`, which is still open from Phase 1.~~ **Done in Phase 4** — both stayed
>   public, and `/admin/brapi-usage` went behind the middleware.
> - **The frontend has no router.** One chart, one symbol, no deep links — the SPA fallback is
>   already in place for when Phase 7 or 8 needs real routes.
> - **The logo assets are not in yet.** The header mark and the empty-state arc are CSS circles
>   standing in for the real `ALVO ⊙` wordmark; dropping the SVG in replaces the `.brand` block and
>   nothing else. A favicon is also still missing — `index.html` sets `theme-color` but no icon.
> - **Nothing debounces the timeframe switcher.** Clicking through 5m→15m→30m fires three requests
>   and aborts the first two; correct, but it spends round trips a debounce would save.

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
    token_hash TEXT PRIMARY KEY,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

- [x] `POST /api/v1/auth/register`, `/login`, `/refresh`, `/revoke`
- [x] ~~bcrypt~~ **argon2id** for passwords, HS256 JWT for access (15 min), 60-day random refresh
      token *(see the decisions below)*
- [x] `RequireAuth` middleware puts `user_id` in the request context
- [x] Market data endpoints stay **public** — candles aren't user data and caching them per-user
      is waste. Only strategies, runs and watchlists sit behind auth
- [x] `JWT_SECRET` and `BRAPI_TOKEN` arrive as env vars, never baked into the image. A secret in a
      layer is in the registry forever

**Done when:** register → login → call a protected endpoint → refresh → revoke all behave, with
tests covering expired and tampered tokens.

### Decisions this phase forced

**argon2id instead of bcrypt.** The plan said bcrypt because Chirpy said bcrypt. argon2id is the
current password-hashing recommendation, `alexedwards/argon2id` has sane defaults and encodes its
parameters into the hash string, and it sidesteps bcrypt's 72-byte silent truncation — a password
longer than 72 bytes has its tail ignored, which is a real correctness bug rather than a stylistic
one. The cost is memory: `DefaultParams` is 64 MB per verification, which matters on a 4 GB box
under concurrent logins and is the number to turn down first if Phase 13's rate limiting isn't
enough. Passwords are capped at 256 bytes so an unbounded body can't turn one request into a
multi-second hash.

**`pgtype` stays out of the service layer, which cost a `sqlc.yaml` override.** Phase 1 added
`timestamptz`, `date` and `numeric` overrides so no `pgtype` reached the handlers. `uuid` had no
override because no table used one until now, so `users.id` generated as `pgtype.UUID` while
`auth.ValidateJWT` returns a `uuid.UUID` — every handler would have converted between them by hand,
and a forgotten `Valid: true` is a silent NULL rather than a compile error. Adding
`db_type: uuid → github.com/google/uuid.UUID` keeps the property Phase 1 established, and it has to
land before Phases 8 and 9 add four more `user_id` columns.

**The refresh token travels in the `Authorization` header, not the body.** That's Chirpy's shape and
the plan asked for Chirpy's shape. It means `/refresh` and `/revoke` take a bearer credential that is
*not* a JWT, which reads oddly until you notice it makes both endpoints uniform with every other
authenticated call and keeps the token out of request bodies and logs.

**`/refresh` does not rotate.** It mints a new access token and leaves the refresh token alone.
Rotation — issuing a fresh refresh token on every use and revoking the old one — detects replay of a
stolen token, but it also breaks any client that fires two refreshes concurrently, and it needs a
reuse-detection story to be worth anything. Deferred deliberately; the schema already supports it,
since `revoked_at` is exactly the column rotation would write.

**Only `GetUserByEmail` can return a password hash.** Every other user-shaped query names its
`RETURNING` columns, so `hashed_password` cannot reach a handler that has no business with it. The
API DTO boundary exists anyway — login needs the hash and must not serialize it — but narrowing the
queries means a future handler that forgets the DTO leaks nothing.

**Email is lowercased on the way in.** Postgres `UNIQUE` is case-sensitive, so without normalization
`Trader@example.com` and `trader@example.com` are two accounts, and whichever one you registered
with is the only one that logs in. Lowercasing the local part is technically not RFC-correct — local
parts may be case-sensitive — but no mail provider in practice treats them so, and two accounts for
one human is the worse failure. `mail.ParseAddress` also rejects the `Name <addr>` form, which would
otherwise store a display name as an email.

**JWT claims are validated during parsing, not after it.** `ValidateJWT` passes `WithValidMethods`,
`WithIssuer` and `WithExpirationRequired` to the parser rather than reading claims back and checking
them itself. `WithValidMethods` is the one that matters: without it, the library will attempt
whatever algorithm the token's own header names, which is the algorithm-confusion class of bug.
`WithExpirationRequired` closes the adjacent hole where a token with no `exp` at all validates
forever.

**Revoke reports whether it revoked anything.** `RevokeRefreshToken` is `:execrows` with
`AND revoked_at IS NULL`, so the handler can answer 401 for an unknown or already-revoked token
instead of 204 for everything, and a replayed revoke can't push `revoked_at` forward to a later
timestamp than the real one.

**The database stores a SHA-256 of the refresh token, and the column says so.** The plan originally
copied Chirpy's raw-token storage, which makes a `pg_dump` — the artifact Phase 13 mails to object
storage nightly — a list of every live session in plaintext. Hashing on the way in means a stolen
dump grants nothing: the client holds the only copy of the real token, and a lookup hashes the
presented value and matches on the digest. The column is `token_hash` rather than `token` precisely
so the next person to touch it cannot compare it against a raw token without noticing.

**Plain SHA-256, not argon2id, and not HMAC.** A password needs a slow KDF because it has perhaps 30
bits of entropy and the attacker's job is guessing it. A refresh token is 32 bytes from
`crypto/rand` — 256 bits — so there is nothing to guess and a slow hash would only add latency to
every authenticated refresh. HMAC keyed on `JWT_SECRET` was the other candidate; it adds a pepper
that a dump alone can't recover, but against a value with full entropy that buys nothing, and it
would couple every live session's validity to a secret that ought to be rotatable on its own.
Lookup is by primary key, so Postgres does the comparison inside a B-tree descent — not constant
time, but timing a 256-bit-entropy index probe is not an attack anyone can mount.

**gosec now skips generated files, because naming a query after a token trips G101.** sqlc names its
query constants after the query, so `const createRefreshToken = "INSERT INTO refresh_tokens ..."` is
a string constant whose *name* matches G101's credential pattern. All three refresh-token queries
fire as HIGH severity, LOW confidence, and there is nowhere to put a `#nosec` comment that `make
sqlc` won't overwrite. The alternatives were renaming the queries to dodge a regex — giving up the
domain's own vocabulary — or dropping G101 everywhere, which would also stop it catching a real
hardcoded secret in hand-written code. `gosec -exclude-generated` keys off the standard
`// Code generated … DO NOT EDIT.` header instead, so it suppresses exactly the nine files in
`internal/db` and scans everything else unchanged. It is set in both the `sec` target and CI, which
have to agree or the PR fails on something `make check` passes.

> **Status: done and verified end to end against the running stack.** The full sequence the
> done-when asks for was exercised over HTTP, and each step behaved:
>
> | step | result |
> |---|---|
> | register | 201, and `Phase4.Check@Example.com` came back stored as `phase4.check@example.com` |
> | register again, lowercased | **409** — normalization means one address is one account |
> | login, wrong password | 401 `incorrect email or password` |
> | login | 200 with user, access token, refresh token, `expires_in: 900` |
> | protected endpoint, no header | 401 with `WWW-Authenticate: Bearer` |
> | protected endpoint, access token | 200 |
> | protected endpoint, *refresh* token | **401** — the two token types are not interchangeable |
> | refresh | 200, new access token, which then opens the protected endpoint |
> | revoke | 204 |
> | revoke again | **401** — `AND revoked_at IS NULL` makes the second one a no-op, not a silent success |
> | refresh after revoke | **401** |
>
> **The hashing was checked against the database, not just asserted in a test.** The row's
> `token_hash` equals `sha256(issued token)` computed independently at the shell, and the issued
> token itself appears nowhere in the table. `users.hashed_password` reads
> `$argon2id$v=19$m=65536,t=1,p=1$…` — the parameters are in the hash, which is what makes them
> changeable later without a migration. Deleting the user cascaded the refresh token away, so the
> FK does what `ON DELETE CASCADE` claims.
>
> Offline tests cover what HTTP can't reach cheaply: `internal/auth` covers the JWT round trip,
> expiry, tampering (swapped payload, swapped signature, truncation, garbage), a foreign secret, a
> foreign issuer, the `alg: none` forgery, a token with no `exp`, a non-UUID subject, the
> bearer-header parser, and the digest's stability and width; `internal/api` covers every 400 path on
> register and login, the 401 paths on refresh and revoke, and that `RequireAuth` rejects missing,
> malformed, expired and foreign-signed tokens while putting the user id in the context for a good
> one.
>
> Carried into later phases, deliberately:
> - **`/api/v1/admin/brapi-usage` is the only endpoint behind `RequireAuth`**, because it is the only
>   non-public endpoint that exists. It closes Phase 1's carry-over. Strategies, runs and watchlists
>   attach to the same middleware as Phases 7-9 add them.
> - **There is no frontend auth UI.** Phase 4 is backend-only by the plan's own checklist; the app
>   still loads straight into the chart, which is public. Whoever adds a login screen also has to
>   decide where the access token lives in the browser, and `localStorage` is the wrong answer.
> - **Login has a timing side channel.** An unknown email returns without hashing anything; a known
>   one spends ~50 ms in argon2id. That difference enumerates registered addresses. The messages are
>   already identical, so the fix is either a dummy verify against a fixed hash or Phase 13's
>   per-IP rate limiting, which is the mitigation that actually generalizes.
> - **Nothing deletes expired or revoked refresh tokens.** The table grows without bound at 60 days
>   per row. Irrelevant at this size, a cleanup job whenever it isn't.

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

- [x] **Streaming, not batch.** `Update` per bar in O(1). A batch API that recomputes from scratch
      per bar makes the backtest loop O(n²) and rules out live updates later. `Compute(series)` is
      a thin fold over `Update`, not a separate implementation *(three folds, actually: `Feed`
      updates without emitting, `Emit` collects from the first ready bar, `Compute` is
      `Reset` + `Emit`. The API needs the first two separately — see priming below)*
- [x] Multi-output indicators return positional `Values()` aligned with a static `Outputs() []string`
      on the spec. String lookup per bar in the hot loop is avoidable, so avoid it
- [x] `Warmup()` is how many bars before `Ready()`. The backtester skips those bars entirely — an
      EMA emitting values during its seeding window is a silent source of fake early trades
- [x] Registry: `indicator.Register(name, Spec)` where `Spec` declares params (name, type, default,
      min/max) and builds an instance. The `?indicators=` query param, the strategy compiler, and
      the UI's parameter form all read from this one place
- [x] `source` selector: `close | open | high | low | hl2 | hlc3 | ohlc4`
- [x] Ring buffers for rolling windows; no reallocation per bar
- [x] First five, to prove the shape: **SMA, EMA, RSI, MACD, Bollinger Bands**
- [x] Golden-value tests: a fixed 200-bar fixture with expected outputs to 6 decimals, cross-checked
      against a reference implementation. Every later indicator gets the same treatment

**Done when:** `GET /api/v1/candles?...&indicators=ema:9,ema:21,rsi:14` returns aligned series, and
the golden tests pass.

> Warmup and NaN handling are where hand-written indicator libraries quietly go wrong. Decide once,
> here: values before `Ready()` are **not emitted** — not zero, not NaN. The API returns a `start`
> offset per indicator series so the chart aligns without padding.

### Decisions this phase forced

**`Warmup()` is the index of the first value, not a count of bars to skip.** "How many bars before
`Ready()`" reads both ways — SMA(20) is ready *after* 20 bars, so the number is either 19 or 20 and
an off-by-one here is a whole bar of fake signal. It is 19: `Warmup()` returns the number of leading
bars that carry no value, which makes it exactly the `start` offset the response reports and exactly
the index a backtest should begin at. `TestEveryRegisteredIndicatorEmitsExactlyAtItsWarmup` asserts
`Compute(...).Start == Warmup()` for every registered indicator, and
`TestNothingIsEmittedBeforeReady` walks bar by bar asserting `Ready() == (i >= Warmup())`, so the
convention cannot drift as Phase 6 adds thirty more.

**A page is primed, not restarted — otherwise every pan-left draws a new hole.** Computing
indicators over the returned page alone is the obvious implementation and it is wrong twice: the
first 20 bars of *every* page would have no SMA(20), and the EMA on page two would disagree with the
EMA on page one at the same timestamp, drawing a visible seam exactly where Phase 3 worked to remove
one. So `CandleService.Prime` reads the buckets immediately *before* the page and feeds them through
`Update` without emitting. `from` bounds what is *returned*, not what is *read*, so the prime query
deliberately ignores it and reaches back past the window's lower edge. It reuses `ListCandlesDesc`
with a zero lower bound and the same fold-boundary trim as `Page` — which is why **Phase 5 adds no
SQL and needs no `make sqlc`**.

**How deep to prime is a question only the indicator can answer.** An SMA(200) needs exactly 199
bars — its window is finite, so bar 200 back has no influence at all. An EMA(9) needs far more than
its 8-bar warmup, because the seed's error decays rather than falls out. A single uniform multiple
serves neither: at 8× the warmup, SMA(200) would read 1,600 bars for the 199 it needs, and RSI(14)
would still be off by 4.6e-3 — Wilder's smoothing uses α = 1/period against an EMA's 2/(period+1),
so it remembers roughly twice as long. Hence the optional `Primer` interface: the default depth is
`Warmup()`, and an indicator that carries state past its warmup overrides it. EMA and MACD ask for
8× their periods, RSI for 16×. Measured against 2,000 synthetic bars, primed page versus full
history:

| indicator | prime bars | worst disagreement |
|---|---|---|
| `sma:20` | 19 | 5e-14 |
| `sma:200` | 199 | 6e-14 |
| `ema:9` | 72 | 1.3e-8 |
| `ema:21` | 168 | 2.4e-8 |
| `rsi:14` | 224 | 8.3e-7 |
| `macd:12:26:9` | 280 | 3.3e-10 |
| `bb:20:2` | 19 | 6.3e-12 |

All of it is orders of magnitude under a centavo, which is the only precision a chart or a fill
price can express. Phase 6's ATR and ADX are Wilder's too and should say so with `PrimeBars()`;
DEMA/TEMA nest EMAs and need the same treatment.

**`start` survives priming, because history runs out.** Priming fixes the page boundary; it cannot
invent bars before a symbol's first one. A chart panned to the very beginning of PETR4's history
still gets `start: 19` on an SMA(20), so the response reports the offset per series and the client
draws from there rather than padding with zeros or nulls.

**`Ready()` is all-or-nothing per indicator, not per output.** MACD's line exists 8 bars before its
signal line does. Emitting the line early with a null signal would put nulls in a `[]float64` and
make every consumer handle them; instead MACD reports `Ready()` only once every output is real, and
its warmup is `(slow-1) + (signal-1)` = 33 on the defaults. Same rule for Bollinger's three bands.
That is the plan's own "not zero, not NaN" applied one level up.

**Every spelling of an indicator collapses to one canonical key.** `ema:9`, `ema:period=9`,
`EMA : 9 ` and `ema:9:source=close` all key as `ema:9`, so asking for the same series twice in one
request dedups instead of computing it twice, and the `ETag` doesn't fork over whitespace. Params
are positional in spec order or named, which keeps `?indicators=ema:9,ema:21,rsi:14` short while
letting `bb:20:mult=2.5` be readable. `source` is a reserved name handled by the framework, so no
indicator can define a parameter that shadows it.

**The registry answers "what can I ask for" and every error message proves it.** An unknown name
lists the catalogue, an out-of-range param prints its bounds, an unknown parameter names the ones
that exist. Phase 6 hangs `GET /api/v1/indicators` on `indicator.Catalog()`, which already carries
the title, group, overlay flag, parameter schema and output names the picker needs — the endpoint
is the only part of that not written yet, and it is Phase 6's by the plan's sequencing.

**EMA seeds with an SMA of its first `period` values.** Seeding with the first price instead is
simpler and is what most naive implementations do, but it makes the first hundred bars depend
entirely on one arbitrary tick, and it disagrees with TA-Lib and with TradingView — which is what
anyone checking these numbers against a chart they trust will compare to. The seed is also what
makes priming converge: it starts the recursion near the truth instead of far from it.

**Wilder's smoothing is already its own helper, one phase early.** Phase 6 asks for it because RSI,
ATR and ADX otherwise get it subtly wrong three times independently. RSI needs it now, so it lands
now, in `smooth.go`, written as `value += (x - value)/period` rather than
`(value*(period-1) + x)/period` — algebraically identical, one fewer multiplication of a large
number by a large number.

**Rolling variance is the stable sliding update, not a sum of squares.** Bollinger needs a standard
deviation per bar and the O(1) way to get one is to carry Σx². For prices around 30 with a spread
around 1, Σx² and (Σx)²/n are within 0.1% of each other and subtracting them throws away most of the
significant digits. The sliding Welford update carries Σ(x-μ)² directly and updates it in constant
time without ever forming that difference, and `TestWindowStddevMatchesTheNaiveComputation` pins it
against a recomputed-from-scratch reference to 1e-12. The mean comes from the ring's running sum,
so Bollinger's middle band and a bare SMA of the same period are the same number.

**The golden fixture is real PETR4 bars and the reference is Python.** 200 daily bars lifted from
the committed brapi response, re-stamped onto the session-open convention the store uses, with
expected values from `testdata/indicators_reference.py` — a second implementation written from the
textbook definitions, in another language, so a formula misremembered in Go has to be misremembered
identically in Python to survive. It is committed next to what it generates because Phase 6 adds
thirty more indicators to the same file; regenerating is `python3 testdata/indicators_reference.py`
from the repo root. `TestGoldenValuesSurviveAHandCheck` computes an SMA(5) and a BB(5,2) from the
first five bars by hand in the test, which is what the reference itself is checked against.

**The priming property is tested on 2,000 synthetic bars, not on the 200 real ones.** The fixture is
too short to prove anything about a 280-bar prime window — the window reaches the start of history
and the comparison becomes trivially exact. The synthetic walk comes from an inline LCG rather than
`math/rand`, which keeps it deterministic across Go versions and keeps gosec's G404 out of it.

**`indicator.Candle` carries `float64` volume and no `adj_close`, and the package imports nothing.**
`market.Candle` stores volume as `int64` and an optional adjusted close; the plan's indicator struct
is plain float64s. Keeping them separate is what lets `internal/indicator` stay a pure computation
package with no database, no calendar and no market types in it — testable entirely from a JSON
fixture. The conversion is one loop at the API boundary, and Phase 9 will need the same loop.

> **Status: done and verified end to end.** `make check` is clean — `gofmt`, `go vet ./...`,
> `staticcheck ./...`, `gosec -exclude-generated ./...`, `go test ./...` and the frontend
> type-check — and the phase's done-when was exercised over HTTP against the running stack:
>
> ```
> curl -s 'localhost:8080/api/v1/candles?symbol=PETR4&timeframe=1d&limit=60&indicators=ema:9,ema:21,rsi:14' | jq '.count, [.indicators[] | {key, start: .series[0].start, n: (.series[0].values | length)}]'
> ```
>
> 60 candles, three indicator bodies, every `start` 0 and every series 60 values long — which is the
> answer priming is supposed to give: 60 bars of daily PETR4 sit well inside the 168 bars `ema:21`
> reads before the window, so the page carries no warmup hole at all. Without priming the same
> request would have returned `ema:21` starting at bar 20.
>
> The offline suite covers the rest: the golden values for nine cases (`sma:5`, `sma:20`, `ema:9`,
> `ema:21`, `ema:9:source=hl2`, `rsi:14`, `rsi:2`, `macd:12:26:9`, `bb:20:2`) to 6 decimals against
> the Python reference; the warmup convention and the `Ready()` walk across every registered
> indicator; that `Reset` makes an indicator reusable and that streaming equals `Compute`; the
> priming table above plus a control showing an unprimed page is visibly wrong; the ring, the
> sliding variance and Wilder's seed; every parse and canonicalisation path; and at the API layer,
> every 400, the alignment of series against candles, the empty-series encoding, and that the body
> omits `indicators` entirely when none were asked for.
>
> **The phase added no SQL.** `Prime` reuses `ListCandlesDesc` with a zero lower bound, so there was
> no `make sqlc` and no migration between Phase 4 and here — the only phase so far that touches the
> read path without touching the schema.
>
> Carried into later phases, deliberately:
> - **`GET /api/v1/indicators` does not exist yet.** `indicator.Catalog()` is what it will serve and
>   error messages list the names in the meantime; the endpoint is Phase 6's done-when.
> - **Nothing draws them.** Phase 7 owns the picker, the panes and the per-indicator colours; the
>   response already carries `overlay` so the client never has to hardcode which pane an indicator
>   belongs in.
> - **Indicators run on raw prices, not `adj_close`.** They see exactly what the chart draws, so a
>   split inside the window bends every moving average the same way it bends the candles. Phase 9
>   decides which series a *backtest* runs on, and the chart should follow that answer rather than
>   lead it — Phase 3's carry-over, unchanged.
> - **The one-day `immutable` cache now covers indicator values too**, and those depend on bars
>   *before* the page. A backfill that fills a hole inside the prime window changes the values for a
>   URL a browser may already hold. Bounded to a day, same as the candles themselves, and the same
>   reason Phase 3 capped it there rather than at a year.
> - **Computed series are not cached server-side.** Priming adds a second query per request that asks
>   for indicators, and the fold plus the indicator walk run every time. Still waiting on profiling,
>   as with the resampler.
> - **An `Instance` holds a live indicator**, so it is single-use per request. Phase 8's strategy
>   compiler must build its own instances rather than reusing parsed ones across runs.
> - **Every indicator registered so far takes a `source`.** Phase 6 adds ones that read several price
>   fields at once — ATR, OBV, MFI, the whole volume group — and those must register with
>   `Sourced: false` so the framework rejects `atr:14:source=hl2` instead of silently ignoring it.
> - **Eight indicators per request, periods capped at 2,000.** Both are arbitrary but bounded; the
>   period cap is what stops `ema:100000` from allocating a ring nobody asked for.

---

## Phase 6 — Indicator library

**Goal:** breadth. Each is one file, one test file, registered.

- [ ] **Overlays:** WMA, HMA, DEMA, TEMA, VWAP, Keltner, Donchian, Parabolic SAR, SuperTrend, Ichimoku
- [ ] **Momentum:** Stochastic, Stochastic RSI, CCI, Williams %R, ROC, Momentum, ADX/+DI/−DI, Aroon
- [ ] **Volatility:** ATR, StdDev, Historical Volatility, Chaikin Volatility
- [ ] **Volume:** OBV, MFI, A/D Line, Chaikin Oscillator, Volume MA, VWMA
- [ ] **Structure:** Pivot Points (classic/Fibonacci), Williams Fractals, ZigZag
- [x] Wilder's smoothing is its own helper — RSI, ATR and ADX all use it and all get it subtly
      wrong independently otherwise *(landed in Phase 5, in `internal/indicator/smooth.go`, because
      RSI needed it there. ATR and ADX pick it up unchanged — and both should declare
      `PrimeBars() = period * wilderPrimeFactor` the way RSI does, or a paged chart will hand back
      values that disagree with a full-history run)*

**Done when:** every registered indicator has golden tests, and `GET /api/v1/indicators` lists the
catalogue with parameter schemas.

> The registry already carries everything that endpoint has to serve — name, title, group, overlay
> flag, parameter schema with ranges and defaults, output names — so `GET /api/v1/indicators` is a
> handler over `indicator.Catalog()` rather than a new data model. Extending the golden fixtures is
> `python3 testdata/indicators_reference.py` after adding the reference formula to it; the fixture
> file is keyed by canonical indicator key, so new cases append without disturbing the existing ones.
> Two registration flags matter for this batch: the volume and true-range indicators (ATR, OBV, MFI,
> A/D, VWAP, Keltner, SuperTrend, the Donchian/Ichimoku structure group) read several price fields at
> once and must register `Sourced: false`, and anything drawn against price rather than in its own
> pane needs `Overlay: true` — Phase 7 reads that flag instead of hardcoding a list.

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
      The budget above assumed one month per request; measure it, don't assume it. **Floor: three
      months.** Phase 2 measured that a shorter window returns a different and wrong 5m series, so
      a one-month chunk is off the table regardless of what the quota arithmetic prefers. Confirm
      the defect's shape on Pro before the full run: fetch one symbol at two window sizes and
      reconcile both folds against the stored `1d`
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
    *Measured in Phase 2: over 43 sessions the folded O/H/L matched the official daily bar 39
    times, the folded close matched 4 times.*
11. **brapi's 5m series depends on the requested range, and `startDate`/`endDate` don't work for
    it at all.** Intraday requests silently discard the dates and fall back to `range=1mo`, which
    returns a different — and demonstrably wrong — series that is missing the opening bars of every
    session. Only an explicit `range` token of `3mo` or wider returns the series that reconciles
    against the official daily bar. So 5m is always one range request per symbol, trimmed
    client-side; never a date window, and never a narrow tail refresh, which would upsert bad bars
    over good ones. Daily is unaffected and honours dates normally. Evidence in Phase 2's notes.
12. **Sessions are not uniformly populated.** A full B3 day is 84 five-minute buckets but a real
    one delivers 79-83; bars with no trades don't exist. Anything that assumes a fixed bar count
    per session — resampling, gap detection, warmup arithmetic — has to fold what is present
    rather than what should be.
13. **Development ran on four large caps.** PETR4, VALE3, ITUB4 and MGLU3 are liquid, gap rarely,
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

- ~~**Free-tier history depth.**~~ **Answered in Phase 2: 10 years of daily** (PETR4, `range=10y`,
  2,482 bars back to 2016-08-22), and **~60 days of intraday**, which does not extend no matter
  what range is asked for.
- **5m history depth on Pro, and per-request chunk size.** Pro advertises 15+ years, but
  intraday retention is usually shorter than daily on this class of provider, and how much 5m
  comes back per request is unmeasured. Both feed directly into Phase 12's budget. Measure on
  day one of the Pro month, before launching the full backfill. **Phase 2 added a hard
  constraint on the answer:** whatever chunk size is chosen must be at least three months, or the
  wrong intraday series comes back. Free retention caps at ~60 days, so whether the `<3mo` defect
  persists on Pro at real depth cannot be tested until the token is upgraded — check it first, by
  fetching one symbol at two window sizes and reconciling both against the stored `1d`.
- **Futures coverage.** brapi lists futures as Pro-only; whether WIN/WDO come with usable
  intraday history is unverified. Phase 11's rollover work depends on the answer, and it can't
  be checked until the Pro month.
- **Backfilling admissions made after the Pro month.** A ticker promoted into an index later can
  only be given 5m history while a Pro token is live. On Free it gets daily from admission
  onward and nothing intraday, ever. If the plan is to drop to Free, either accept a universe
  where newer members have shallower intraday history, or batch admissions and buy a second Pro
  month once it's worth it.
