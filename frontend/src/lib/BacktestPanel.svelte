<script lang="ts">
  import { onDestroy, onMount, untrack } from 'svelte';

  import BacktestReport from './BacktestReport.svelte';
  import { describe, HttpError, TIMEFRAMES, type Timeframe } from './api';
  import {
    fetchCurve,
    fetchRun,
    fetchRuns,
    fetchTrades,
    launchBacktest,
    settled,
    type Curve,
    type Run,
    type Trade,
  } from './backtest';
  import { formatCents, formatSignedPct } from './format';
  import { fetchStrategies, type SavedStrategy } from './strategy';
  import type { User } from './session';

  const INDEX_TICKER = '^BVSP';
  const POLL_MS = 1500;

  type Props = {
    symbol: string;
    timeframe: Timeframe;
    user: User | null;
    onTrades: (trades: Trade[], symbols: string[]) => void;
    onClose: () => void;
    onHelp?: () => void;
  };

  let { symbol, timeframe, user, onTrades, onClose, onHelp }: Props = $props();

  let strategies = $state<SavedStrategy[]>([]);
  let runs = $state<Run[]>([]);
  let active = $state<Run | null>(null);
  let curve = $state<Curve | null>(null);
  let trades = $state<Trade[]>([]);

  // The form seeds from whatever the chart is showing and then goes its own way: tracking
  // the props would overwrite what you typed the moment the chart behind the panel changed.
  let strategyId = $state('');
  let runSymbol = $state(untrack(() => symbol));
  let runTimeframe = $state<Timeframe>(untrack(() => timeframe));
  let start = $state(defaultStart());
  let end = $state(today());
  let capital = $state(100000);
  let heldOverride = $state<number | null>(null);

  let error = $state<string | null>(null);
  let busy = $state(false);
  let loading = $state(false);
  let timer: ReturnType<typeof setTimeout> | null = null;

  // A comma separated list is a basket; one name is the single-symbol run every phase
  // before this one produced, and the two go down the same path.
  const basket = $derived(
    runSymbol
      .split(',')
      .map((ticker) => ticker.trim().toUpperCase())
      .filter((ticker) => ticker !== ''),
  );

  // The API reads max_positions = 0 as "hold every name", which is what someone typing a
  // list of tickers means. Until the field is touched, follow the basket instead of pinning
  // it to one and silently turning a portfolio into a one-at-a-time rotation.
  const maxPositions = $derived(
    heldOverride === null ? basket.length : Math.min(Math.max(heldOverride, 1), basket.length),
  );

  const canRun = $derived(
    user !== null && strategyId !== '' && basket.length > 0 && !busy && start !== '' && end !== '',
  );

  function today(): string {
    return new Date().toISOString().slice(0, 10);
  }

  function defaultStart(): string {
    const at = new Date();
    at.setFullYear(at.getFullYear() - 2);
    return at.toISOString().slice(0, 10);
  }

  function stopPolling() {
    if (timer !== null) {
      clearTimeout(timer);
      timer = null;
    }
  }

  async function loadStrategies() {
    if (!user) {
      strategies = [];
      return;
    }
    try {
      strategies = await fetchStrategies();
      if (strategyId === '' && strategies.length > 0) {
        strategyId = strategies[0].id;
      }
    } catch (cause) {
      error = describe(cause);
    }
  }

  async function loadRuns() {
    if (!user) {
      runs = [];
      return;
    }
    try {
      runs = await fetchRuns();
    } catch (cause) {
      error = describe(cause);
    }
  }

  async function loadReport(run: Run) {
    loading = true;
    try {
      const [points, filled] = await Promise.all([fetchCurve(run.id), fetchTrades(run.id)]);
      curve = points;
      trades = filled;
      onTrades(filled, run.symbols ?? [run.symbol]);
    } catch (cause) {
      error = describe(cause);
      curve = null;
      trades = [];
    } finally {
      loading = false;
    }
  }

  async function select(run: Run) {
    stopPolling();
    error = null;
    active = run;
    curve = null;
    trades = [];

    if (run.status === 'done') {
      await loadReport(run);
      return;
    }
    if (run.status === 'error') {
      return;
    }
    poll(run.id);
  }

  function poll(id: string) {
    stopPolling();
    timer = setTimeout(() => void tick(id), POLL_MS);
  }

  async function tick(id: string) {
    try {
      const run = await fetchRun(id);
      if (active?.id !== id) {
        return;
      }

      active = run;
      runs = runs.map((row) => (row.id === run.id ? run : row));

      if (!settled(run)) {
        poll(id);
        return;
      }
      if (run.status === 'done') {
        await loadReport(run);
      }
    } catch (cause) {
      error = describe(cause);
    }
  }

  async function launch() {
    if (!canRun) {
      return;
    }

    busy = true;
    error = null;

    try {
      const run = await launchBacktest({
        strategy_id: strategyId,
        symbols: basket,
        timeframe: runTimeframe,
        start,
        end,
        capital_cents: Math.round(capital * 100),
        max_positions: maxPositions,
      });

      runs = [run, ...runs];
      await select(run);
    } catch (cause) {
      error =
        cause instanceof HttpError && cause.status === 401
          ? 'Sign in to run a backtest.'
          : describe(cause);
    } finally {
      busy = false;
    }
  }

  function summary(run: Run): string {
    if (run.status === 'error') {
      return 'failed';
    }
    if (!run.metrics) {
      return run.status;
    }
    return formatSignedPct(run.metrics.return_pct);
  }

  function onKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      event.stopPropagation();
      onClose();
    }
  }

  onMount(() => {
    void loadStrategies();
    void loadRuns();
  });

  onDestroy(stopPolling);
</script>

<svelte:window onkeydown={onKeydown} />

<div class="backdrop">
  <button type="button" class="scrim" aria-label="Close" onclick={onClose}></button>

  <div class="dialog" role="dialog" aria-modal="true" aria-label="Backtests">
    <header>
      <h2>
        Backtests
        {#if onHelp}
          <button type="button" class="help" onclick={onHelp} title="How backtests work">?</button>
        {/if}
      </h2>
      <button type="button" class="close" onclick={onClose} aria-label="Close">×</button>
    </header>

    {#if !user}
      <p class="hint">Sign in to run a backtest and keep its report.</p>
    {:else}
      <form
        class="launch"
        onsubmit={(event) => {
          event.preventDefault();
          void launch();
        }}
      >
        <label>
          <span>Strategy</span>
          <select bind:value={strategyId}>
            {#each strategies as saved (saved.id)}
              <option value={saved.id}>{saved.name}</option>
            {:else}
              <option value="">No strategies saved yet</option>
            {/each}
          </select>
        </label>

        <label class="wide">
          <span>{basket.length > 1 ? `Symbols · ${basket.length}` : 'Symbol'}</span>
          <input
            bind:value={runSymbol}
            spellcheck="false"
            autocapitalize="characters"
            placeholder="PETR4, VALE3, ITUB4"
          />
          <small>
            {#if basket.length > 1}
              Basket of {basket.length} on shared capital, holding {maxPositions} at once.
            {:else}
              One ticker, or several separated by commas to backtest a basket on shared capital.
            {/if}
          </small>
        </label>

        <label class="short">
          <span>Timeframe</span>
          <select bind:value={runTimeframe}>
            {#each TIMEFRAMES as tf (tf)}
              <option value={tf}>{tf}</option>
            {/each}
          </select>
        </label>

        <label class="short">
          <span>From</span>
          <input type="date" bind:value={start} />
        </label>

        <label class="short">
          <span>To</span>
          <input type="date" bind:value={end} />
        </label>

        <label class="short">
          <span>Capital</span>
          <input type="number" min="100" step="100" bind:value={capital} />
        </label>

        {#if basket.length > 1}
          <label class="short">
            <span>Held at once</span>
            <input
              type="number"
              min="1"
              max={basket.length}
              value={maxPositions}
              oninput={(event) => (heldOverride = Number(event.currentTarget.value))}
            />
          </label>
        {/if}

        <button type="submit" class="run" disabled={!canRun}>
          {busy ? 'Queueing…' : 'Run'}
        </button>
      </form>

      {#if error}
        <p class="error">{error}</p>
      {/if}

      <div class="body">
        <ol class="runs">
          {#each runs as run (run.id)}
            <li>
              <button
                type="button"
                class:on={run.id === active?.id}
                onclick={() => void select(run)}
              >
                <span class="who">{(run.symbols ?? [run.symbol]).join(', ')} · {run.timeframe}</span>
                <span class="when">{run.start} → {run.end}</span>
                <span
                  class="how"
                  class:bad={run.status === 'error' || (run.metrics?.return_pct ?? 0) < 0}
                  class:live={!settled(run)}
                >
                  {summary(run)}
                </span>
                <span class="cash">{formatCents(run.capital_cents)}</span>
              </button>
            </li>
          {:else}
            <li class="hint">No runs yet.</li>
          {/each}
        </ol>

        <div class="detail">
          {#if !active}
            <p class="hint">Pick a run, or start one above.</p>
          {:else if !settled(active)}
            <p class="hint">
              {active.status === 'queued' ? 'Queued…' : 'Running…'} this refreshes on its own.
            </p>
          {:else if loading}
            <p class="hint">Loading the report…</p>
          {:else}
            <BacktestReport run={active} {curve} {trades} indexTicker={INDEX_TICKER} />
          {/if}
        </div>
      </div>
    {/if}
  </div>
</div>

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 60;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 2rem 1rem;
    background: color-mix(in srgb, var(--alvo-ink) 55%, transparent);
  }

  .scrim {
    position: absolute;
    inset: 0;
    background: none;
    border: 0;
    padding: 0;
    cursor: default;
  }

  .dialog {
    position: relative;
    display: flex;
    flex-direction: column;
    gap: 0.7rem;
    width: min(78rem, 100%);
    max-height: 100%;
    padding: 0.9rem 1.1rem 1.1rem;
    background: var(--bg);
    border: 1px solid var(--line);
    border-radius: 0.5rem;
    box-shadow: 0 1.5rem 3rem rgb(0 0 0 / 0.3);
    overflow: hidden;
  }

  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  h2 {
    margin: 0;
    font-size: 0.95rem;
    letter-spacing: 0.06em;
    text-transform: uppercase;
  }

  .help {
    font: inherit;
    font-size: 0.7rem;
    line-height: 1;
    width: 1.15rem;
    height: 1.15rem;
    margin-left: 0.45rem;
    border: 1px solid var(--line);
    border-radius: 50%;
    background: none;
    color: var(--muted);
    cursor: pointer;
    vertical-align: middle;
  }

  .close {
    font: inherit;
    font-size: 1.3rem;
    line-height: 1;
    padding: 0 0.3rem;
    border: 0;
    background: none;
    color: var(--muted);
    cursor: pointer;
  }

  .launch {
    display: flex;
    flex-wrap: wrap;
    align-items: end;
    gap: 0.5rem;
  }

  .launch label {
    display: flex;
    flex-direction: column;
    gap: 0.2rem;
    flex: 1 1 12rem;
    min-width: 0;
  }

  .launch label.short {
    flex: 0 0 auto;
  }

  .launch span {
    font-size: 0.68rem;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--muted);
  }

  .launch input,
  .launch select {
    font: inherit;
    font-size: 0.82rem;
    padding: 0.32rem 0.45rem;
    border: 1px solid var(--line);
    border-radius: 4px;
    background: var(--panel);
    color: var(--fg);
  }

  .launch input[type='number'] {
    width: 7rem;
  }

  .run {
    font: inherit;
    font-size: 0.82rem;
    padding: 0.4rem 1.1rem;
    border: 1px solid var(--accent);
    border-radius: 4px;
    background: var(--accent);
    color: var(--accent-fg);
    cursor: pointer;
  }

  .run:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .body {
    display: grid;
    grid-template-columns: minmax(14rem, 20rem) minmax(0, 1fr);
    gap: 1rem;
    flex: 1;
    min-height: 0;
  }

  .runs {
    list-style: none;
    margin: 0;
    padding: 0;
    overflow-y: auto;
    border: 1px solid var(--line);
    border-radius: 5px;
  }

  .runs li + li {
    border-top: 1px solid var(--line);
  }

  .runs button {
    display: grid;
    grid-template-columns: 1fr auto;
    gap: 0.15rem 0.6rem;
    width: 100%;
    font: inherit;
    text-align: left;
    padding: 0.5rem 0.6rem;
    border: 0;
    background: none;
    color: var(--fg);
    cursor: pointer;
  }

  .runs button:hover,
  .runs button.on {
    background: var(--hover);
  }

  .who {
    font-size: 0.82rem;
    font-weight: 600;
  }

  .when,
  .cash {
    font-size: 0.7rem;
    color: var(--muted);
    font-variant-numeric: tabular-nums;
  }

  .how {
    font-size: 0.82rem;
    text-align: right;
    font-variant-numeric: tabular-nums;
    color: var(--good);
  }

  .how.bad {
    color: var(--bad);
  }

  .how.live {
    color: var(--muted);
  }

  .cash {
    text-align: right;
  }

  .detail {
    overflow-y: auto;
    min-width: 0;
  }

  .launch label.wide {
    flex: 1 1 100%;
  }

  .launch label small {
    color: var(--muted);
    font-size: 0.75rem;
    line-height: 1.35;
  }

  .hint {
    margin: 0;
    padding: 0.6rem;
    font-size: 0.8rem;
    color: var(--muted);
  }

  .error {
    margin: 0;
    padding: 0.45rem 0.6rem;
    font-size: 0.8rem;
    border-left: 2px solid var(--bad);
    background: var(--panel);
    color: var(--bad);
  }
</style>
