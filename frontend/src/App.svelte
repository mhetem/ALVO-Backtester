<script lang="ts">
  import Chart from './lib/Chart.svelte';
  import SymbolSearch from './lib/SymbolSearch.svelte';
  import {
    searchSymbols,
    TIMEFRAMES,
    type Bar,
    type ChartMode,
    type SymbolRow,
    type Timeframe,
  } from './lib/api';
  import { formatChange, formatPrice, formatStamp, formatVolume } from './lib/format';

  let symbol = $state('PETR4');
  let name = $state('');
  let timeframe = $state<Timeframe>('1d');
  let mode = $state<ChartMode>('candles');
  let hovered = $state<Bar | null>(null);
  let latest = $state<Bar | null>(null);
  let count = $state(0);

  const shown = $derived(hovered ?? latest);
  const intraday = $derived(timeframe !== '1d');

  function select(row: SymbolRow) {
    symbol = row.ticker;
    name = row.name;
  }

  async function resolveName(ticker: string) {
    try {
      const rows = await searchSymbols(ticker);
      const match = rows.find((row) => row.ticker === ticker);
      name = match ? match.name : ticker;
    } catch {
      name = ticker;
    }
  }

  $effect(() => {
    void resolveName(symbol);
  });
</script>

<main>
  <header>
    <div class="brand">
      <span class="wordmark">ALVO</span>
      <span class="target" aria-hidden="true"></span>
      <span class="product">Backtester</span>
    </div>

    <SymbolSearch onSelect={select} />

    <div class="current">
      <strong>{symbol}</strong>
      <span class="name">{name}</span>
    </div>
  </header>

  <nav>
    <div class="group" role="group" aria-label="Timeframe">
      {#each TIMEFRAMES as tf (tf)}
        <button type="button" class:on={tf === timeframe} onclick={() => (timeframe = tf)}>
          {tf}
        </button>
      {/each}
    </div>

    <div class="group" role="group" aria-label="Chart style">
      <button type="button" class:on={mode === 'candles'} onclick={() => (mode = 'candles')}>
        Candles
      </button>
      <button type="button" class:on={mode === 'bars'} onclick={() => (mode = 'bars')}>
        Bars
      </button>
    </div>

    <span class="count">{count} bars loaded</span>
  </nav>

  <div class="readout" aria-live="off">
    {#if shown}
      <span class="stamp">{formatStamp(shown.time, intraday)}</span>
      <span><i>O</i>{formatPrice(shown.open)}</span>
      <span><i>H</i>{formatPrice(shown.high)}</span>
      <span><i>L</i>{formatPrice(shown.low)}</span>
      <span><i>C</i>{formatPrice(shown.close)}</span>
      <span><i>V</i>{formatVolume(shown.volume)}</span>
      <span class="change" class:up={shown.close >= shown.open} class:down={shown.close < shown.open}>
        {formatChange(shown.open, shown.close)}
      </span>
    {:else}
      <span class="stamp">—</span>
    {/if}
  </div>

  <Chart
    {symbol}
    {timeframe}
    {mode}
    onHover={(bar) => (hovered = bar)}
    onLoaded={(bar, total) => {
      latest = bar;
      count = total;
    }}
  />
</main>

<style>
  main {
    display: flex;
    flex-direction: column;
    height: 100dvh;
  }

  header {
    display: flex;
    align-items: center;
    gap: 1.5rem;
    padding: 0.7rem 1.15rem;
    border-bottom: 1px solid var(--line);
  }

  .brand {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    flex-shrink: 0;
  }

  .wordmark {
    font-size: 1.05rem;
    font-weight: 300;
    letter-spacing: 0.34em;
    margin-right: -0.34em;
  }

  .target {
    display: inline-grid;
    place-items: center;
    width: 0.95rem;
    height: 0.95rem;
    border: 1.5px solid var(--accent);
    border-radius: 50%;
  }

  .target::after {
    content: '';
    width: 0.4rem;
    height: 0.4rem;
    border-radius: 50%;
    background: var(--accent);
  }

  .product {
    color: var(--accent);
    font-size: 0.9rem;
    font-weight: 500;
  }

  .current {
    display: flex;
    align-items: baseline;
    gap: 0.55rem;
    min-width: 0;
    margin-left: auto;
  }

  .current strong {
    font-weight: 600;
    letter-spacing: 0.02em;
  }

  .current .name {
    color: var(--muted);
    font-size: 0.8125rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  nav {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    padding: 0.6rem 1.15rem;
    border-bottom: 1px solid var(--line);
  }

  .group {
    display: flex;
    gap: 0.2rem;
    padding: 0.2rem;
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: 999px;
  }

  .group button {
    font: inherit;
    font-size: 0.78rem;
    font-weight: 500;
    color: var(--muted);
    background: none;
    border: 0;
    border-radius: 999px;
    padding: 0.22rem 0.75rem;
    cursor: pointer;
    transition:
      background 120ms ease,
      color 120ms ease;
  }

  .group button:hover {
    color: var(--fg);
    background: var(--hover);
  }

  .group button.on {
    color: var(--accent-fg);
    background: var(--accent);
    font-weight: 600;
  }

  .group button.on:hover {
    background: var(--accent);
  }

  .count {
    margin-left: auto;
    color: var(--muted);
    font-size: 0.7rem;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    font-variant-numeric: tabular-nums;
  }

  .readout {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 1.1rem;
    padding: 0.55rem 1.15rem;
    background: var(--panel);
    border-bottom: 1px solid var(--line);
    font-size: 0.8125rem;
    font-variant-numeric: tabular-nums;
    font-feature-settings: 'tnum' 1;
    min-height: 1.35rem;
  }

  .readout i {
    color: var(--muted);
    font-style: normal;
    font-size: 0.7rem;
    font-weight: 600;
    letter-spacing: 0.1em;
    margin-right: 0.35rem;
  }

  .stamp {
    color: var(--muted);
    letter-spacing: 0.03em;
  }

  .change {
    font-weight: 600;
  }

  .change.up {
    color: var(--good);
  }

  .change.down {
    color: var(--bad);
  }

  @media (max-width: 46rem) {
    header {
      flex-wrap: wrap;
      gap: 0.75rem;
    }

    .current {
      order: 3;
      width: 100%;
      margin-left: 0;
    }

    nav {
      flex-wrap: wrap;
    }

    .count {
      margin-left: 0;
    }
  }
</style>
