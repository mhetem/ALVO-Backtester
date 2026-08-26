<script lang="ts">
  import { onMount } from 'svelte';

  import BacktestPanel from './lib/BacktestPanel.svelte';
  import Chart from './lib/Chart.svelte';
  import IndicatorPanel from './lib/IndicatorPanel.svelte';
  import IndicatorPicker from './lib/IndicatorPicker.svelte';
  import SignIn from './lib/SignIn.svelte';
  import StrategyEditor from './lib/StrategyEditor.svelte';
  import SymbolSearch from './lib/SymbolSearch.svelte';
  import {
    describe,
    HttpError,
    searchSymbols,
    TIMEFRAMES,
    type Bar,
    type ChartMode,
    type SymbolRow,
    type Timeframe,
  } from './lib/api';
  import { marksOf, type Trade, type TradeMark } from './lib/backtest';
  import { fetchCatalog, type Catalog, type CatalogEntry } from './lib/catalog';
  import { formatChange, formatPrice, formatStamp, formatVolume } from './lib/format';
  import {
    addIndicator,
    fromStored,
    toStored,
    type ActiveIndicator,
  } from './lib/indicators';
  import {
    createLayout,
    deleteLayout,
    fetchLayouts,
    readLocalLayouts,
    readWorking,
    sameSet,
    saveLocalLayout,
    updateLayout,
    writeLocalLayouts,
    writeWorking,
    type SavedLayout,
  } from './lib/layouts';
  import { logout, resume, type User } from './lib/session';
  import { seriesPalette } from './lib/theme';

  let symbol = $state('PETR4');
  let name = $state('');
  let timeframe = $state<Timeframe>('1d');
  let mode = $state<ChartMode>('candles');
  let hovered = $state<Bar | null>(null);
  let latest = $state<Bar | null>(null);
  let count = $state(0);

  let catalog = $state<Catalog | null>(null);
  let catalogError = $state<string | null>(null);
  let indicators = $state<ActiveIndicator[]>([]);
  let colors = $state<string[]>(seriesPalette());

  let user = $state<User | null>(null);
  let layouts = $state<SavedLayout[]>([]);
  let activeLayoutId = $state<string | null>(null);
  let layoutError = $state<string | null>(null);
  let layoutBusy = $state(false);
  let addError = $state<string | null>(null);

  let panelOpen = $state(false);
  let pickerOpen = $state(false);
  let signInOpen = $state(false);
  let strategyOpen = $state(false);
  let backtestOpen = $state(false);

  let marks = $state<TradeMark[]>([]);
  let marksFor = $state('');

  let layoutsFor = '';
  let restored = false;

  const shown = $derived(hovered ?? latest);
  const intraday = $derived(timeframe !== '1d');
  const ceiling = $derived(catalog?.max_per_request ?? 8);
  const full = $derived(indicators.length >= ceiling);

  const activeLayout = $derived(layouts.find((layout) => layout.id === activeLayoutId) ?? null);

  // Markers belong to the symbol they were run against; charting another ticker with
  // them still drawn would put fills on candles they never touched.
  const shownMarks = $derived(marksFor === symbol ? marks : []);

  const dirty = $derived(
    activeLayout === null
      ? indicators.length > 0
      : !sameSet(toStored(indicators), activeLayout.indicators),
  );

  const note = $derived(
    user
      ? `${layouts.length} layout${layouts.length === 1 ? '' : 's'} saved to ${user.email}`
      : `${layouts.length} layout${layouts.length === 1 ? '' : 's'} saved in this browser`,
  );

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

  function applyIndicators(next: ActiveIndicator[]) {
    indicators = next;
    writeWorking(toStored(next), activeLayoutId);
  }

  function selectLayout(id: string | null) {
    layoutError = null;
    activeLayoutId = id;

    const chosen = layouts.find((layout) => layout.id === id);
    const next = chosen && catalog ? fromStored(chosen.indicators, catalog) : indicators;

    indicators = chosen ? next : indicators;
    writeWorking(toStored(indicators), id);
  }

  async function persist(name: string, id: string | null) {
    const stored = toStored(indicators);

    if (!user) {
      const next = saveLocalLayout(layouts, id, name, stored);
      writeLocalLayouts(next);
      layouts = next;
      return next.find((layout) => layout.name === name)?.id ?? id;
    }

    const saved = id ? await updateLayout(id, name, stored) : await createLayout(name, stored);
    layouts = [...layouts.filter((layout) => layout.id !== saved.id), saved].sort((a, b) =>
      a.name.localeCompare(b.name),
    );
    return saved.id;
  }

  async function saveLayout(name: string, id: string | null) {
    if (layoutBusy) {
      return;
    }

    layoutBusy = true;
    layoutError = null;

    try {
      activeLayoutId = (await persist(name, id)) ?? null;
      writeWorking(toStored(indicators), activeLayoutId);
    } catch (cause) {
      if (cause instanceof HttpError && cause.status === 401) {
        user = null;
      }
      layoutError = describe(cause);
    } finally {
      layoutBusy = false;
    }
  }

  async function removeLayout() {
    const id = activeLayoutId;
    if (!id || layoutBusy) {
      return;
    }

    layoutBusy = true;
    layoutError = null;

    try {
      if (user) {
        await deleteLayout(id);
      }
      layouts = layouts.filter((layout) => layout.id !== id);
      if (!user) {
        writeLocalLayouts(layouts);
      }
      activeLayoutId = null;
      writeWorking(toStored(indicators), null);
    } catch (cause) {
      if (cause instanceof HttpError && cause.status === 401) {
        user = null;
      }
      layoutError = describe(cause);
    } finally {
      layoutBusy = false;
    }
  }

  async function loadLayouts(who: User | null) {
    if (!who) {
      layouts = readLocalLayouts();
      return;
    }

    try {
      layouts = await fetchLayouts();
      layoutError = null;
    } catch (cause) {
      if (cause instanceof HttpError && cause.status === 401) {
        user = null;
      }
      layouts = [];
      layoutError = describe(cause);
    }
  }

  function openPicker() {
    addError = null;
    pickerOpen = true;
  }

  function add(entry: CatalogEntry) {
    const created = addIndicator(entry, indicators);
    if (!created) {
      addError = `Every ${entry.title} this chart can hold is already on it.`;
      return;
    }

    addError = null;
    pickerOpen = false;
    panelOpen = true;
    applyIndicators([...indicators, created]);
  }

  function forgetActiveLayout() {
    activeLayoutId = null;
    layoutError = null;
    writeWorking(toStored(indicators), null);
  }

  function showTrades(trades: Trade[], ticker: string) {
    marks = marksOf(trades);
    marksFor = ticker;
  }

  function signedIn(who: User) {
    user = who;
    signInOpen = false;
    forgetActiveLayout();
  }

  async function signOut() {
    await logout();
    user = null;
    forgetActiveLayout();
  }

  onMount(() => {
    void (async () => {
      try {
        catalog = await fetchCatalog();
      } catch (cause) {
        catalogError = describe(cause);
      }
    })();

    void (async () => {
      user = await resume();
    })();
  });

  $effect(() => {
    const known = catalog;
    if (!known || restored) {
      return;
    }

    restored = true;
    const working = readWorking();
    indicators = fromStored(working.indicators, known);
    activeLayoutId = working.activeId;
  });

  $effect(() => {
    void resolveName(symbol);
  });

  $effect(() => {
    const known = catalog;
    const who = user;
    if (!known) {
      return;
    }

    const token = who?.id ?? '';
    if (token === layoutsFor) {
      return;
    }
    layoutsFor = token;
    void loadLayouts(who);
  });

  $effect(() => {
    const scheme = window.matchMedia('(prefers-color-scheme: dark)');
    const update = () => {
      colors = seriesPalette();
    };

    scheme.addEventListener('change', update);
    return () => scheme.removeEventListener('change', update);
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

    <div class="account">
      {#if user}
        <span class="who" title={user.email}>{user.email}</span>
        <button type="button" class="plain" onclick={() => void signOut()}>Sign out</button>
      {:else}
        <button type="button" class="plain" onclick={() => (signInOpen = true)}>Sign in</button>
      {/if}
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

    <div class="group" role="group" aria-label="Indicators">
      <button type="button" class:on={panelOpen} onclick={() => (panelOpen = !panelOpen)}>
        Indicators{indicators.length > 0 ? ` · ${indicators.length}` : ''}
      </button>
      <button type="button" onclick={openPicker} aria-label="Add an indicator">+</button>
    </div>

    <div class="group" role="group" aria-label="Strategies">
      <button type="button" class:on={strategyOpen} onclick={() => (strategyOpen = true)}>
        Strategies
      </button>
      <button type="button" class:on={backtestOpen} onclick={() => (backtestOpen = true)}>
        Backtests{shownMarks.length > 0 ? ` · ${shownMarks.length}` : ''}
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

  <div class="workspace">
    <Chart
      {symbol}
      {timeframe}
      {mode}
      {indicators}
      {colors}
      trades={shownMarks}
      onHover={(bar) => (hovered = bar)}
      onLoaded={(bar, total) => {
        latest = bar;
        count = total;
      }}
    />

    {#if panelOpen}
      <IndicatorPanel
        {catalog}
        {indicators}
        {colors}
        {note}
        {layouts}
        {dirty}
        {layoutError}
        activeId={activeLayoutId}
        busy={layoutBusy}
        onChange={applyIndicators}
        onSelectLayout={selectLayout}
        onSaveLayout={(name, id) => void saveLayout(name, id)}
        onDeleteLayout={() => void removeLayout()}
        onAdd={openPicker}
        onClose={() => (panelOpen = false)}
      />
    {/if}
  </div>
</main>

{#if pickerOpen}
  <IndicatorPicker
    {catalog}
    {full}
    error={catalogError ?? addError}
    onAdd={add}
    onClose={() => (pickerOpen = false)}
  />
{/if}

{#if signInOpen}
  <SignIn onDone={signedIn} onClose={() => (signInOpen = false)} />
{/if}

{#if strategyOpen}
  <StrategyEditor {catalog} {user} onClose={() => (strategyOpen = false)} />
{/if}

{#if backtestOpen}
  <BacktestPanel
    {symbol}
    {timeframe}
    {user}
    onTrades={showTrades}
    onClose={() => (backtestOpen = false)}
  />
{/if}

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

  .account {
    display: flex;
    align-items: baseline;
    gap: 0.6rem;
    flex-shrink: 0;
  }

  .who {
    color: var(--muted);
    font-size: 0.75rem;
    max-width: 11rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .plain {
    font: inherit;
    font-size: 0.78rem;
    font-weight: 500;
    color: var(--accent);
    background: none;
    border: 0;
    padding: 0;
    cursor: pointer;
  }

  .plain:hover {
    text-decoration: underline;
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

  .workspace {
    position: relative;
    display: flex;
    flex: 1;
    min-height: 0;
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

    .account {
      order: 2;
    }

    nav {
      flex-wrap: wrap;
    }

    .count {
      margin-left: 0;
    }
  }
</style>
