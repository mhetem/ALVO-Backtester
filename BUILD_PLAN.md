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
> - ~~**There is no frontend auth UI.**~~ **Closed in Phase 7**, which is the first phase with
>   anything to save. The answer to where the access token lives: in memory only, with the refresh
>   token in an httpOnly `SameSite=Strict` cookie the browser holds and JavaScript cannot read. The
>   app still loads straight into the public chart when signed out.
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
> - ~~**Nothing draws them.**~~ **Closed in Phase 7** — the picker, the panes and the per-indicator
>   colours all landed there, and the client reads `overlay` off the response rather than hardcoding
>   which pane an indicator belongs in.
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

- [x] **Overlays:** WMA, HMA, DEMA, TEMA, VWAP, Keltner, Donchian, Parabolic SAR, SuperTrend, Ichimoku
- [x] **Momentum:** Stochastic, Stochastic RSI, CCI, Williams %R, ROC, Momentum, ADX/+DI/−DI, Aroon
- [x] **Volatility:** ATR, StdDev, Historical Volatility, Chaikin Volatility
- [x] **Volume:** OBV, MFI, A/D Line, Chaikin Oscillator, Volume MA, VWMA
- [x] **Structure:** Pivot Points (classic/Fibonacci), Williams Fractals, ZigZag
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

### Decisions this phase forced

**Thirty-two registrations, not thirty-one — `method` is not a parameter kind.** The plan lists
"Pivot Points (classic/Fibonacci)" as one line, but `Param` only carries `int` and `float`, so a
`method` selector would have to be an integer enum in a URL — `pivots:1:1` meaning "Fibonacci" is
unreadable and unvalidatable. They register as two names, `pivots` and `fibpivots`, which is what
the picker wants anyway: two entries with the same parameter schema and the same seven outputs
rather than one entry with a magic number.

**Two files per indicator would be two files of nothing.** The plan asks for "one file, one test
file" and gets the first half: each indicator is its own file, thirty-one of them for thirty-two
registrations. The second half would be thirty-two test files that all assert what
`indicator_golden.json` already asserts, which
is the shape Phase 5 already rejected — SMA and EMA have no `sma_test.go` either. Correctness per
indicator lives in the golden fixture; the test files are grouped by the plan's own five groups and
hold only what a fixture cannot state — that a bounded oscillator stays inside its bounds, that
Keltner's midline *is* an EMA(20), that Aroon pins to 100/0 on a monotone run, that the pivot ladder
comes out in order. `TestEveryGoldenCaseNamesADistinctIndicator` fails the build if a new
registration arrives without a golden case, which is the guarantee the "one test file" rule was
reaching for.

**Session VWAP does not exist yet, and the honest name for what does is "rolling".** Every trader
means *session-anchored* VWAP, and `internal/indicator` cannot compute one: the package deliberately
imports nothing — no calendar, no exchange timezone — so it cannot tell where a session starts. A
cumulative-from-the-first-bar VWAP would have been worse than useless under paging, because the
anchor would move every time the chart panned. So `vwap:20` is a rolling window over the typical
price, titled "Rolling VWAP" so nobody reads it as the other thing, and the anchored version waits
for a phase that can hand the indicator a session boundary. It is a real distinction from `vwma`,
which weights a *chosen source* by volume; `vwap` always reads `hlc3` and registers `Sourced: false`.

**OBV and the A/D Line are cumulative, so the framework had to grow an `Anchor()`.** Their level
depends on where the accumulation started, which under paging is "wherever the deepest indicator in
the same request happened to prime to" — so `?indicators=obv` and `?indicators=obv,ema:200` would
return different OBV levels for exactly the same bars, at a URL cached `immutable` for a day. The
optional `Anchorer` interface fixes the zero at the page boundary: `computeIndicators` calls
`Anchor()` between the prime feed and the emit, the accumulator resets to zero while the previous
close is kept, and the emitted series depends only on the page. The consequence, stated plainly:
OBV and A/D levels are *not* comparable across pages — slope and divergence are what they mean, and
Phase 7 should scale their pane per view rather than pinning a zero line. The Chaikin Oscillator
deliberately does *not* anchor: it is a difference of two EMAs of the same accumulator, so a
constant offset cancels, and anchoring mid-stream would leave its EMAs holding a level its input no
longer has. `TestTheCumulativeSeriesAnchorAtThePageAndTheOthersDoNot` pins exactly which two
indicators implement the interface.

**Ichimoku emits the cloud already shifted, and drops Chikou.** Senkou A and B are drawn 26 bars
*ahead* of the bar that computes them, which a streaming indicator emitting one row per bar cannot
do at the right-hand edge — the last 26 bars of cloud extend past the last candle and there is no
bar to hang them on. What it can do is emit, at bar `i`, the value computed at bar `i-26`, so the
client draws all four series against the candle they belong to with no client-side shifting. Chikou
is the close shifted 26 bars *back*, which needs lookahead to emit and carries no information the
client does not already have in `c[]`; it is not an output. The cost is the leading edge of the
cloud, which Phase 7 can extrapolate from `senkou_a`/`senkou_b` if it wants it.

**Williams Fractals and ZigZag are step lines, because "no value here" is not expressible.** Phase 5
decided values before `Ready()` are not emitted — not zero, not NaN — and the same rule applied
inside a series means a fractal indicator cannot emit "nothing" on the 96% of bars that are not
fractals. Both therefore emit a level that is always defined and steps when the structure does:
`fractals` carries the last confirmed pivot high and low, seeded at the first full window with that
window's extremes; `zigzag` carries the running extreme of the current leg plus a `direction` of
+1/-1/0. `TestFractalsStepOnlyOntoAConfirmedPivot` checks every step lands on a bar whose
neighbours are strictly lower, which is the property the step line is standing in for.

**ZigZag whipsaws when a single bar's own range exceeds the deviation, and that is the indicator
being right.** The leg extreme absorbs the current bar's high before the retracement is measured
against the same bar's low, so a bar spanning 9.5% of its price reverses a 5% ZigZag every bar. The
first test fixture walked into exactly that — a ramp starting at price 10 with a fixed 1.0 bar range
— and reported 20 reversals over one peak. The fixture was wrong, not the indicator: `tentRamp` now
takes a base price so the bar range is a fraction of it. The property is worth knowing before Phase
7 puts a deviation control in front of anyone, because BR equities run 2–4% daily ranges and a
ZigZag set much below that will draw noise rather than structure.

**Pivot points are computed over the previous `period` bars, not the previous session.** Same
constraint as VWAP: the package has no calendar. `pivots:1` on a daily chart is therefore the
classic daily pivot exactly, `pivots:5` is a usable weekly one, and on intraday bars it is pivots
over the previous `period` bars rather than over yesterday's session — which the parameter name
makes visible instead of pretending otherwise.

**Rolling max and min needed their own structure, and it is not a ring.** Donchian, Stochastic,
Williams %R, Aroon and Ichimoku all want "the highest high of the last n bars" per bar, and a ring
only carries a running sum. `extreme` is a monotonic deque over a fixed ring of `size` slots: each
push evicts the out-of-window head first, then pops every tail the new value dominates, so it is
O(1) amortised with no allocation per bar. It also carries the *age* of the current extreme, which
is the whole of Aroon — and the tie rule matters there, so the deque pops on `<=` and the most
recent occurrence of a repeated high wins, matching TA-Lib.
`TestTheRollingExtremeMatchesAScanOfTheWindow` pins value and age against a scan of the same window
over 500 bars at four window sizes, including the monotone run that makes a naive deque overflow.

**CCI is the one O(n) indicator, because mean absolute deviation has no sliding form.** Bollinger's
standard deviation slides in constant time because Σ(x-μ)² telescopes; Σ|x-μ| does not — evicting a
value can flip the sign of every remaining term. So CCI walks its window each bar, which is what
TA-Lib does too, and `ring.at(i)` exists for it. At period 20 that is 20 subtractions per bar
against a database round-trip per request; it is not the thing to optimise.

**WMA slides in constant time and drifts, which is fine and worth saying.** The O(1) update is
`numerator += period*x - sum_before`, which reuses the ring's running total and never re-walks the
window — but it is an accumulator, so error compounds over a long stream instead of being bounded by
the window. `TestTheSlidingWeightedAverageMatchesTheNaiveComputation` recomputes the same window
from scratch over 1,000 bars and requires agreement to 1e-9; the golden tolerance is 1e-6 and a
centavo is 1e-2. HMA nests three WMAs behind it.

**The same sliding sum let a bounded oscillator leave its bounds, so the bounded ones clamp at
emit.** `ring.mean()` divides a running total that is maintained by add-and-evict, so it carries
drift the window does not: three raw %K values of *exactly* 100 summed to 300.0000000000001, and
Stochastic RSI emitted 100.00000000000004. The raw percentage itself is exact — when the close is
the window high, `(close-low)/(high-low)` is the same subtraction over itself and divides to exactly
1 — so the escape is entirely the smoothing, and 4e-14 is nothing to a chart but it is a lie about
the indicator's range and it breaks any rule written as `k >= 100`. `stoch`, `stochrsi` and `mfi`
clamp to 0..100 on the way out. MFI is in that list for the same reason rather than a measured
failure: its money-flow sums are the same kind of running total, and a window whose positive flows
have all rolled out leaves a residue instead of an exact zero, which turns the ratio very slightly
negative. RSI, ADX and Aroon need no clamp and got none — Wilder's update is a convex combination
of values already inside the range, and Aroon divides two exact integers.

**Priming: eight new `PrimeBars()` overrides, and one indicator that cannot honestly have one.** The
Phase 5 rule holds — an indicator with a finite window primes exactly its warmup, one that carries
state past it says so. The Wilder family (ATR, ADX, SuperTrend, and Stochastic RSI through its RSI)
asks for 16× as RSI does; the EMA family (DEMA, TEMA, Keltner, Chaikin Volatility, Chaikin
Oscillator) asks for 8×. `TestPrimingReproducesWhatFullHistoryWouldHaveComputed` now runs 36 keys
and requires every one to land inside half a centavo of what full history would have drawn.

Three indicators are *path-dependent* rather than convergent: Parabolic SAR, SuperTrend and ZigZag
carry a trend flag that only a reversal clears, so no prime depth is provably enough — it is enough
once it reaches back past a flip. They declare a flat 250 bars, which should reach back past
several flips for SAR and SuperTrend on the test walk, and both are in the priming table.
**ZigZag is not**, and that
is the honest reason: a 5% leg on that walk takes ~170 bars, so 250 bars is one or two legs and a
page can genuinely start mid-leg with a different anchor than full history would have had. A panned
chart may redraw a ZigZag leg. Every other indicator in the library will not.

**Chaikin Oscillator is compared relatively, because half a centavo means nothing to a volume
series.** Priming it is a cumulative line under two EMAs: the constant offset cancels, but the
EMA(10) seeded 80 bars back still carries a residue proportional to the *A/D line's* magnitude,
which runs in the tens of millions. The absolute half-tick bound that governs price series is the
wrong instrument, so its priming test asserts the disagreement is under 1e-4 of the oscillator's own
swing. The golden comparison hit the same wall from the other side — OBV and A/D values sit around
1e10, where 1e-6 absolute is below float64 resolution — so `within()` is now
`|got-want| <= 1e-6 * max(1, |want|)`: unchanged at 1e-6 absolute for anything price-sized, relative
above that.

**The reference stayed Python and grew the whole new library, but two of its formulas are
transcriptions and say so.** Everything with a textbook definition is written from the definition,
in another language, so
a formula misremembered in Go has to be misremembered identically in Python to survive — that is
what the second implementation is for. Parabolic SAR and ZigZag have no closed form; they are
sequential state machines, and the Python is a line-by-line transcription of the Go rather than an
independent derivation. It still catches transcription slips and pins the values against future
edits, but it is not the same evidence the other thirty registrations carry.

**`GET /api/v1/indicators` serves the registry plus what the picker cannot derive.** Name, title,
group, overlay flag, `sourced` flag, parameter schema with kind/default/min/max, output names — all
straight off `Spec`. Two additions earn their place: the canonical `key` built from the defaults, so
the picker has a working request string for every entry without knowing the encoding, and `warmup`
for the same instance, so it can say how much history a choice costs before the user makes it. The
body also carries `groups`, `sources` and `max_per_request` so the whole form is drivable from one
response. Cached an hour — it changes only when the binary does.

> **Status: one run of `internal/indicator`, two failures, both fixed.**
> The failures were Stochastic RSI emitting 100.00000000000004 and ZigZag reversing 20 times over one
> peak; both are written up above, the first a change to the library and the second a change to the
> fixture. Nothing else in the package failed, which means the generator had already run and the
> golden comparison passed on all 44 cases — thirty-two new indicators agreeing with a second
> implementation in another language to 6 decimals across 200 real PETR4 bars, on the first attempt.
>
>
> The generator appends 35 cases keyed by canonical indicator key — 44 in all — and rewrites
> `indicator_bars.json` byte-identically from the same committed brapi response. `vet`,
> `staticcheck`, `gosec` and the frontend type-check have not been run against this phase at all.
>
> Carried into later phases, deliberately:
> - ~~**Nothing draws any of it.**~~ **Closed in Phase 7** — the picker, the panes and the colours are
>   driven entirely off the catalogue's `overlay` flag and output names.
> - ~~**`direction` outputs are ±1 series, not markers.**~~ **Answered in Phase 7: they are not drawn
>   at all.** All three emitters are overlays, so a ±1 line on the price pane flattens the price
>   scale. The output stays in the API for Phase 8 to read as a signal, and candle colouring remains
>   available later.
> - **Session-anchored VWAP and session pivots wait on a calendar-aware indicator input.** Both are
>   listed above as the reason the current shapes are what they are.
> - **Ichimoku's leading edge is not emitted.** The 26 bars of cloud that extend past the last candle
>   have no bar to attach to in a per-bar series.
> - **A ZigZag leg can redraw when the chart pans.** Documented above rather than hidden behind a
>   prime depth that would only look like a fix.
> - **Still no server-side cache of computed series.** Thirty-two indicators do not change that
>   answer; the resampler and the indicator walk are both still waiting on profiling.

---

## Phase 7 — Indicators on the chart

- [x] Indicator picker driven by `GET /api/v1/indicators` — the UI never hardcodes a list
- [x] Overlay indicators on the price pane; oscillators in their own pane (Lightweight Charts panes)
- [x] Per-indicator parameter editing, colour, visibility, removal
- [x] ~~Indicator set persisted per user per symbol~~ **Named layouts per user, carried across
      symbols** (`chart_layouts` table) — see the decision below

```sql
CREATE TABLE chart_layouts (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    layout     JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, name)
);
```

*(`00009` created this keyed on `(user_id, symbol_id)`; `00010` reshaped it — the reasoning is
the first decision below.)*

**Done when:** you can stack EMA(9)/EMA(21) on price and RSI(14) below, tweak periods live, and it
survives a reload.

### Decisions this phase forced

**Layouts are a toolkit, not an attachment to a symbol — and that reversed this phase's own first
answer.** The plan's bullet said "indicator set persisted per user per symbol", and `00009` built
exactly that: primary key `(user_id, symbol_id)`, one layout per ticker. Using it exposed the flaw
immediately. An indicator set is a *way of reading a chart* — EMA(9)/EMA(21) with RSI below is a
method, not a fact about PETR4 — so tying it to a symbol means rebuilding the same three indicators
every time you look at a new ticker, and there is no way to keep two ways of reading and switch
between them. The per-symbol model was answering a question nobody asks.

So `00010` reshapes the table: a UUID primary key, a `name`, `UNIQUE (user_id, name)`, and no
`symbol_id` at all. A layout is a named, reusable set that follows you across every ticker, and a
user can hold up to fifty of them. **The migration is not a drop-and-recreate** — it derives each
existing row's name from the ticker it was attached to, so a layout saved against PETR4 survives as a
layout *called* "PETR4". The `Down` reverses that by matching names back to tickers and is
deliberately lossy: a layout named "Ichimoku daily" has no symbol to go back to and is deleted rather
than guessed at.

**The working set and the saved layouts are different things, and conflating them was the other half
of the mistake.** What is on the chart right now is a *working set*: it lives in `localStorage`,
carries across symbols and reloads, and belongs to nobody. A saved layout is a named snapshot you
choose to write down. That split is what makes explicit `Save` meaningful — the previous model
auto-saved every tweak, which is right when there is exactly one layout per symbol and wrong the
moment layouts are named, because every experiment would silently overwrite the thing you named. The
panel shows a `•` against the active layout when the working set has drifted from it, `Save`
overwrites, and `Save as` branches.

**Signed-out layouts live in the browser and do not follow you in.** `localStorage` holds a full list
of named layouts for anonymous use, the server holds them once you sign in, and signing in shows your
account's layouts rather than merging the two. The earlier per-symbol model adopted a local layout on
first sign-in, which worked because there was exactly one candidate; with named collections, merging
means reconciling two sets of names and inventing a conflict rule for something the user never asked
for. The working set still carries across the boundary, so signing in does not disturb the chart in
front of you — only the list of what is saved changes.

**Phase 7 is the phase that had to answer Phase 4's open question, because it is the first one with
anything to save.** "Persisted per user per symbol" needs a user, and the plan's own carry-over said
whoever adds a login screen decides where the access token lives — and that `localStorage` is the
wrong answer. It is: any script that gets injected reads it. So the access token lives in a
module-level variable in `session.ts` and dies with the tab, and the refresh token travels in an
**httpOnly, `Secure`, `SameSite=Strict` cookie scoped to `Path=/api/v1/auth`** that JavaScript cannot
read at all. On boot the app POSTs `/refresh` with no `Authorization` header; the cookie answers, and
that is the entire "am I signed in" check.

**`Secure` is unconditional, and gosec is the reason it is not conditional.** The first cut derived
it per request from `r.TLS` or `X-Forwarded-Proto`, so it would be on behind Caddy and off on plain
`http://localhost`. **G124** flagged both `SetCookie` calls at HIGH confidence: the rule reads the
composite literal, and anything that is not a literal `true` reads as missing. It is right to insist,
and the only other way past it is a `#nosec` directive — a comment, in a project that has none. So
the flag is hardcoded on. The consequence worth knowing: **Chrome, Firefox and Edge accept a Secure
cookie on `http://localhost`**, treating loopback as a secure context, so `make up-dev` and the Vite
proxy both work. **Safari does not**, and neither does reaching the dev box over a LAN address like
`http://192.168.x.x:8080`. In those two cases the cookie is silently dropped and a reload signs you
out — the app still works, it just stops remembering you, and the layout falls back to the
`localStorage` copy. Production is HTTPS behind Caddy, where the question does not arise.

**The change to Phase 4 is additive, and the curl flow it verified still works.** `/login` still
returns the refresh token in the body *and* now also sets the cookie; `/refresh` and `/revoke` take
the bearer header when there is one and fall back to the cookie when there isn't. A client that
never sends cookies behaves exactly as Phase 4 documented. `/refresh` also now returns the `user`
alongside the access token — `GetUserFromRefreshToken` was already selecting those columns and
throwing them away, so a page reload costs one request instead of a refresh plus a lookup, and no
`/me` endpoint had to exist.

**`localStorage` is the wrong place for a token and the right place for an indicator list.** A
signed-out user still gets their chart back after a reload, because the layout is written to
`alvo.layout.<TICKER>` on every change. Signing in **adopts** it: the client asks the server for
that symbol's layout, and on a 404 pushes whatever is on screen up rather than blanking it. So the
upgrade path from "playing with it" to "I have an account" never costs you the chart you built.

**The colour you pick is a palette slot, not a hex.** Phase 3 established that the palette lives in
CSS and the chart reads it back through `getComputedStyle`, precisely so light and dark need no
parallel palette in TypeScript. A stored `#3987e5` would defeat that — it is the dark step of blue,
and it would stay the dark step on the cream theme. So `chart_layouts` stores small integers, the
server validates them as `0..7`, and `--series-1..8` resolves each one per theme. The cost is that
there is no free colour input; the gain is that no user can pick a colour that fails contrast on one
of the two themes, and the swatch row is a better control than a colour picker anyway.

**The eight series colours were computed, not chosen.** The candidate hues went through the
`dataviz` validator against *this* app's two surfaces — cream `#f6f1e7` and ink `#16211f` — not
against the reference surfaces, and the slot **ordering** was searched rather than picked: all 8!
orderings were enumerated, the 1,684 that clear every hard gate in both modes kept, and among those
the one that pushes the candle colours latest was taken. The order is
**blue, yellow, magenta, violet, green, red, aqua, orange** — so the first four indicators on a chart
are all clearly not the green-up/orange-down candles under them, which is the collision that matters
here and that a generic categorical palette does not know about. Both modes pass: worst adjacent
normal-vision ΔE 19.6 light / 19.3 dark against a ≥15 floor. Two results carry obligations, and both
are already met: the red↔aqua adjacency sits in the 6–8 CVD warn band, which is legal only with
secondary encoding, and four light-mode hues fall under 3:1 on cream, which requires visible labels.
The chart legend and the indicator panel both name every series next to its dot, and every
oscillator sits in its own pane — so identity is never carried by colour alone. **Reordering these
slots is not cosmetic**; redo the search if it is ever tempting.

**`direction` outputs are not drawn at all.** Phase 6 left this open — SuperTrend, Parabolic SAR and
ZigZag each emit a ±1 series, and the note said Phase 7 might want them as candle colouring. What
Phase 7 found is that drawing them as-is is not an option: all three are overlays, so a ±1 line
lands on the price pane and flattens the price scale to nothing. Putting it on a hidden auto-scaled
scale would draw a meaningless line across the middle of the chart instead. So the client skips any
output named `direction`, which is one rule keyed on the name the registry already publishes rather
than a list of three indicators to keep in sync. The signal is still in the API response for
whoever wants it; Phase 8 reads it as a rule input, and candle colouring stays available later.

**Slots 9 and 10 are white and black, and they are the only two that do not swap with the theme.**
Everything about the palette is theme-aware by construction — `--series-1..8` have a light step and a
dark step, so blue stays blue and stays legible on both surfaces. White and black cannot work that
way: a "white" that turns black on the cream theme is not the thing anyone picked it for. So they are
defined once on `:root` and inherited by both themes, which means **white on cream and black on ink
are nearly invisible, deliberately** — they are the manual escape hatch for someone who wants a stark
line on the theme they actually use, not a choice the app can validate. Two consequences follow:
auto-assignment when an indicator is added never reaches them (it draws from the first eight, the
validated categorical set, via `AUTO_SLOTS`), and every swatch and legend dot carries a 1px
`--line` ring so a black dot on the ink panel is still findable.

**Line style is per indicator, not per output.** Solid or dotted, applied to every line the indicator
draws. Per-output would be more expressive and is more UI than the distinction earns — the case this
serves is telling two overlapping moving averages apart at a glance, and a dotted MACD signal line
against a solid MACD line is a refinement nobody asked for. It is one string in the stored layout,
validated server-side against `solid|dotted`, so widening it to `dashed` later is a slice literal and
a button. The histogram output ignores it, having no line to style.

**The Ichimoku cloud is a series primitive, because Lightweight Charts has no band series.** There is
no built-in fill-between-two-lines in v5 — the only mechanism is `ISeriesPrimitive`, which lets a
plugin draw onto the pane canvas with the chart's own coordinate converters. `band.ts` implements one:
it takes the two output columns, maps each bar through `timeScale().timeToCoordinate` and the host
series' `priceToCoordinate`, and fills the polygon between them. It draws from `drawBackground` at
`zOrder: 'bottom'`, so the cloud sits under the candles rather than over them, which is where a cloud
belongs.

**The cloud is not an Ichimoku feature, it is an output-name pair.** Hardcoding "ichimoku" would have
been the third name-based special case and the least general of them. Instead the client fills between
any indicator that declares **`senkou_a`/`senkou_b`** or **`upper`/`lower`** — which picks up
Bollinger Bands, Keltner and Donchian for free, with no registry change and no new endpoint. The fill
colour is derived rather than configured: it is the translucent colour of whichever output is on top,
so Ichimoku's cloud flips tone when Senkou A crosses Senkou B — the bullish/bearish colouring the
indicator is read for — while a Bollinger band, whose lines never cross, gets one stable tint of the
upper band's colour. The user's own colour picks drive both. Runs are split at the crossing bar
rather than at the true intersection, which is a half-bar error nobody can see.

**`fancy-canvas` is not imported, on purpose.** The renderer's `target` is typed as
`CanvasRenderingTarget2D`, which lives in `fancy-canvas` — a transitive dependency of
`lightweight-charts` that `package.json` does not declare. Importing it would mean either depending on
npm's flattening or adding a dependency the app never calls directly. `band.ts` declares the two
members it actually uses as a local structural type instead; TypeScript's method bivariance accepts
the real target against it, and nothing breaks if fancy-canvas rearranges the rest of that interface.

**Displacement moved out of the indicator and into the response, as a per-output bar offset.** This
is what unblocked both the Chikou span and the projected cloud, and it cost Ichimoku's emission
contract. Phase 6 had Ichimoku emit Senkou A and B *already delayed* — at bar `i` it emitted the value
computed at bar `i-26` — so the cloud aligned with the candle it was drawn against and no client-side
shifting was needed. That works right up to the last candle and then stops: the values computed over
the final 26 bars belong 26 bars *ahead*, where there is no candle, so the server simply never emitted
them and the leading edge could not be drawn. Worse, the information was gone by the time the client
saw it.

So `Spec` grew an optional `Offsets func(Params) []int`, one entry per output: positive means "draw
this value N bars ahead of the bar that computed it", negative means N bars behind. Ichimoku now
computes in place — at bar `i` it emits the tenkan, kijun, Senkou A and B *of bar `i`* — and declares
`[0, 0, 26, 26, -26]`. The client reads the offsets off the response and does the shifting when it
builds the line data. The division is the same one Phase 3 drew for timezones: the server states the
value and the bar it belongs to, and *placement is a rendering concern*.

**Chikou is an output that duplicates `c[]`, and that is the cheap half of the trade.** Phase 6
refused to emit it because it is the close shifted 26 bars back, which a streaming indicator cannot
produce and which the client already has. Both halves are still true — Ichimoku emits the plain close
and the client draws it at `i - 26`. What the redundancy buys is that Chikou becomes an ordinary
output: it gets a name in the catalogue, a colour slot, a legend row, and the visibility and style
controls, all through machinery that already exists. Special-casing it in the client would have been
a fourth name-based convention and a series with no controls attached to it.

**Ichimoku's warmup dropped by its displacement, and its ring buffers are gone.** The delay was
implemented with two `ring`s holding `displacement + 1` values, and `Ready()` waited for them to fill
— so `ichimoku:9:26:52:26` needed 77 bars before it emitted anything, 26 of them purely to run a
conveyor belt. Computing in place makes the warmup what it should always have been:
`max(tenkan, kijun, senkou) - 1`, or 51. A chart panned to the very start of a symbol's history now
shows the cloud 26 bars earlier.

**Projecting forward needs real future sessions, so the calendar generates them.** Extending the time
axis by "26 more bars" means knowing when those bars open, and on B3 that is not arithmetic —
weekends, holidays and the 84-bucket session shape all matter, and this project has spent two phases
being careful about exactly that. Guessing from the median bar spacing would have put the cloud on
Saturdays. `Calendar.FutureBuckets` walks forward from the newest stored bar through
`NextTradingDay` and `SessionBuckets`, so a projected daily bar lands on the next trading day and a
projected 15m bar lands on a real 15m bucket of a real session. The candle response carries them as
`future`, sized to the largest offset any requested indicator declares — so a request with no
displaced indicator gets no extra field, and the projection is never computed for a page that is not
the newest one, because older pages project onto bars that already exist.

**The last value now labels the price axis in the series' own colour.** `lastValueVisible` was off on
every indicator series, which meant the only way to read a value was to hover. Turning it on gets the
label for free, coloured by Lightweight Charts from the series colour, on the right scale of whichever
pane the series lives in. The price line stays off — eight indicators' worth of dashed horizontals
across the chart is clutter, whereas eight axis labels collide and the library hides the losers. One
consequence worth naming: for a displaced output the "last value" is the *projected* one, so
Ichimoku's cloud labels sit 26 bars ahead of the last candle. That is the number a trader reading a
cloud actually wants.

**`histogram` is the only other output name the client reads.** It renders as a histogram series
instead of a line — MACD is the one indicator that has it today, and any future indicator that
declares an output by that name gets the same treatment for free. Everything else is a line. That is
three name-based conventions total — `direction`, `histogram`, and the band pairs above. Displacement
is deliberately *not* one of them: it arrives as data on the response. That is the whole of what the
client knows about any specific indicator: the picker, the panel, the panes and the legend are otherwise driven entirely by
`GET /api/v1/indicators`.

**Changing the indicator set refetches the whole loaded window in one request, not page by page.**
Indicator values are computed server-side, per page, primed from the bars *before* that page — so
they cannot be requested separately from the candles, and a chart that has panned back four pages
would need four requests to backfill values for bars it already holds. Instead `refresh()` asks for
`min(bars.length, MaxPageLimit)` bars with no cursor, which returns exactly the bars already on
screen because paging only ever runs backwards from the newest. Above 5,000 bars the window shrinks
to the newest 5,000 and the visible logical range is shifted to compensate, so the view does not
jump. Removing the *last* indicator skips the request entirely — the bars are already there and only
the values need clearing.

**Paged indicator values merge by position, and the `start` offset is why that works.** Each series
comes back as `start` plus a dense `values` array; the client expands that into a column of
`number | null` the same length as the page, so prepending an older page is a concatenation of two
columns rather than a timestamp join. The one trap is the duplicate-bar filter Phase 3 left in
`loadOlder`: filtering bars without filtering values identically would shift every column by the
number of duplicates. So the filter now produces a list of *kept indices* and the bars and every
indicator column are both mapped through it.

**Panes are rebuilt only when the structure changes, and re-coloured otherwise.** A rebuild tears
down every indicator series and every pane above the price pane, which also throws away any pane
heights the user dragged. Colour and visibility changes therefore go through `applyOptions` on the
existing series instead, and only adding, removing or hiding an indicator — the cases where the pane
count genuinely moves — pays for a rebuild. Switching candles ⇄ bars forces one too, because
re-adding the price series puts it last in pane 0's z-order and would draw candles over the moving
averages.

**Adding the same indicator twice bumps its first integer parameter rather than refusing.** The
phase's own done-when is EMA(9) *and* EMA(21), and the picker adds with defaults — so a second EMA
would collide on `ema:20` and either be rejected or silently dedupe against the first. It lands as
`ema:21` instead and the user retunes it. The rule is bounded by the parameter's own `max` from the
catalogue, and gives up rather than looping.

**The parameter editor is debounced at 350 ms and keyed by position, not by indicator key.** The key
*is* the parameters — retuning `ema:20` to `ema:9` changes the row's identity — so keying the list
by it would destroy and recreate the number input mid-keystroke and take the focus with it. Keying
by index keeps the DOM node and lets the committed value flow back into it. A retune that would
collide with another entry on the chart is refused and the field snaps back, rather than producing
two rows the server will dedupe into one series.

**The server validates a layout by replaying every key through `indicator.Parse`.** Same parser the
`?indicators=` query param uses, so a layout cannot hold a key the engine cannot build, an
out-of-range period, or a source an indicator does not take — and what gets stored is the
*canonical* key, so `EMA : period=9` and `ema:9` are one row. Colours are checked against the
palette size and padded to one per output. The client mirrors the key encoding in `catalog.ts`
because it has to build request strings before the server ever sees them; the server's answer is
authoritative and the client adopts it on load.

**The migration is `00009`, not `00008`.** An untracked `00008_strategies.sql` for Phase 8 already
sat in the tree. Inserting `chart_layouts` at 00008 and renumbering it would be the wrong move if
that file has already been applied anywhere: goose records versions as integers, so it would consider
00008 done, skip `chart_layouts` entirely, and then fail applying a `strategies` table that already
exists. Numbers are cheap and gaps are harmless; a renumbered applied migration is not.

> **Status: done and verified in the browser.** `make sqlc`, the reference regeneration, `make up`
> and `make check` all ran clean, and the done-when was exercised for real: EMA(9) and EMA(21) stack
> on price with RSI(14) in its own pane below, periods retune live, and the set survives a reload.
>
> Three things needed a second pass and are worth recording, because each was a class of mistake
> rather than a typo:
>
> | what failed | why |
> |---|---|
> | `make sec` | **G124.** `Secure` was derived per request from `r.TLS`/`X-Forwarded-Proto`; the rule reads the composite literal and anything but a literal `true` is "missing". Fixed by hardcoding it on — see the decision above |
> | `go test -race` | `indicator_golden.json` still held Ichimoku's pre-delayed values with `start: 77`. The library was right and the fixture was stale; regenerating was the fix |
> | `svelte-check` | `column` was only computed when `active` existed, but that is a ternary TypeScript cannot narrow through. An explicit `!active` in the guard |
>
> The library-versus-fixture failure is the one to remember: when a golden test disagrees after an
> indicator changes shape, check which side moved before touching either.
>
> Offline tests hit no database: `internal/api/layouts_test.go` covers every rejection path on a
> stored layout — unknown indicator, out-of-range parameter, unknown source, the same indicator
> twice, more indicators than a request can carry, a future layout version, an unknown line style,
> more colours than outputs, a colour outside the palette, and a missing or over-long name — plus
> that keys are stored canonically, that `visible` defaults to true when the field is absent, and
> that colours are padded per output. `internal/indicator` gained the assertion that Ichimoku emits
> at the bar it computed and declares `[0, 0, 26, 26, -26]`, and that every other indicator declares
> one offset per output and all of them zero.
>
> Carried into later phases, deliberately:
> - **Pane heights reset when the indicator set changes.** Dragging a pane divider and then adding an
>   indicator loses the drag. Storing heights in `chart_layouts` is the fix, and it is a JSONB field
>   away — but it needs the rebuild to become a diff rather than a teardown first.
> - **The cloud fill has no toggle.** Any indicator with a `senkou_a`/`senkou_b` or `upper`/`lower`
>   pair gets one. On Bollinger that is a faint tint most people want; if it ever isn't, the switch
>   is one boolean in the stored layout and one condition in `rebuildIndicators`.
> - **A cloud is anchored to its first output's price scale.** Both outputs of a pair are always on
>   the same scale today, since a band is by definition two series in the same units — but nothing
>   enforces it, and a future indicator pairing outputs across scales would fill against the wrong
>   one.
> - **Indicators cannot be reordered.** They stack in the order added, and pane assignment follows
>   that order, so moving an oscillator above another means removing and re-adding it.
> - **The layout stores the indicator set and nothing else.** Not the timeframe, not candles-vs-bars.
>   The plan asked for the indicator set; the other two are one field each whenever they are wanted.
> - **Layouts have no ordering or folders.** They list alphabetically by name, capped at fifty per
>   user, with nothing between "one list" and that.
> - **Renaming is only reachable through `Save`.** `PUT` takes a name, so the API supports a rename
>   on its own; the panel only ever sends one alongside a save of the current working set.
> - **An expired access token is recovered lazily, on the next save.** `layouts.ts` retries once
>   through `/refresh` on a 401 and gives up to a signed-out state if that fails. There is no timer
>   refreshing it in the background, so the first save after fifteen idle minutes costs an extra
>   round trip.
> - **The refresh cookie still does not rotate**, per Phase 4. It now has a browser holding it for
>   sixty days, which raises the value of rotation and reuse detection from "theoretical" to
>   "worth doing before this is public".
> - **A ZigZag leg can still redraw when the chart pans** — Phase 6 documented it; Phase 7 is where
>   it becomes visible.
> - **The projected region has no bottom-row date labels.** Phase 3's two-row time axis is built from
>   `bars`, so the strip stops at the last candle while Lightweight Charts' own top row keeps
>   labelling the projected buckets. Extending it means teaching `updateAxis` about `future`.
> - **Hovering the projected region reads nothing.** The crosshair maps a time back to a bar index,
>   and projected buckets have no bar, so the legend falls back to the last candle rather than showing
>   the cloud value under the cursor.
> - **`future` is only fetched with the newest page.** Correct today, because paging only ever runs
>   backwards from the present. A future "jump to date" that lands mid-history would need the
>   projection recomputed for that window's newest bar.
> - **Changing an indicator refetches candles the browser already has.** The URL carries the
>   `indicators=` list, so a different set is a different cache entry; only the no-indicator URL
>   benefits from Phase 3's `immutable` day. A server-side cache of computed series — still waiting
>   on profiling, as with the resampler — would be the thing that makes this free.

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

- [x] Operands resolve to: a named input, a literal number, a price field (`close`, `high`, …),
      or `{"ref": ["fast", 1]}` for *n* bars back *(plus `volume`, which is not an `indicator.Source`)*
- [x] Comparators: `gt lt gte lte eq crosses_above crosses_below rising falling between`
- [x] Combinators: `all any not`
- [x] Validation on write: unknown indicator, out-of-range param, unresolvable operand, or a cycle
      is a 400 with a **JSON pointer to the offending node** — the builder highlights it inline
- [x] Compile step: spec → a flat evaluation plan with indicators instantiated once and operands
      resolved to slot indices. Compile at run start, not per bar
- [x] Editing a saved strategy bumps `version` and leaves prior backtest runs pointing at the spec
      they actually ran. A run whose strategy silently mutated underneath it is unreproducible
      *(the bump is conditional — a rename alone does not count as an edit; see below)*
- [x] CRUD + `POST /api/v1/strategies/validate` (dry-run, no write)
- [x] Frontend: visual rule builder writing this JSON, plus a raw JSON editor

*(The table landed as `00011_strategies.sql`, with `UNIQUE (user_id, name)` and a `strategies_user_idx`
added on top of the DDL above — the same shape Phase 7 gave `chart_layouts`.)*

**Done when:** an EMA-cross strategy round-trips UI → JSON → validate → save → compile without loss.

### Decisions this phase forced

**A stop-loss is not a condition, and treating it as one is a category error the JSON hides.** The
plan's own example puts `stop_loss` and `take_profit` inside the exit's `any`, next to a crossing —
and read as English that is exactly right: *exit when the fast line crosses down, or the stop is hit,
or the target is hit.* But a crossing is evaluated at the close of bar *i*, while a stop is a resting
order that fills **intrabar**, at a price no rule ever compares. They are different machines. Left in
the tree, nothing stops you writing `{"not": {"stop_loss": …}}` or burying a stop inside an `all`,
neither of which means anything a broker could execute.

So the parser accepts brackets exactly where they read naturally — as the whole exit, or as direct
members of the exit's top-level `any` — and rejects them anywhere else with a pointer. **Compile then
hoists them out**: `Plan.Stop` and `Plan.Target` come out as `*Bracket`, and `Plan.Exit` is whatever
condition tree is left, which may be nothing at all. Phase 9 gets the two things it actually needs
already separated: a rule to evaluate at each close, and up to two resting orders to check against
the bar's high and low. An `atr` bracket also gets a slot, so its ATR is one more indicator in the
same plan rather than a special case in the engine — and it **deduplicates against a user-declared
`atr` input with the same period**, so declaring ATR(14) as an input and stopping at 2×ATR(14) builds
one indicator, not two.

**Chained inputs are what make the cycle checkbox mean anything.** The plan asks for a cycle to be a
400, but with inputs that only ever read price, a cycle is unreachable — the checkbox would be
decoration. It becomes real once an input's `source` may name *another input*, which is also the
feature people actually want: an SMA of RSI, an EMA of a smoothed oscillator. So `source` resolves to
a price field **or** an input name, `findCycle` walks the graph and reports the loop by name
(`a → b → a`) at the offending `/inputs/<name>/source`, and units are instantiated in topological
order so an upstream slot is always filled before the thing reading it updates.

Chaining is only allowed into indicators that declare `Sourced` — an ATR reads a whole candle, and
"ATR of RSI" is not a thing. The downstream indicator is fed a **synthetic candle whose O/H/L/C are
all the upstream value**, so whichever source field it picks yields that value; and it is fed
*nothing at all* on bars where the upstream has not produced a value yet, which is what makes an
SMA(3) of RSI(14) start counting its three bars from RSI's first output rather than from bar zero.
Warmup adds down the chain for the same reason.

**Evaluation is three-valued, and that is the lookahead guard.** A rule whose operands are not
available yet — an indicator still warming up, a `ref` reaching further back than the tape holds —
returns *unknown*, not false. It has to: with two values, `{"not": {…}}` over a warming indicator
evaluates to **true**, and a strategy would fire an entry on bar zero for no reason. So `Rule.Eval`
returns `(value, known)` and combinators do proper Kleene logic — `all` is false the moment one child
is known-false, `any` is true the moment one child is known-true, and anything still undecided
propagates as unknown. Only `known && value` signals.

The other half of the guard is structural: `Tape` is a ring of the last `Depth + 1` bars and its only
readers are `Slot(slot, back)` and `Field(field, back)`, both of which take a **non-negative** offset
and refuse anything beyond what has actually been pushed. There is no method on the frame that could
return a future bar, which is the property Phase 9's bar loop needs to inherit.

**Canonicalisation pins every parameter, so a saved strategy cannot drift under a library change.**
Validation does not just accept or reject — it returns a canonical spec, and that is what gets
stored. `{"indicator": "rsi"}` comes back as `{"indicator": "rsi", "params": {"period": 14},
"source": "close"}`; `{"rising": ["fast"]}` comes back as `{"rising": ["fast", 1]}`; a
`{"ref": ["fast", 0]}` collapses to `"fast"`. The reproducibility argument is the same one behind
copying the spec into `backtest_runs`: if a future phase changes RSI's default period, every strategy
that had merely *implied* 14 would silently become a different strategy. Written down, they cannot.
`output` is the one thing left implicit for single-output indicators, because `"output": "ema"` on
every EMA is noise in a file people hand-edit.

**Costs default to what B3 charges, not to zero.** An omitted `costs` block becomes
`fee_bps: 3.25`, `slippage_bps: 5`, `brokerage_cents: 0`, and the defaults fill in per field, so
`{"fee_bps": 0}` means zero fees and still carries slippage. Defaulting to zero would have been the
neutral-looking choice and the wrong one: the failure mode of a backtester is a result that looks
better than reality, and free trading is the most flattering assumption available. Since the
canonical spec is stored, whatever was charged is always readable off the run.

**`version` in the spec and `version` on the row are different numbers wearing the same name.** The
JSON `"version": 1` is the *format* version — it gates how the parser reads the document, and a `2`
is rejected today. The `version` column is an *edit counter* on one user's strategy. Nothing links
them, and nothing should. The column is also bumped **conditionally**, in SQL:
`version + (spec IS DISTINCT FROM $5)::int`. Renaming a strategy or fixing its description is not a
change of logic, and a version number that ticks on a typo tells you nothing about which runs are
comparable. `jsonb`'s `IS DISTINCT FROM` compares semantically, which is only safe *because* the
stored spec is canonical.

**Input names are identifiers, not free text.** Lower-case, starting with a letter, digits and
underscores after, at most 24 characters, and never a price field name. Two reasons: an input called
`close` would shadow a price field in every operand position, and a name containing `/` or `~` would
need RFC 6901 escaping in the very pointers the builder relies on to highlight the right box.
Escaping is implemented anyway, but no valid spec can exercise it.

**Empty combinators are rejected rather than given their mathematical identity.** `{"all": []}` is
vacuously true and `{"any": []}` is vacuously false, which is correct logic and a terrible entry
rule — an empty `all` would open a position on every single bar. Both are 400s that say which one it
would have been.

**`eq` on floats compares with a tolerance.** Exact float equality between an indicator value and a
literal essentially never fires, so `eq` would be a comparator that silently does nothing. It uses a
relative epsilon of 1e-9. It is still the wrong comparator for almost every strategy; it is in the
list because the plan asked for it.

**`exit` is optional, because buy-and-hold has to be expressible.** Phase 9's done-when is that a
buy-and-hold strategy returns the underlying's return minus one round trip of costs — and buy-and-hold
is precisely a strategy with an entry and no exit, closed at the end of the run. A spec with no `exit`
key compiles to a plan with a nil `Exit` rule and no brackets. `entry` and `sizing` stay required.

**Strategies need an account; the builder does not.** Unlike Phase 7's layouts, there is no
`localStorage` fallback — a strategy exists to be backtested, and backtests are server-side rows
against a `user_id`. But the builder, the rule tree and the JSON tab all work signed out, with
Validate and Save disabled; you can compose a spec and read exactly what would be sent. Merging an
anonymous strategy into an account on sign-in is the same "reconcile two sets of names" problem
Phase 7 declined, and it buys less here.

**`POST /strategies/validate` sits behind `RequireAuth` even though it touches no user data.** It
parses and compiles arbitrary JSON, instantiates indicators and walks a tree — it is the only
unauthenticated-by-nature endpoint in the app that costs real CPU per call. Phase 4's rule was that
market data is public and user data is not; this is neither, so the cheaper answer wins until Phase
13 has rate limiting.

> **Status: done and verified in the browser.** `make sqlc`, `make migrate` and `make check` all ran
> clean **on the first pass** — gofmt, vet, staticcheck, gosec, the Go suite under `-race` and
> `svelte-check` — which is the first phase since 0 where nothing needed a second attempt. The
> done-when was exercised for real: an EMA(9)/EMA(21) cross goes UI → JSON → validate → save →
> compile, and comes back canonicalised with `"period"`, `"source"` and `"fee_bps"` spelled out
> even though none of the three was typed.
>
> The clean first pass is worth attributing rather than celebrating: this phase touched no
> indicator maths, no chart rendering and no auth surface — the three places every earlier phase
> drew blood. It added a parser, a compiler and a form. Phase 9 goes back to arithmetic that has a
> right answer, and should be expected to behave like Phases 5 and 7 did.
>
> **The conditional version bump survived code generation.** `$5` appears twice in `UpdateStrategy` —
> once as `spec = $5` and once inside `version + (spec IS DISTINCT FROM $5)::int` — and the worry was
> that sqlc would split it into two parameters or name it `dollar_5`. It did neither: one
> `Spec []byte` field, five arguments. Worth remembering the next time a query wants to read a
> column's old value in the same statement that overwrites it.
>
> Offline tests hit no database. `internal/strategy/parse_test.go` asserts the **exact JSON pointer**
> for forty-odd rejections — unknown indicator, out-of-range and fractional and undeclared
> parameters, an unknown source, a source on an indicator that reads whole candles, volume as a
> source, an unknown output, an input reading from itself, two inputs in a loop, a price field used
> as an input name, a name that is not an identifier, an unknown field, an unresolvable operand, an
> unknown comparator, wrong operand counts, empty `all` and `any`, a rule with two operators, a
> bracket on the entry side and under an `all` and under a `not`, two stops, a `pct` level carrying
> a `period`, an out-of-range level, a ref beyond the tape and a ref onto a number, bad sizing types
> and values, `risk_pct` with no stop, costs beyond any broker, a future spec version, a backwards
> `between`, and a non-literal bar count — plus that the plan's own example spec survives a second
> canonicalisation byte-for-byte. `compile_test.go` covers instance and slot dedup, multi-output
> slots, the bracket hoist, chained warmup, and look-back depth per comparator. `eval_test.go` walks
> synthetic candles and asserts the exact bar each rule fires on, including that nothing fires while
> an indicator is warming and that `not` does not leak a signal out of unknown.
>
> Carried into later phases, deliberately:
> - **Validation reports one fault, not all of them.** The plan asked for *a* pointer and the builder
>   highlights one box at a time, but a JSON-tab author fixing six things pays six round trips. The
>   response shape (`{error, pointer}`) has room for a `faults` array whenever that stops being
>   acceptable.
> - ~~**Short selling is not modelled.**~~ **Closed after Phase 10** — `entry` and `exit` take
>   `long`, `short`, or both, and the prediction held exactly: every Phase 8 spec still parses,
>   because adding an optional key is not a format change.
> - **Nothing evaluates the brackets yet.** `Plan.Stop` and `Plan.Target` carry the level and, for
>   `atr`, the slot to read the range from. Turning that into a fill — and deciding the stop wins
>   when a bar's high and low touch both — is Phase 9's intrabar-ambiguity bullet.
> - **A declared input that no rule mentions is still computed.** It costs an indicator per bar for
>   nothing. Dropping unreferenced slots at compile time is a filter over `Plan.Index`, deferred
>   because Phase 10 may well want to chart inputs the rules never compare.
> - **`fixed_cash` sizing is in cents and `pct_equity` is a fraction**, both under one `value` field,
>   because the plan's shape has one. The validation messages name the unit; the builder does not
>   convert.
> - **The builder cannot reorder or drag rules.** Conditions sit in the order they were added, and
>   moving one means removing and re-adding it — the same limitation Phase 7's indicator list has.
> - **A spec the builder cannot represent falls back to the JSON tab.** `specFromJSON` throws rather
>   than guessing, and the editor switches tabs with the reason. Nothing today produces such a spec,
>   but a hand-edited file with an unknown operator would.
> - **The plan is stateful and single-run.** It holds live indicator instances, so a `*Plan` belongs
>   to one goroutine; `NewTape` resets them. Phase 9's worker pool must compile per run, which is
>   what "compile at run start" asked for anyway.
> - **`Slot.Name` is recorded but nothing reads it.** It is there so Phase 10 can label a series by
>   the input that asked for it rather than by an indicator key.

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

- [x] **The bar loop, and the one rule that matters:** at bar *i*, feed the candle to indicators,
      evaluate rules using values as of *close of bar i*, emit intents. Intents fill at **bar
      *i+1* open**. The engine must make it structurally impossible for a rule to read bar *i+1* —
      lookahead bias is the single most common way a backtester produces a beautiful lie
- [x] Broker sim: market / limit / stop orders, plus bracket (stop-loss + take-profit) attached
      to a position
- [x] **Intrabar ambiguity:** when a bar's high hits the take-profit *and* its low hits the stop,
      OHLC cannot say which came first. Assume the **stop** filled. Pessimistic and consistent;
      record the ambiguity count in the run metrics so you know how often it mattered
- [x] Slippage: bps on the fill price, applied against you. Fees: `brokerage_cents` fixed +
      `fee_bps` on notional. B3 emolumentos are roughly 3.25 bps — configurable, not hardcoded
- [x] Position sizing: `fixed_qty | pct_equity | fixed_cash | risk_pct` (the last sized off the
      ATR stop distance)
- [x] Round to `lot_size` and `tick_size` from the symbol row. A backtest buying 137 shares of a
      100-lot stock is not a trade that existed
- [x] Long-only first. ~~Short selling in a second pass~~ — **the second pass landed after Phase 10**;
      borrow cost and shorting restrictions are still their own problem, and still unmodelled
- [x] Worker pool draining `backtest_runs WHERE status = 'queued'` with `FOR UPDATE SKIP LOCKED`.
      `SKIP LOCKED` is what makes `docker compose up --scale app=3` safe without a queue broker
- [x] Container CPU/memory limits in compose, and a worker count derived from `GOMAXPROCS` — which
      respects the container's CPU quota in Go 1.25, so it needs no manual tuning
- [x] `POST /api/v1/backtests` → 202 + run id; `GET /api/v1/backtests/{id}` → status/result
- [x] Determinism: same spec + same candles = byte-identical metrics. A test asserts this

*(The tables landed as `00012_backtests.sql`, with `CHECK`s on `status`, `timeframe`, `capital_cents`
and `end_date >= start_date` added on top of the DDL above, plus
`backtest_runs_queue_idx ON (created_at) WHERE status = 'queued'` — the partial index the claim
query actually walks — and `backtest_runs_user_idx ON (user_id, created_at DESC)`.)*

**Done when:** a buy-and-hold strategy returns exactly the underlying's return minus one round
trip of costs. That single test catches most engine bugs.

### Decisions this phase forced

**The bar loop has five steps, and their order *is* the lookahead guard.** For each bar the engine
does: fill whatever intent last bar's close produced, against this bar's open → check the resting
brackets against this bar's high and low → **push the bar into the tape** → evaluate the rules →
mark equity at the close. The push is third on purpose. When an intent fills, the tape has not yet
seen the bar it is filling on, and when the rules run, the only intent they can create is one that
no code will look at until the next iteration. A rule cannot influence a fill on the bar it was
evaluated on because at fill time the rule has not run yet, and it cannot read the bar it fills on
because at rule time the tape is one bar behind. That is a stronger guarantee than "remember to use
`i+1`" — it is a property of the sequence, not of the arithmetic. Phase 8's `Tape` supplies the
other half: `Slot` and `Field` take non-negative offsets only, so there is no expression a rule
could write that reaches forward.

**The final bar closes the position before its equity is marked, not after.** An open position at
the end of the run is sold at the last bar's close with `exit_reason = end_of_run`, and only then
does the last equity point get written. Marked the other way round, the curve's last value would
carry a position the trade table says was closed, and the run's return would disagree with the sum
of its own trades — the first thing anyone checks. It also means a pending entry on the final bar
is dropped rather than filled: there is no next open to fill it at, and inventing one is exactly the
lie the bar loop exists to prevent.

**The stop wins an ambiguous bar, and how often that happened is a metric.** When a bar's low
reaches the stop and its high reaches the target, OHLC cannot say which came first — the bar is four
numbers, not a tape. The engine fills the stop. What matters more than the choice is that
`ambiguous_bars` is in the metrics: a run with three ambiguous bars out of two hundred is telling
you something different from a run where half the exits were coin flips, and the second one should
not be read as a result at all. Phase 11's finer answer, if it is ever wanted, is to resolve the
ambiguity from the 5m base candles the daily bar was folded from; the count is what tells you
whether that work would pay.

**A limit fill is not slipped; a stop fill is.** Slippage "against you" is right for a market order
and right for a stop, which becomes a market order the instant it is touched — but a resting limit
fills at its price *or better* or it does not fill at all, and shaving basis points off it models a
fill that no exchange would print. So the take-profit fills at exactly its level, while the
stop-loss carries the same slippage as an entry. Both orders are honest about gaps: a bar that opens
straight through a sell stop fills at the **open**, not at the stop price, which is the loss you
would actually have taken. The mirror case is price improvement — a bar that opens above a sell
limit fills at the open — and it is in there for symmetry, not generosity.

**Brackets are priced at the signal, anchored to the fill, and an entry that cannot price its
bracket does not happen.** An ATR stop reads its ATR at the close of the *signal* bar, because that
is the information you had when you placed the order; the resulting distance is then hung off the
price you actually got at the next open. The consequence worth stating: if the bracket's ATR is
still warming when the entry rule fires, the engine **skips the entry** and counts it in
`skipped_entries` rather than entering without the stop. A strategy stripped of the stop it
declared is a different strategy, and it is the more flattering one — it never takes the small loss.
This is the same three-valued discipline Phase 8 applied to rules, extended to the orders the rules
imply. It is also why the metric exists: five skipped entries at the start of a run is warmup,
five hundred is a spec whose stop can never be priced.

**Sizing runs at the fill price, not the signal price.** Sizing off the close you saw is the
intuitive choice and it breaks on the first gap: a position sized against last night's close is
unaffordable at this morning's open, and cash goes negative — silently, since nothing in a backtest
bounces. So the order is fill price → stop distance → quantity → afford check → lot floor. The
affordability clamp is computed directly (cash, less fixed brokerage, over price plus the fee rate)
and then walked down one lot at a time until the total cost fits, because the direct form cannot
account for the rounding that lot flooring introduces. `pct_equity` reads *cash*, which is
unambiguous here only because the engine is flat whenever it sizes: one position at a time, no
pyramiding, so equity and cash are the same number at the only moment sizing is asked for.

**Lot flooring is why 137 shares became 100.** `lot_size` and `tick_size` come off the symbol row,
not from a constant: quantities floor to a whole lot, and prices round to a tick **against you** —
up on a buy, down on a sell. The tick rounding carries an epsilon check that returns the price
untouched when it already sits on a tick, which matters more than it sounds: without it,
`price/tick` float noise would occasionally kick an exact price up a whole tick, and the
buy-and-hold test would stop being exact for a reason that has nothing to do with trading.

**Money is integer cents; prices stay float64.** Cash, equity, PnL and fees are `int64` cents, and
every conversion from a price crosses through one `math.Round` at the notional. Prices are float64
because that is what `NUMERIC(18,6)` becomes coming through sqlc, and converting them to a decimal
type at the boundary would buy nothing the rounding at the notional does not already buy. The
determinism the plan asked for falls out of this: same spec, same candles, same sequence of float
operations, and the only place a fraction of a cent could accumulate is closed off by rounding at
a fixed point. `Metrics` is a struct rather than a map, so its JSON field order is fixed too, and
nothing inside a run reads the clock, iterates a map, or spawns a goroutine. The test runs the same
spec over the same bars twice and compares the marshalled results byte for byte.

**The done-when measures the underlying from the fill bar's open, not from the first close.** A
buy-and-hold strategy is an entry that is always true and no exit; it buys at the second bar's open
and sells at the last bar's close, so "the underlying's return" for the purposes of that test is
`(last close − fill open) × qty`, and the run matches it to the cent with costs zeroed. With costs
switched on, the difference is exactly one round trip — two brokerages and two `fee_bps` charges —
which the test asserts as a subtraction rather than by recomputing the fees a second way.

**A reversal costs a bar, and that is the model being honest.** An exit signalled at one close fills
at the next open, and the entry rule is only evaluated at *that* bar's close, so flipping out of a
position and back in takes one flat bar. The alternative — evaluating the entry immediately after
the exit fills, mid-bar — would be reading a price the rule is not entitled to. Strategies that look
good only when they can reverse instantly are strategies that look good only in a backtester.

**The queue is the database, and `SKIP LOCKED` is the whole trick.** `ClaimBacktestRun` is one
`UPDATE ... WHERE id = (SELECT ... FOR UPDATE SKIP LOCKED LIMIT 1) RETURNING *`, which means two
containers racing for the same queued run cannot collide and neither blocks: the loser skips the
locked row and takes the next one. No broker, no leases, no heartbeat table. Workers wake on an
in-process nudge channel when the API queues a run in the same container, and fall back to a 2s poll
for runs queued by another replica — the poll is the correctness path, the nudge is only latency.
Every run carries a 10-minute deadline, and a janitor requeues anything left `running` for 30
minutes; the deadline being shorter than the threshold is what makes the janitor safe under
`--scale app=3`, since a live run can never look abandoned. A clean shutdown returns the in-flight
run to `queued` rather than failing it, using a context detached from the cancellation that caused
the shutdown.

**Workers are `GOMAXPROCS - 1`, floored at one.** Go 1.25 derives `GOMAXPROCS` from the container's
CPU quota, so `cpus: "2.0"` in the prod compose file yields one worker and leaves a core for serving
requests — which is the point of subtracting one. A backtest is a tight CPU-bound loop with no IO
after the candles are loaded; letting it saturate every core would make the API's latency a function
of how many people are running backtests. More throughput is a horizontal question:
`--scale app=3` gives three containers, three workers, and `SKIP LOCKED` sorts out who runs what.

**A truncated candle range is an error, not a shorter backtest.** `CandleService.Load` takes a row
limit and returns the *oldest* rows that fit, so a range that exceeds it would silently produce a
run that stops early and reports a return for a period it never covered. The runner compares the
base row count against the limit and fails the run with a message that names the fix — narrow the
range or use a wider timeframe. Fifty thousand base rows is roughly seven thousand hourly bars or
two hundred years of daily ones; it binds on 5m ranges and almost nothing else.

**A run copies the spec, so deleting a strategy that has runs is a 409.** `backtest_runs.spec` holds
the canonical JSON that actually ran, and `strategy_id` references `strategies(id)` with no cascade
— exactly as the plan's DDL wrote it. That combination means `DELETE /strategies/{id}` can now fail
on a foreign key, so the handler catches `23503` and answers 409 with the reason rather than a 500.
Cascading instead would have deleted the runs, which is the wrong default for the one table in the
app that exists to be a record of what happened.

**Validation of a run happens twice, deliberately.** The API parses and compiles the stored spec
before queueing, so a spec that cannot compile is a 400 while someone is looking at the screen
rather than an errored row discovered later; the worker then compiles it again at run start, because
a `*Plan` holds live indicator instances and belongs to exactly one goroutine — the constraint
Phase 8 wrote down and this phase is the first to actually need.

> **Status: done, and verified end to end against the store.** `make sqlc`, `make migrate` and the
> full check run — gofmt, vet, staticcheck, gosec, the Go suite under `-race`, and `svelte-check` —
> all pass on the generated tree. Both guesses about the generated code held: `:copyfrom` added
> `CopyFrom` to the `DBTX` interface in `internal/db/db.go` without disturbing
> `market.NewCandleService`, which takes `DBTX` and is handed a `*pgxpool.Pool` everywhere; and
> `GetBacktestRun`'s `r.*, s.ticker` produced a `GetBacktestRunRow` rather than reusing the model.
>
> The offline tests are what they were built to be — `broker_test.go` walks every order kind against
> a single bar including both gap cases, and `engine_test.go` runs whole strategies over hand-written
> candles and asserts the exact bar each fill lands on, the done-when among them. But the path
> through Postgres is only provable in Postgres, and it ran: PETR4 daily, 2023-01-02 to 2024-12-30,
> buy-and-hold at `pct_equity 0.95` on R$100,000. **Claimed 3ms after the API queued it** — the nudge
> channel, not the 2s poll. 499 bars in, 499 equity rows out, first `2023-01-02` and last
> `2024-12-30`, so the `DATE` round trip keeps both ends of the range. One trade: in at 22.96 on
> **2023-01-03**, the bar *after* the signal, out at 36.17 on the last bar with `end_of_run`, 4100
> shares. `bars_in_market` came back 497 — `bars - 2`, the first bar flat and the last one closed
> before its equity was marked.
>
> **The money reconciles by hand, which is the check that mattered.** R$95,000 of budget at 22.96
> buys 4137 shares, floored to 4100 by the 100-share lot. 4100 × 22.96 is 9,413,600 cents and 3.25
> bps of that is 3,059; the exit's 14,829,700 cents costs 4,820; 3,059 + 4,820 is the 7,879 the run
> reported, and 14,829,700 − 9,413,600 − 7,879 is its 5,408,221 to the cent. The 54.08% return sits
> under the underlying's 57.53% over the same two fills because 5% of capital never left cash by
> construction and lot rounding left a little more behind — R$5,833 idle — with about 16.5 bps going
> to costs. Every number in the metrics block is reachable from the two fills, which is the property
> that makes the engine arguable with rather than merely green.
>
> Carried into later phases, deliberately:
> - **No list endpoint and no `/trades` or `/equity`.** The plan puts those in Phase 10; until then a
>   run id comes back in the 202 and in the `Location` header, and that is the only handle on it.
> - **Short selling is still not modelled**, on either side of the boundary: Phase 8's parser rejects
>   `"short"`, and the engine only ever sends `Buy` to open. `side` is still a column and
>   `OrderSide` is still an enum, so the second pass adds a direction rather than a concept.
> - **`OrderLimit` and `OrderStop` exist for the brackets and nothing else.** No rule can emit a
>   resting order today — entries and rule exits are always market-at-next-open — so the limit paths
>   are exercised by the take-profit and by tests. A limit entry is a spec change, not an engine one.
> - **One position at a time, no pyramiding, no partial exits.** The whole position leaves on the
>   first thing that fires. Scaling out means a trade row that is no longer one row.
> - **The equity curve is one row per bar**, capped by the same 50,000-bar limit as the run. A 5m run
>   over a year is around 19,000 rows; `:copyfrom` writes them in one round trip, and Phase 10 will
>   want to downsample them for the chart rather than ship them all.
> - **`skipped_entries` counts, but does not say why.** Warmup, an unpriceable bracket, and not
>   enough cash for a single lot all land in the same counter. Splitting it is a metrics change
>   Phase 10 can make when the report has somewhere to show it.
> - **Metrics are the arithmetic Phase 9 needed to test itself** — bars, trades, wins, losses, fees,
>   final equity, return, exit reasons, ambiguity. Drawdown, Sharpe and the rest are Phase 10's list,
>   and they read the equity curve rather than the engine, so nothing in here has to change to get
>   them.
> - **The engine trades raw OHLC, never `adj_close`.** `indicator.Candle` has no adjusted field, so
>   the indicator library, the charts and now the engine all read raw prices. That is at least
>   consistent, and it is wrong for any total-return question: the PETR4 run above reports a price
>   return and silently drops one of the heaviest dividend streams on the exchange. Phase 10 is where
>   this has to be settled, because it is a decision about what a return *means* — and its benchmark
>   comparison is meaningless unless both sides are on the same basis.
> - **The janitor's thresholds are constants, not config.** 10-minute run deadline, 30-minute stale
>   window, 2-second poll. They are only wrong if a legitimate run can exceed ten minutes, which the
>   50,000-bar cap is meant to prevent — if that stops being true, the deadline moves first and the
>   stale window has to move with it.

---

## Phase 10 — Metrics and reports

- [x] Returns: total, CAGR, annualized vol
- [x] Risk: max drawdown (value + duration), Sharpe, Sortino, Calmar
- [x] Trades: count, win rate, profit factor, expectancy, avg win/avg loss, largest win/loss,
      max consecutive losses, avg holding period, time in market
- [x] Benchmark comparison against buy-and-hold of the same symbol, and against IBOV
- [x] `GET /api/v1/backtests/{id}/trades` and `/equity`
- [x] Frontend: equity curve, underwater/drawdown plot, trade table, and **entry/exit markers drawn
      on the price chart** — seeing the trades on the candles is where strategy bugs become obvious
- [x] Risk-free rate for Sharpe comes from the CDI/Selic, not from 0

*(`00013_backtest_reports.sql` adds `backtest_trades.dividends_cents` and the nullable
`backtest_equity.hold_cents` / `index_cents`, so one row per bar now carries all three curves.
`GET /api/v1/backtests` landed alongside the two the plan asked for, because a list endpoint is
what turns a run id into something the UI can find again.)*

**Done when:** a run produces a report you'd actually trust to reject a strategy.

### Decisions this phase forced

**Dividends are credited as cash, which is what settled Phase 9's open question about what a
return means.** The engine still trades raw OHLC — fills print at prices that existed — but a
sixth step now runs at the top of the bar loop, before the fill: if a position is open and the bar
is an ex-date, `qty x dividend` lands in cash. The dividend itself is derived rather than stored,
from the only adjustment data brapi gives us: with `r = adj_close / close`, the cash that went ex
at bar *i* is `close(i-1) x (1 - r(i-1)/r(i))`. Measuring the four tokenless tickers first is what
made this safe to do — `close` comes back **already split-adjusted** (MGLU3 sits at R$31 in April
2023, post-grouping), so the ratio moves on dividends alone and the derivation is not quietly
fighting a second corporate action. The scale of what was being dropped justifies the work:
PETR4's cumulative factor runs 0.2983 to 1.0 over five years.

**Crediting before the fill is the whole correctness argument, and it is the same trick as Phase
9's ordering.** The dividend belongs to whoever held at the *previous* close. Running `credit`
first means an entry filling at this bar's open misses it — right, since buying on the ex-date
buys the stock without the dividend — while a position exiting later on this same bar still
collects it, because at credit time it is still open. Both cases fall out of the sequence rather
than out of a date comparison anyone has to keep straight.

**A run says which basis it is on, because the basis is not a property of the app.** `adj_close`
is populated on 99.9% of daily bars and on **none** of the 5m ones, so a 5m run is a price return
no matter what the daily run beside it is. `metrics.basis` is `total_return` or `price_return`,
`unadjusted_bars` counts the stretches inside a total-return run that had nothing to work with,
and the report says so in words rather than leaving the reader to assume. Two numbers on different
bases are not a comparison, and the one place that matters most is the benchmark.

**A jump too big to be a dividend is counted, not credited.** The largest genuine implied yield in
the committed data is PETR4 at 18.37%; a 2:1 split would read as 50%. `maxImpliedYield` sits at
30% between them, and a bar past it lands in `unpriced_actions` instead of paying out. Crediting
it would invent cash no shareholder received; silently applying it as a price move would be worse.
Adjusting the *share count* through a split is the real fix and it is Phase 11's, since it changes
what a trade row is. The counter is what tells you whether that work would pay.

**Profit factor is null, not infinity, and that is a bug the type system did not catch.** Gross
win over gross loss is undefined for a run that never lost, and the obvious `math.Inf(1)` makes
`json.Marshal` **fail** — which would have errored the whole run at the write, for the strategies
that did best. A zero would have been worse than the crash, since it reads as the opposite of what
happened. So the field is `*float64`, the JSON is `null`, and the report prints "no losing trade".
A test now marshals the metrics and asserts the null, because the failure mode is invisible until
a run happens to have no losing trade.

**The benchmark buys at the second bar's open, not the first.** A buy-and-hold decision is made
before the run starts, so bar zero's open is arguably available to it — but the engine structurally
cannot fill before bar one, and handing the benchmark a bar no strategy can have would tax every
strategy by an amount that has nothing to do with the strategy. It carries the same costs, the same
lot floor and the same dividends as the engine, so the difference between the two curves is the
strategy and nothing else. The `TestBuyAndHoldBenchmarkMatchesAHeldPosition` case pins this: an
always-long spec has to score exactly zero excess.

**IBOV is absent, and absent is a value.** `^BVSP` still answers `401` without a Pro token, so the
index benchmark reports `unavailable` with the reason rather than a flat 0% that reads as a real
comparison. It is also the one benchmark that needs no dividend handling — Ibovespa is already a
total-return index, so a total-return strategy meets it on its own basis. When Phase 12 buys the
token, the series appears and nothing in the code changes.

**The risk-free rate is a committed dated series, and it admits where it ends.** `data/rates/selic.json`
holds Copom's decisions as dated steps, loaded and validated exactly like `b3_holidays.json` and
`contracts.json`. The rate is converted on the Brazilian convention the file declares —
`basis: 252` — so the daily rate compounds back to the annual figure over 252 business days, and an
intraday bar takes its share of a day rather than of a calendar year. What matters more than the
arithmetic is the `through` field: a run reaching past it sets `risk_free_stale`, and the report
says Sharpe is carrying the last known rate forward. A flat rate over 2021-2025 would have been
wrong by twelve points of Selic without ever saying so.

**Annualization reads the trading calendar rather than a constant.** `BarsPerYear` is 252 for daily
and `252 x floor(session / bucket)` for everything else, so the 7-hour B3 session yields 84 five-
minute bars a day and the same Sharpe formula works at every timeframe. Hard-coding a bars-per-year
per timeframe would have been four constants that drift the first time the session hours change.

**The equity curve is downsampled in Postgres, on a stride that always keeps both ends.**
`ListBacktestEquity` numbers the rows in a window and keeps `(n - 1) % stride = 0 OR n = total`, so
a 19,000-point 5m run comes back as ~1,900 for the chart while the first and last points — the two
that define the return — survive by construction. The response says `sampled` and reports the true
`total`, so a thinned curve never passes as the whole thing.

The predicate was first written `n % stride = 1`, which is the same set for every stride above one
and **empty for a stride of exactly one** — `n % 1` is always 0, so a run shorter than the point
budget returned only its final bar and every chart drew a single dot. Offsetting the numbering
instead of the remainder is what makes the no-downsampling case fall out of the same expression as
the rest, and the case that broke was the common one: 495 daily bars, well under the 2,000 asked
for, needing no thinning at all.

**Both benchmark curves live on the equity table, not in the metrics blob.** Three aligned numbers
per bar in `backtest_equity` beats several thousand points inside a JSONB column: one query feeds
the chart, the columns are nullable so a run without an index benchmark simply has nulls, and the
`/equity` endpoint omits a curve entirely rather than shipping a ragged one.

### What the first real strategies forced

**A rule can name any line an indicator emits, so one declaration reaches all of them.** An input
still declares its own `output`, but a rule may now write `cloud.kijun` for any other line of the
same input. The slot is attached on first use, which keeps the cost honest in both directions:
Ichimoku is computed once no matter how many of its five lines are read, and a line nothing reads
costs no slot at all. Naming the input's declared output — `cloud.tenkan`, since tenkan is
ichimoku's first — resolves to the slot the input already had rather than a second copy, and the
canonical spec drops a line name from a single-output indicator because `fast.ema` and `fast` are
the same series. Without this, reaching five Ichimoku lines meant five inputs out of a budget of
twelve, which is the definition of unusable.

**Short selling is modelled, and the Plan grew a leg for each direction.** The four flat fields
`Entry`, `Exit`, `Stop` and `Target` became `Plan.Long` and `Plan.Short`, each a `Leg` carrying its
own — because a stop that sits below the entry for a long sits above it for a short, and
sharing one `Bracket` would silently hand one side the other's risk. The engine reads direction off
the position rather than assuming: a short sells to open and buys to cover, its equity is cash less
what the buy-back costs, and it **pays** the dividend a long would have collected, since the lender
of the shares is still entitled to it. The test that matters is the mirror: long PnL and short PnL
over the same bars sum to exactly zero.

**Long wins a bar where both sides fire.** A spec whose two entries fire on the same close has to
resolve to one position, and a fixed order is the only resolution that stays deterministic across
runs. The bracket budget also moved from per-spec to per-side — one stop each is two positions'
worth, not two brackets on one — and `risk_pct` sizing now names the side whose exit is missing its
stop rather than passing validation and skipping every short entry at runtime.

**A new strategy sizes in shares, not in a fraction of equity.** The builder's blank spec opens on
`fixed_qty` at 100. `pct_equity` compounds, which makes a first run's numbers hard to argue with by
hand — the whole property Phase 9 built the engine around — and 100 shares of a 100-lot stock is
the one size that survives lot flooring untouched.

**Borrow cost is the honest gap this opened.** The engine shorts anything, in any size, for free.
Real shorting costs a borrow fee and is sometimes simply unavailable, so a short strategy's numbers
here are its best case rather than its expected one. Listed in Phase 11.

> **Status: done, migrated and checked end to end.** `00013_backtest_reports.sql` is applied,
> `sqlc` regenerated cleanly, and the full check run — gofmt, vet, staticcheck, gosec, the Go suite
> under `-race`, and `svelte-check` — passes on the generated tree. The guess about the cast
> parameter held: `$2::bigint` in the downsampler became `Column2`, the same shape `SearchSymbols`
> already had.
>
> Three things were caught by writing the checks rather than by reading the code, and all three
> would have been invisible until a particular run happened to hit them:
> - **`math.Inf(1)` for profit factor fails `json.Marshal`**, which would have errored the result
>   write for exactly the runs that never lost a trade. The field is `*float64` and the JSON is
>   `null`; a test marshals the metrics and asserts it.
> - **`int32(offset)` wraps negative on a large query parameter**, and Postgres rejects `OFFSET -N`.
>   It saturates at `MaxInt32` through the `int32Of` helper that already existed.
> - **The rate curve's ascending-date check earned its place on the first load**, catching a step
>   entered a year out of position before any Sharpe could compute against a curve that ran
>   backwards.
>
> Carried into later phases, deliberately:
> - **The Selic series runs 2020-08-06 to 2026-08-05 and declares `through: 2026-08-26`.** `through`
>   is deliberately not the last decision's date: a rate holds until the next Copom meeting, so the
>   field asserts how far the curve is *known good*, and a run past it sets `risk_free_stale` rather
>   than quietly carrying a stale rate. It dates the file on purpose. Source of record is BCB SGS
>   series 432.
> - **Splits still do not adjust the share count.** They are detected and counted, not handled.
> - **`skipped_entries` still does not say why.** Phase 9 flagged this as Phase 10's to split once
>   the report had somewhere to show it; the report now shows the count and the three possible
>   causes in one sentence, which is honest but is not the split.
> - **Trade markers are drawn against the loaded page.** A fill outside the visible range has no
>   marker rather than snapping to the nearest bar, and switching symbols clears them, since
>   markers from one ticker on another ticker's candles would be worse than none.

---

## Phase 11 — Beyond a single run

- [x] Parameter sweeps: ranges per input, grid execution across the worker pool, results heatmap
- [x] Walk-forward analysis: rolling in-sample optimize → out-of-sample test
- [x] Portfolio backtests: one strategy across a basket, shared capital. **Shipped working and
      unreachable:** a comma-separated list in the symbol field was the whole interface, and
      nothing said so. Phase 12 gave it a placeholder, permanent help text and a live basket
      summary, and fixed the `max_positions` default it had been quietly overriding
- [x] Strategy sharing / public read-only links
- [x] Borrow cost on short positions, and the hard-to-borrow list that says which shorts were
      actually available
- [x] Corporate actions that are not dividends: a split adjusts the share count, rather than being
      counted in `unpriced_actions` and skipped
- [x] **Futures: contract rollover and back-adjusted continuous series.** Was deferred on data,
      not effort. **Phase 12 found it under `/v2/futures/*`: ~14 months of daily settlement
      history per contract, expired ones included** — see the closed open question below. Built on
      `settlement` rather than OHLC, because that is the only column populated on every bar

*(Four migrations. `00014_carry_and_actions.sql` adds `backtest_trades.borrow_cents` and
`split_cash_cents`; `00015_backtest_baskets.sql` adds `backtest_run_symbols`,
`backtest_runs.max_positions` and `backtest_trades.symbol_id`, backfilling both from the
run's primary symbol; `00016_backtest_sweeps.sql` adds `backtest_sweeps`,
`backtest_sweep_symbols` and the five sweep columns on `backtest_runs`;
`00017_shared_strategies.sql` adds `strategies.share_token`.)*

**Done when:** a sweep says which parameters were best, a walk-forward says whether that
answer survived contact with data it was never tuned on, and a short's numbers include what
it costs to borrow the shares.

### Decisions this phase forced

**A sweep child is an ordinary backtest run, and that is the whole design.** A grid point is a
row in `backtest_runs` with a `sweep_id`, a `params` blob and its own canonical spec — so the
worker pool claims it, the engine runs it, and `/trades`, `/equity` and the whole Phase 10
report open on it unchanged. The alternative was a second execution path for sweeps, which
would have meant a second place for lookahead bias, cost modelling and dividend handling to
drift out of agreement with the first. Nothing in the engine knows a sweep exists.

**Ad-hoc runs jump the queue ahead of sweep children, or a sweep makes the app unusable while
it drains.** `ClaimBacktestRun` orders by `(sweep_id IS NOT NULL), created_at`, and the partial
index was rebuilt on that expression. Two hundred queued points would otherwise sit in front of
the one run you queued to check something, on a box with three workers. The per-user limit was
split the same way: `CountActiveBacktestRuns` now counts only runs with no `sweep_id`, and
sweeps are held to one at a time on their own.

**An axis addresses what it varies with a JSON Pointer, because that is already this codebase's
vocabulary for pointing at part of a spec.** `/inputs/fast/params/period` is the same string a
parse fault would hand back in `pointer`, so an axis the builder rejects and a spec the parser
rejects speak the same language. Only three shapes are reachable — an indicator parameter, the
sizing value, a cost — and the allowlist is what keeps `/version` and `/entry/long` from being
addressable at all.

**Every point of the grid is built and re-parsed before anything is queued.** A range from 5 to
20 by 5 on a period whose ceiling is 15 is one 400 with a pointer, not four runs of which two
fail an hour later for a reason nobody is watching for. The cost is 200 parses at create time,
which is nothing, and the canonical spec each point produces is what its run row stores — so a
sweep child is self-describing in exactly the way an ordinary run already was.

**Walk-forward is two stages coordinated through the queue, not one long job.** Every fold's
in-sample grid is independent of every other fold's, so all `folds × points` of them are queued
at once and the pool parallelises the expensive half. Only the out-of-sample run depends on a
result, and it is created when the last in-sample run of its fold settles — `ReadyWalkForwardFolds`
asks for folds with nothing queued or running, at least one done, and no test run yet. Running
the whole thing inside one worker would have been simpler to read and would have blown the
ten-minute run timeout on any fold count worth having.

**Two workers can finish a fold's last two runs at once, so the database decides who wins.** The
promotion path is racy by construction and a partial unique index on `(sweep_id, fold) WHERE
phase = 'out_of_sample'` makes the loser a no-op rather than a duplicate test run. Catching the
unique violation and returning nil is the whole handler; there is no lock, and there is nothing
to leak if a worker dies between the check and the insert.

**A run that never traded is not scored, which is not the same as scoring zero.** Zero is a
number a losing strategy reaches. A parameter set that never opened a position has not earned
it, and letting the two tie is precisely how a walk-forward promotes a spec that does nothing at
all into the next window. `Metrics.Score` returns `(0, false)` for `trades == 0` and the ranking
skips it. A fold where *no* point traded stays unresolved and the report says "nothing traded"
rather than sitting on "waiting" forever.

**Profit factor ranks at positive infinity and serialises as null, and those are different
questions.** Phase 10 established that `math.Inf(1)` fails `json.Marshal`; a ranking is not JSON,
so a run with no losing trade scores `+Inf` in memory and takes its rightful place at the top.
The sweep's *response* then drops the score to null the same way the metric itself does. Getting
this backwards in either direction is a bug: refusing to score it demotes the best run in the
grid, and marshalling it errors the whole endpoint.

**Cash is the only thing a basket shares, and that is what made the portfolio case a loop rather
than a second engine.** Each symbol became a `book` carrying its own bars, its own indicator
tape, its own corporate actions and its own position; the engine holds the cash, the equity
curve and the trade log. A single-symbol run is a basket of one down the same path, which is why
there is no second code path to keep honest — every Phase 9 and Phase 10 test still exercises
the portfolio engine.

**The timeline is the union of every symbol's bars, so a halted ticker costs nobody a bar.** A
symbol simply has no bar at a stamp it did not trade, and its own index does not advance. That
falls out correctly for a ticker listed late, halted mid-run, or delisted before the end: it
closes out on the last bar *it* has, not on the last bar the basket has, and everyone else's
signal-to-fill spacing is untouched.

**The seat count is checked at the fill, not at the signal.** A basket capped at three positions
can have five symbols fire on the same close, and by the time the fourth fills, another symbol
has taken the last seat. Checking at the signal would hand the strategy knowledge of an
allocation that had not happened yet. `crowded_out` counts what this cost, so a report can say
that raising `max_positions` was worth considering rather than leaving the reader to guess why
half the signals produced nothing.

The cap is therefore a promise about what is *held* at once, not about how many trades a run
takes. A position closed on bar *i* frees its seat on bar *i*, including when the close is the
end-of-run exit — so a symbol that has been waiting can enter on the final bar's open and be
closed out at that same bar's close. That reads as a pointless trade, and it is the only honest
answer: the waiting symbol's intent was formed at the previous close, and declining it because
this bar turned out to be the last one is exactly the lookahead the whole engine is built to
avoid.

**Order within a bar is the basket's own order, for the same reason long beat short in Phase
10.** Several symbols firing on one close have to resolve to a definite set of positions, and a
fixed order is the only resolution that survives being run twice. The basket is offered in the
order it was submitted.

**Borrow cost is a property of the market, not of the strategy, so it is not in the spec.** It
lives in `data/borrow/b3_btb.json`, loaded and validated exactly like `selic.json`: a dated
`through`, a `basis: 252`, a default annual rate, per-ticker overrides, and windows naming what
was hard to borrow when. Putting `borrow_bps` in `Costs` would have made it something a strategy
author chooses, which is the one thing it is not — you pay what BTB charges. The committed file
carries a single documented default and no per-ticker data, because inventing plausible BTB
rates for four tickers would be worse than admitting the curve is flat: the mechanism is real,
the numbers are one placeholder, and `through` dates it.

**Borrow accrues on the previous close for the same reason a dividend is credited there.** The
fee is rent on shares that were already borrowed when the bar opened, so it is charged before
the fill: a short entered on this bar's open pays nothing for it, and one covered later on this
same bar still pays. Both cases fall out of the sequence rather than out of a date comparison.

**The fee is carried as a float and only whole cents move.** A daily 2% rate on a small short is
tens of thousandths of a cent a bar, and rounding each bar independently charges exactly zero,
forever. `borrowOwed` accumulates and settles whole cents as they appear, with the residual
rounded into the trade at exit. This is the difference between modelling borrow and appearing to.

**Splits adjust the share count, and `entryCents` does not move — which is where the one real
bug of this phase was.** The obvious thing is to rewrite the cost basis to `qty × entryPrice`
after the adjustment. It is wrong whenever a grouping leaves a fraction behind: the fraction is
sold for cash, and rewriting the basis to cover only the surviving shares double-counts that
cash as profit. Working the arithmetic for the test case is what caught it — 105 shares grouped
one for ten, bought at R$10.10 and finished at R$112.00, has to come out at R$111.50 of profit
and came out at R$162.00. `entryCents` is what the position cost and a split refunds nothing, so
it stays put; after a fractional grouping `qty × entryPrice` deliberately no longer reproduces
it, and the gap is exactly the basis of the shares that were cashed out.

**A split smaller than 3:2 cannot be told from a large dividend, and the dividend reading wins.**
The classifier reads the adjustment ratio: below `maxImpliedYield` it is a dividend, above it the
implied factor is matched against the ratios real corporate actions actually use. The two regions
meet at 33%, which is why the table starts at 3:2 and no 5:4 term appears — a 5:4 split reads as
a 20% yield, and PETR4 genuinely paid 18.37%. Preferring the dividend there is the right call for
this market, and it is a limitation rather than a bug. A jump matching neither still lands in
`unpriced_actions`, and there is now a test for exactly that case.

**A grouping that leaves less than a whole share settles the position in cash.** This is what B3
actually does with grouping leftovers, and the alternative — carrying a fractional position, or
flooring it to zero and losing the money silently — is a fiction either way. It exits with a
`split` reason, which is a fifth thing an exit can be and is counted as such.

**Brackets move with the split.** A 5% stop quoted against a R$10.10 entry sits at R$9.595, and
the first bar after a 2:1 split trades at half that. Not dividing the stop would take every
bracketed position out at the open of the split bar, at a price that never happened.

**A share token is stored in the clear, unlike a refresh token, and that is deliberate.** A
refresh token authenticates a person; a share token grants read access to one spec. Hashing it
would mean the editor could never show the link again — every "what was that link?" would have
to mint a new one and silently break the old. The endpoint returns the spec and the compiled
plan, and nothing about who wrote it: no user id, no email, no runs, no results.

**A revoked link and a link that never existed answer identically.** `404` either way, with the
same text. Distinguishing them would turn the public endpoint into an oracle for probing which
tokens have ever been real.

**The buy-and-hold benchmark is equal-weight across the basket.** Each symbol gets the same share
of the starting capital at its own second bar and carries the same costs, dividends and splits
as the engine — so the difference between the two curves is still the strategy and nothing else.
A one-symbol basket is that code with the division by one, which is why `TestBuyAndHoldBenchmark
MatchesAHeldPosition` still pins an always-long spec to exactly zero excess.

**The Backtests list stopped showing sweep children.** `ListBacktestRuns` filters
`sweep_id IS NULL`. Two hundred rows of one grid would bury the runs you queued by hand, and the
sweep panel is where those points belong anyway.

> **Status: done, migrated and checked end to end.** The four migrations are applied, `sqlc`
> regenerated cleanly, and the full check run — gofmt, vet, staticcheck, gosec, the Go suite
> under `-race`, and `svelte-check` — passes on the generated tree.
>
> Three things were caught by running the checks rather than by reading the code, and the first
> two had been sitting in the repo since Phase 0 without anything reaching them:
> - **The nullable `uuid` override in `sqlc.yaml` emitted a module path where a type name
>   belongs**, producing `SweepID *github.com/google/uuid.UUID` and a generate that would not
>   parse. sqlc reads `go_type` two ways: the string shorthand `"github.com/google/uuid.UUID"`
>   splits at the last dot into an import and a type, while the object form — the one
>   `pointer: true` forces you into — emits `type:` verbatim and derives no import. Every other
>   object-form override is `time.Time` or `float64`, which are verbatim-valid Go, so the bug
>   needed a nullable `uuid` column to fire and `backtest_runs.sweep_id` is the schema's first.
>   The fix is `import: "github.com/google/uuid"` with `type: "UUID"`.
> - **gosec flagged six `int -> int32` conversions**, all of them provably bounded by validation
>   the analyser cannot see — `max_positions` at 20, the grid at 200 points, folds at 12. They go
>   through the saturating `int32Of` Phase 10 already added for the same reason, rather than a
>   `//nosec`, so the guarantee survives a cap being raised later.
> - **`MaxPositions` caps what is held, not how many trades a run takes**, and the first version
>   of that test asserted the wrong thing. The behaviour is right and is now pinned by a test of
>   its own; the reasoning is three paragraphs up.
>
> The one bug in the engine itself was caught by doing the arithmetic for a test, not by reading
> the code: **rewriting `entryCents` after a split double-counts the cash from a fractional
> grouping as profit.** It is invisible on any split that divides evenly, which is most of them,
> and it would have shown up as a trade whose profit disagreed with the equity curve — the one
> cross-check the report does not draw.
>
> The guesses about generated names all held: `$1::uuid[]` takes `[]uuid.UUID` as a bare
> argument the way `DeactivateSymbols` takes `[]string`; the nullable sweep columns produce
> pointer fields; and `GetSweepRow` and `ListSweepsRow` are convertible, the way
> `ListBacktestRunsRow` and `GetBacktestRunRow` already were.
>
> Carried forward, deliberately:
> - **The borrow curve is one flat default rate.** The loader, the per-ticker overrides and the
>   hard-to-borrow windows are all real and tested; the committed data is a placeholder that says
>   so. Real BTB rates are the next honest improvement, and nothing in the code changes when they
>   land.
> - **Shorts are still sized against cash rather than against margin.** Selling short brings cash
>   in; the engine caps the size by what the cash would buy, which is conservative and is not what
>   a broker actually requires.
> - **A heatmap collapses a third axis to the best point behind each cell.** Three axes are
>   allowed, two are drawn, and the note under the map says so rather than letting the reader
>   assume they are looking at the whole grid.
> - **Walk-forward compounds its out-of-sample folds arithmetically, chaining `1 + r`.** Each fold
>   starts from the full capital rather than from what the previous fold left, so the compounded
>   figure is what the sequence of parameter choices would have returned, not what one account
>   running continuously would have.
> - **A fold whose whole grid finished without trading stays unresolved.** There is no winner to
>   carry forward, so no out-of-sample run is queued and the table says "nothing traded" rather
>   than sitting on "waiting" forever.
> - **Futures are untouched**, for want of a single futures candle.

---

## Phase 12 — Go live: the Pro month

**Goal:** buy one month of Pro, turn four tickers into the whole market and five years of 5m,
then decide whether to keep paying.

Everything before this ran on four free tickers. This phase is mostly *operational* — the code
is already written and timeframe-agnostic. Do it against the production box, not a laptop.

- [x] Upgrade the token. Set `BRAPI_TOKEN`; nothing else changes
- [x] Write out the current IBOV, IBXX and SMLL compositions into `data/indexes/`, commit them, and
      run the sync to set `tracked = true` on the union. No judgment call, no screen to defend.
      ~~Expect ~200 tickers~~ **150.** Pulled from B3's own `GetPortfolioDay` rather than
      transcribed: IBOV 77, IBXX 98, SMLL 107, as of 2026-08-26
- [x] Sanity-check the union size before spending anything. Every symbol is ~103k rows and a
      permanent share of the daily sync budget; if the number comes back at 400 rather than 200,
      something double-counted the overlap. **It came back under, not over:** IBOV is a complete
      subset of IBXX (77 of 77), and 55 of SMLL's 107 are also in IBXX, so `98 + (107 - 55) = 150`.
      The safe direction, and it cut the backfill by a quarter
- [x] Dry-run the backfill: no writes, just count the requests it *would* make and print the total
      against the 500k quota. Run this before the real one, every time. Note the printed
      percentage is against Free's 15,000, not Pro's 500,000
- [x] Determine the real chunk size empirically — how much 5m history brapi returns per request.
      The budget above assumed one month per request; measure it, don't assume it. **Floor: three
      months.** Phase 2 measured that a shorter window returns a different and wrong 5m series, so
      a one-month chunk is off the table regardless of what the quota arithmetic prefers. Confirm
      the defect's shape on Pro before the full run: fetch one symbol at two window sizes and
      reconcile both folds against the stored `1d`.
      **Measured on day one — record the figures here:** the depth a single `range` request
      actually returned, and whether the `<3mo` defect survived on Pro at real depth. The code
      issues one range request per symbol either way (`IntradayRange`), so the budget question is
      settled; what is worth keeping is the depth number, because it is what a future re-backfill
      would be sized against
- [x] Backfill 1d first (cheap, works on any tier, and gives every symbol a usable chart
      immediately), then 5m
- [x] Run it resumable and rate-limited, in the background, in the container. It will take hours.
      `docker compose run -d --rm app backfill --universe`
- [x] Verify: row counts per symbol against expected session counts, gap report clean, spot-check
      a known split (e.g. a 1:2) to confirm adjustment landed
- [x] `pg_dump` immediately afterwards and get it off the box. **This dump is the expensive
      artifact** — it cost a month of Pro. Losing it means paying again
- [x] Then decide: stay on Pro (~R$116/mo, 5m stays current) or drop to Free (daily keeps
      updating, 5m freezes at the backfill date, historical backtests unaffected).
      **Staying on Pro.** `INGEST_ENABLED=true`, `INGEST_INTRADAY=true`: ~150 requests per
      timeframe per session, ~6,300/month against 500,000. Dropping to Free later is
      `INGEST_INTRADAY=false` and nothing else
- [x] **Futures, unplanned.** The Pro token also answered Phase 11's blocked item — see the closed
      open question below. `sync-futures` loads ~14 months of daily settlement history for
      WIN/IND/WDO/DOL, and the continuous series is wired through to the engine and the chart

**Done when:** the universe is loaded, verified, dumped off-box, and the daily sync runs inside
the Free quota.

**Done.** 150 tracked symbols, backfilled and verified, dumped off-box, and the in-process
scheduler keeping both timeframes current on Pro. The phase also cost more code than "mostly
operational" predicted: a ticker-classification fix, the scheduler itself, and the whole futures
path that the token made possible for the first time.

### Decisions this phase forced

**The scheduler is in-process, because the alternative puts the cadence outside the artefact that
gets deployed.** A host crontab calling `docker compose run` works, but it lives on the box rather
than in the image, so a rebuilt server is one forgotten `crontab -e` away from a universe that
silently stops updating — and nothing in the app would know. `INGEST_ENABLED` starts a goroutine
next to the backtest worker pool, which means the cadence ships, restarts, and is visible in the
same logs as everything else. It defaults to **off**: a dev box pulling this change must not
quietly begin calling brapi.

**It fires off the calendar, not off a clock.** The trigger is `session.Close + INGEST_CLOSE_DELAY`
resolved through the same `Calendar` the resampler and gap report use, so holidays and B3's short
sessions are handled by construction rather than by a cron expression that has never heard of
Carnaval. A poll every five minutes asks the calendar whether the moment has passed; there is no
wall-clock schedule to drift out of agreement with the trading day.

**"Already synced today" is a database question, not a variable.** `restart: unless-stopped` plus
an in-memory flag would re-run the whole sync on every container restart after the close.
`LatestSyncRunAt` reads the newest `ok`/`empty` row in `ingest_runs` for the timeframe and compares
it against today's trigger, so the guard survives restarts and crash loops — and it reuses the
ledger the ingester was already writing rather than inventing a second source of truth. A backfill
earlier the same morning does not satisfy it, because the comparison is against the close, not
against the date.

**The scheduled path and the CLI are the same call.** The goroutine builds `SyncOptions` and calls
`Ingester.Sync`, which is what `sync-candles` does — so trap 11's rule that 5m is always a `3mo`
range request trimmed client-side is obeyed automatically, and can't drift in one path only.
`TrackedSymbols` moved onto the ingester for the same reason: the universe was defined in
`resolveSymbols` in `main`, and the scheduler would have been a second place to get the
futures filter right.

**Futures got their own tables, because they are not candles.** `candles` is `NOT NULL CHECK
(open > 0)` with `CHECK (low <= open)`, and brapi never serves an `open` for a futures contract —
not on a thin far-dated one, not on the eighty heavily-traded front-month sessions of `WINZ25`.
Storing futures there meant either fabricating every open or dropping the constraint for equities
too, and the second option trades away a real guarantee on 20M rows to accommodate a series that
is not OHLC in the first place. `futures_contracts` and `futures_quotes` keep `settlement` `NOT
NULL` — it is populated on 100% of bars — and leave the traded fields nullable, which is what the
data actually is.

**The continuous series is derived on read, like the resampler.** Nothing stores a back-adjusted
series. `BuildContinuous` picks the front contract per session by nearest unexpired
`expiration`, records a roll when that selection changes, and walks backwards applying the
cumulative settlement gap. The roll day itself belongs to the *new* contract and keeps the smaller
offset — the adjustment applies to bars strictly before it, which is the off-by-one this method
invites. Storing the output would mean rewriting every historical row at every roll, and 15m/30m/1h
already established that derived-on-read is this codebase's answer to that.

**The roll gap is measured on the last session where both contracts exist, not on the roll day.**
The first implementation looked for the outgoing and incoming contract in the same day's quotes and
subtracted. On the roll day the outgoing contract has already expired and has no bar at all, so it
found nothing and returned a gap of zero — for every roll, silently, which makes back-adjustment a
no-op and yields a continuous series that is visibly smooth and completely wrong. `rollGap` now
walks backwards from the roll to the most recent session carrying a settlement for *both* symbols,
and reports `Measured` so an unmeasurable roll is distinguishable from a genuinely zero one.
Verified against the store: all eight WIN rolls measure between 1,254 and 4,553 points, 0.74% to
2.46%, which is the carry spread a bimonthly Ibovespa contract should show.

**Futures fill at the next settlement, because there is no open to fill at.** Trap 1 makes
lookahead structural by signalling on the close of *i* and filling at the open of *i+1*. That rule
has no futures equivalent, so the futures path signals on settlement of *i* and fills at settlement
of *i+1*: the one-bar delay is preserved, and the price used is the only one guaranteed to exist.
Filling at the traded close instead would silently vary the basis bar to bar and vanish entirely on
thin contracts — `INDV26` traded 20 of 250 sessions.

**A futures root is a symbol, so nothing downstream had to learn what a future is.** WIN, IND,
WDO and DOL were already rows in `symbols` from `contracts.json`. The continuous series is loaded
through `CandleService`, which means the engine, the candles endpoint, the indicator pipeline, the
chart and the symbol search all reached it without a futures-shaped branch in any of them. The
routing is one `Kind` check in three places — the runner, the candles handler, and the prime
lookup — and everything past those points is the equity path unchanged.

**Settlement bars are emitted as `O = H = L = C`, which makes the fill rule fall out for free.**
The engine signals on the close of *i* and fills at the open of *i+1* (trap 1). Give it a bar
whose open and close are both that session's settlement and "fill at the open of *i+1*" *is*
"fill at the settlement of *i+1*" — the rule chosen for futures, implemented by construction
rather than by a second code path that could drift. The degenerate range is also honest: one price
is genuinely all that is known for the session, so a stop or limit can only trigger on settlement
crossing it, which is the conservative reading. `barOf` needed no change; `closeOf` already
existed for end-of-run exits and proved the shape works.

**The chart draws futures as a line, because a candlestick of `O = H = L = C` is a row of dashes.**
Selecting a futures root switches the chart to line mode and pins the timeframe to `1d`, and the
header carries a `continuous · back-adjusted` tag. Without it the series looks like an ordinary
price history, and back-adjusted futures prices are *not* prices anyone traded — the further back
you read, the further they sit from what printed. The tag is the only thing standing between that
series and a reader who assumes otherwise.

**The regression tests were written from the bugs, not from the design.** `BuildContinuous`
shipped with two defects that produced smooth, believable, wrong curves: a roll gap measured on
the roll day, where the outgoing contract no longer exists, and a back-adjustment that gave the
roll day itself the larger offset. Neither raised an error and neither looked wrong on a chart;
both were found by querying the stored data in SQL and comparing against what the arithmetic
should have produced. The tests now pin that comparison — the load-bearing one asserts that a
day-over-day move in the adjusted series equals the move of the contract actually held that day,
across rolls included, which is the property back-adjustment exists to provide and which fails
under either bug. Known-good values from the real store are the fixture's reference: WIN rolls 8
times over the window with gaps of 1,254 to 4,553 points.

**A feature with no affordance is not shipped.** Baskets worked end to end — API, engine, panel —
and went unused because the only hint that a symbol field took more than one symbol appeared
*after* you had already typed a comma: the label pluralised, and the "held at once" control
un-hid. Both are feedback for someone who already knew. What was missing was a placeholder and one
sentence of help, which is a smaller change than any of the machinery behind it. Worth checking
the same way elsewhere: every control whose existence is conditional on the state it configures is
invisible to the person who needs it most.

**The daily futures pass reads the term structure, not the contracts.** `sync-futures` walks
every contract to build history — 51 list pages plus one request each — which is right once and
absurd daily. `/v2/futures/term-structure?asset=WIN` returns every live expiration for a root in
a single call with the same fields, so the tail is **one request per root**: four a day against
113. Expired contracts never move again, so they belong to the backfill and are simply absent
here. `SyncTail` reads the roots already in `futures_contracts` and spends nothing when none have
been backfilled, which is why `INGEST_FUTURES` can default on without costing a deployment that
never wanted futures.

**The tail's repeat guard is in memory, and that asymmetry is deliberate.** The candle sync reads
`ingest_runs` because a duplicate costs ~300 requests; the futures tail costs four, so a mark on
the scheduler is proportionate and the worst a restart can do is re-upsert rows that do not
change. Guarding it *somehow* was not optional though — the scheduler polls every five minutes,
so an unguarded tail would have re-run every five minutes from the close until midnight.

**The help pages document the traps, not the buttons.** A tour of which field does what would
have been shorter and useless: the controls are already labelled. What a user cannot see is that a
signal fills at the *next* bar's open, that a blank profit factor means too few trades rather than
no losses, that the in-sample column of a walk-forward is the one to distrust, or that the universe
is survivorship-biased by an amount nobody can measure. So the four pages — strategies, backtests,
sweeps, and a fourth on the data itself — are mostly this file's traps section rewritten for
someone who did not build the thing. The data page exists because those caveats belong to no
panel: they are true of every run, and there was nowhere in the UI that said so.

**Cost on Pro is not the constraint, and never was.** 150 symbols × 2 timeframes × ~21 sessions is
~6,300 requests a month against Pro's 500,000 — and still inside Free's 15,000. Decision 5 said
quota pressure is cadence rather than history; at one request per symbol per timeframe per day,
even the intraday half is affordable. What Pro actually buys after the backfill is a 5m head that
keeps extending, which is a data question, not a budget one.

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
- [x] **Both are ARM**, which makes the `linux/arm64` build load-bearing rather than a nicety
- [x] Multi-arch image build (`linux/amd64`, `linux/arm64`) via buildx in CI, pushed to GHCR on tag
- [x] `docker-compose.prod.yml`: adds **Caddy** as the only published service, reverse-proxying the
      app. Automatic Let's Encrypt TLS from a two-line Caddyfile — the app still serves the SPA
      itself, Caddy only terminates HTTPS
- [x] `restart: unless-stopped` on every service; memory limits so a runaway backtest can't OOM
      Postgres
- [x] Postgres tuning for a small box: `shared_buffers`, `work_mem`, `effective_cache_size` set
      explicitly. Defaults assume far more RAM than 4 GB
- [x] **Backups.** Nightly `pg_dump | gzip` to object storage (Cloudflare R2 / Backblaze B2, both
      effectively free at this size). Test a restore *once*, for real. The candle store is ~4 GB
      that cost a month of Pro to acquire
- [x] `HEALTHCHECK` hitting `/healthz` — a distroless image has no `curl`, so this is an
      `/alvo healthcheck` subcommand hitting itself
- [x] Graceful shutdown on SIGTERM: stop accepting, drain in-flight requests, mark running
      backtests back to `queued`. Docker sends SIGTERM and waits 10s — an orphaned `running` row
      that no worker owns is a job that never finishes
- [x] Per-IP and per-user rate limiting on the API
- [x] Response caching for hot candle ranges
- [x] Metrics + `pprof` behind auth
- [x] Backtest timeout + memory ceiling per run
- [ ] Pin base images by digest once the build is stable
- [ ] **Move the candle store to the box.** Not in the original list, and the one step that has no
      undo: the store is in this machine's `alvo_pgdata` volume. `pg_dump -Fc` out, `pg_restore`
      into an *empty* database on the box, and only then start the app. The app runs goose on
      startup, so an app that boots first migrates the schema and the restore then collides with
      tables that already exist. Restored first, the dump carries `goose_db_version` at 18 and
      goose correctly does nothing
- [ ] Prove a restore from object storage, not from the disk the dump was written on

**Done when:** `docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d` on a fresh
box serves the app over HTTPS, and a restore from last night's dump has been proven to work.

### Decisions this phase forced

**The candle store is 138 MB, not 4 GB, and that reframes the phase.** Phase 12's numbers were
never written down; read back off the database they are 187,528 daily rows spanning 2021-08-20 to
2026-08-26, and 525,397 5m rows spanning **2026-06-22 to 2026-08-26 — 48 sessions.** Whatever Pro's
intraday retention is, the backfill came back with two and a half months, not five years. Backups
at this size are trivial rather than the thing the deploy is designed around, and the decision the
plan defers to "whether to keep paying" is narrower than it looked: Pro is buying a universe that
stays current and a benchmark, not intraday depth.

**5m is a rolling window, so ingestion being off is data loss, not staleness.** Trap 11 already
forbids the narrow tail refresh that would backfill a gap. The consequence it doesn't state is
that any day the scheduler isn't running is a permanent hole in 5m — which is why
`INGEST_ENABLED=true` is part of the cutover and not a follow-up.

**Docker's stop grace was shorter than the shutdown path.** `shutdownTimeout` is 15s and Docker's
default is 10s, so SIGKILL landed five seconds into the drain, inside `runner.Wait()` — producing
exactly the orphaned `running` row this phase's checklist exists to prevent. The requeue path was
correct the whole time and never got reached. `stop_grace_period: 30s`. A correct recovery path
behind a deadline that fires first is indistinguishable from no recovery path.

**`TRUST_PROXY` is explicit, and defaults to false.** Behind Caddy every request arrives from
Caddy's address, so a per-IP limiter keyed on `RemoteAddr` gives the whole internet one shared
bucket and the first abusive client locks out everyone. Keyed on `X-Forwarded-For` with nothing in
front, any caller picks their own bucket and the limiter is decorative. Neither failure shows up
in testing. The Caddyfile *overwrites* `X-Forwarded-For` with `{remote_host}` rather than appending,
so the header cannot be spoofed through the proxy, and the config flag is what stops the dev
deployment trusting a header nobody is setting.

**`GOMEMLIMIT` is the memory ceiling; the container limit alone is not.** Go reads the host's RAM
and CPU count, not the cgroup's. On a 12 GB box under a 1024m cap the GC grows the heap past the
limit and the app is OOM-killed, while `Workers()` sizes the backtest pool to cores the container
may not use. `MaxBars` and `runTimeout` bound a single run; these two bound the aggregate.

**Stats are a JSON endpoint, not a Prometheus exporter.** On one box with no scraper, an exporter
is a dependency with no consumer. `/api/v1/admin/stats` reports pool counts, queue depth, heap and
the live `GOMEMLIMIT` — enough to answer "is it wedged", which is the only question this deployment
gets asked. It also confirms the memory limit actually took, which is otherwise invisible until
something dies.

**Both admin routes sit behind `requireAuth`, which is any user, not an admin.** With three rows in
`users` that is a deliberate acceptance, not an oversight. It becomes a `role` column and a
`requireAdmin` wrapper the first time someone else registers.

**The backup script refuses to upload a dump under 1 MB.** `set -o pipefail` catches a `pg_dump`
that dies mid-stream, but a dump that succeeds against an empty or half-restored database does not
fail — it uploads, looks like a healthy backup, and ages out the good ones behind it. A size floor
is the cheap check that a backup nobody reads is still a backup.

**Digest pinning is last, deliberately.** Pinning before the ARM build is proven means debugging
pinned images. `postgres:17-alpine` is the one that matters: an unpinned tag that rolls to a new
major refuses to start against a PG17 data directory, and the failure arrives during a routine
`docker compose pull` rather than at a moment anyone chose.

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
3. **Corporate actions.** Unadjusted prices make every split a fake crash. Handled in Phase 11 by
   reading the adjustment ratio: a jump small enough to be a dividend is credited as cash, one
   matching a ratio a real split uses adjusts the share count, and anything else is counted in
   `unpriced_actions` and left alone. The seam is at 33% — below it the dividend reading wins, so
   a split smaller than 3:2 is not detectable and never will be from this data.
4. **Intrabar ambiguity.** OHLC cannot order events inside a bar. Pick pessimistic, count it, report it.
5. **Session-aligned buckets.** Resampling on wall-clock boundaries misaligns every intraday bar
   against the 10:00 open.
6. **Warmup leakage.** Indicators emitting values before they're seeded generate phantom early trades.
7. **Float money.** Equity curves that drift a few centavos per trade over 5,000 trades are wrong
   by a visible amount.
8. **Overfitting.** Phase 11's parameter sweeps make it trivially easy to find a strategy that fits
   noise. Walk-forward shipped in the same phase for exactly that reason, and the in-sample score
   on a heatmap is the number to distrust: the one worth acting on is the out-of-sample column of
   the fold table, which was produced by parameters chosen without ever seeing those days.
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
13. **A short's numbers are still a best case.** Borrow accrues from Phase 11's committed curve,
    but that curve is one flat default rate until real BTB data lands, and the hard-to-borrow list
    is empty. A short on a name that was genuinely expensive or simply unavailable will look
    better here than it was.
14. **Development ran on four large caps.** PETR4, VALE3, ITUB4 and MGLU3 are liquid, gap rarely,
    and never halt. The first backfill of illiquid tickers will surface zero-volume bars, missing
    sessions and stale prices that four blue chips never exercised. Expect Phase 12 to find bugs
    in code that looked finished.
15. **A B3 ticker root can contain a digit.** `ClassifyTicker` originally spelled the root as
    `[A-Z]{4}`, which is true of every ticker the four development names exercised and false of
    `B3SA3` — B3's own listing, and 3.3% of the IBOV carteira loaded in Phase 12. The root is now
    `[A-Z][A-Z0-9]{3}`: still anchored to a leading letter, so a numeric-leading string is
    rejected rather than read as a stock. This is the first bug trap 14 predicted, and it surfaced
    before a single request was spent because `sync-symbols` refuses the whole admission list on
    one unclassifiable ticker instead of silently dropping it. Keep that behaviour — a symbol
    missing from the universe is invisible, and an index constituent that never gets ingested is
    a backtest quietly running on 149 names while reporting 150.
16. **`IND` and `DOL` are live US tickers on the equities endpoint.** Futures belong to
    `/v2/futures/*`, but nothing stops the ordinary `/quote/{ticker}` path being handed a root —
    and it does not fail. `IND` returns the Xtrackers Nifty 500 India ETF and `DOL` the WisdomTree
    True Developed International Fund, both with real prices that would validate and store
    cleanly. Nothing fetches them today because three separate guards skip `KindFuture`:
    enrichment, the stale-symbol scan, and `TrackedSymbols`. Those guards are load-bearing for
    correctness, not tidiness — remove any one and a symbol labelled *Mini Ibovespa Futuro*
    silently fills with a US ETF's candles. Futures ingestion, when it comes, addresses concrete
    contracts on the futures endpoint; a bare root must never reach `/quote`.
17. **`/v2/futures/list` hides expired contracts, and the omission is silent.** The default listing
    returns only contracts still trading — 1,758 of them, 69 across WIN/IND/WDO/DOL.
    `includeExpired=true` returns 5,026, and 113 for those roots inside the history window. The
    difference is not cosmetic: a continuous series assembled from currently-listed contracts only
    picks the same nearest-unexpired contract for every session in the window, so it back-adjusts
    across **zero rolls** and looks entirely reasonable while being a single contract's settlement
    curve wearing a continuous series' name. `sync-futures` defaults `--include-expired` to true
    and the report says which mode ran. The tell that something is wrong is a roll count of zero
    over a window that spans a quarter.
18. **A futures roll has no overlap on the roll day itself.** The outgoing contract's last bar is
    its expiration day; the next session it does not exist. Anything comparing the two contracts —
    a roll gap, a liquidity crossover, a spread — has to look at the session *before* the roll,
    where both still settle. Measured on the roll day it finds one contract and silently produces
    a zero.
19. **A default that the API and the UI disagree about is a silent wrong answer.** `max_positions`
    reads `0` as "hold every name in the basket", which is what someone typing a list of tickers
    means. The panel defaulted the field to `1` and always sent it, so a three-name basket ran as
    a one-at-a-time rotation — a different strategy entirely, with nothing on screen saying so and
    no error to notice. The field now follows the basket until it is explicitly changed. The
    general shape: a default that exists in two places will eventually only be right in one of
    them, and the failure is a plausible number rather than a crash.

---

## Deferred to post-launch

- **Real-time / streaming.** Explicitly out of scope for v1. The groundwork is already here — the
  streaming `Indicator` interface takes bars one at a time, and SSE over the candle service is a
  small addition — but shipping it means intraday polling cadence, which means permanent Pro.
  Revisit once the thing is deployed and actually used.
- **Timeframes below 5m.** Not a resolution this project serves. Reopening it means a 5× row
  count and a re-backfill.
- **Futures rollover and back-adjusted continuous series.** The only Phase 11 item left. It waits
  on futures candles existing at all, not on the code — `data/contracts.json` already names the
  four roots.
- **Real BTB borrow rates.** Phase 11 built the curve, the per-ticker overrides and the
  hard-to-borrow windows; `data/borrow/b3_btb.json` carries one placeholder default until there
  is a source to load. Nothing in the code changes when there is.

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
  ~~**Measured on day one of the Pro month; the figures were not written down.**~~ **Recovered from
  the store in Phase 13, and the answer is short: 48 sessions.** 5m spans 2026-06-22 to 2026-08-26,
  525,397 rows across 151 symbols; daily spans 2021-08-20 to 2026-08-26, 187,528 rows. Pro bought
  five years of daily and roughly two and a half months of intraday. What is still unseparated is
  *why*: either intraday retention on Pro is the same ~60 days as Free, or `backfill` asked for a
  `3mo` range token and got precisely that. One request settles it — fetch a single symbol at
  `range=5y` on 5m and see whether anything predating 2026-06-22 comes back — and it can only be
  asked while the token is live. Either way the practical consequence is fixed: **5m is a rolling
  window, not an archive**, so it only extends as long as the scheduler keeps running.
- ~~**Futures coverage.**~~ **Answered in Phase 12: available, with a settlement series rather
  than an OHLC one.** Futures live in their own namespace, `/api/v2/futures/*`. The equities
  `/quote/{ticker}` and `/available` endpoints know nothing about it and answer *Nenhum resultado
  encontrado* for `WINV26` — a routing answer that is easy to misread as a coverage answer.
  - `/v2/futures/list` pages 1,758 contracts (100/page ceiling) with full metadata. Every root
    `contracts.json` names is present, and the `contractMultiplier` agrees with the seeded
    `point_value`: WIN 0.2, IND 1, WDO 10, DOL 50.
  - `/v2/futures/quote?symbols=` takes a comma-separated list, one settled bar per contract for
    the previous session. Good for the daily tail, useless for history.
  - `/v2/futures/historical?symbol=` — **singular `symbol`**, and passing `symbols` fails
    validation in a way that looks like a missing route. Returns `future.history`, newest first.
    It defaults to a one-year window; **`startDate` extends it**, down to a hard floor of
    **2025-06-10** that no parameter reaches past. `range`, `limit` and `from`/`to` are ignored.
  - **Expired contracts keep their history.** `WINZ25`, `WINQ26` and `WINM26` all return complete
    series after expiry, which is what makes a continuous series buildable *retroactively* rather
    than only forward.
  - **`settlement` is populated on 100% of bars, on every contract.** Traded fields — high, low,
    close, average, volume, trades — are null on sessions where that contract did not trade, and
    the density tracks liquidity: `WINZ25` traded 133 of 135 sessions, the far-dated `INDV26`
    only 20 of 250.
  - **`open` is null on every bar of every contract, always** — including the 80 heavily-traded
    front-month sessions of `WINZ25`. `referencePrice` likewise. This is not a gap to fill in; the
    field is never served.

  - **There is no intraday futures data, and it is not a parameter away.** `/v2/futures/historical`
    ignores `interval`, `granularity`, `resolution`, `timeframe` and `period` alike — the response
    is the same 250 daily bars stamped at midnight. No `intraday`, `candles`, `chart`, `bars` or
    `ticks` route exists under the namespace. And the equities `/quote/{ticker}` path, which does
    serve 5m for stocks, returns `NOT_FOUND` for a confirmed-real contract symbol: the two
    namespaces do not overlap. Futures are daily on brapi, full stop, so `LoadContinuous` rejects
    any timeframe but `1d` and the chart pins futures to it.

  So Phase 11's item is unblocked, on ~14 months of daily settlement history. The continuous
  series must be built on `settlement`, which is the complete column and is in any case the
  conventional basis for back-adjustment. It cannot be built on OHLC: there is no `open` at all,
  and the other four are sparse exactly where liquidity is thin.
- **The futures contract multiplier reaches storage but not the engine.** `futures_contracts`
  records it and `ContinuousSeries` carries it, but position sizing reads `lot_size` off the
  `symbols` row, so a WIN position is sized as though one unit is worth one point when it is worth
  R$0.20. A futures run therefore produces a plausible equity curve whose P&L magnitude is wrong
  by the multiplier — 5x for WIN, 10x for WDO, 50x for DOL. It is worse in a mixed basket, where
  the error is not uniform and so the allocation between futures and equities is wrong rather than
  just the totals. The open part is where the multiplier belongs: folded into `Symbol.LotSize` at
  load, carried as a distinct field through sizing, or applied at the P&L boundary. Until it is
  decided, futures numbers are directionally right and absolutely wrong.
- **Backfilling admissions made after the Pro month.** A ticker promoted into an index later can
  only be given 5m history while a Pro token is live. On Free it gets daily from admission
  onward and nothing intraday, ever. If the plan is to drop to Free, either accept a universe
  where newer members have shallower intraday history, or batch admissions and buy a second Pro
  month once it's worth it.
