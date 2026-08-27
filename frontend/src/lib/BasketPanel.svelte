<script lang="ts">
  import { onMount } from 'svelte';

  import SymbolSearch from './SymbolSearch.svelte';
  import { describe, type SymbolRow } from './api';
  import {
    createBasket,
    deleteBasket,
    fetchBaskets,
    MAX_BASKET_NAME,
    MAX_BASKET_SYMBOLS,
    replaceBasket,
    sameBasket,
    tickersOf,
    updateBasket,
    type SavedBasket,
  } from './baskets';
  import type { User } from './session';

  type Props = {
    user: User | null;
    symbol: string;
    onPick: (ticker: string) => void;
    onClose: () => void;
  };

  let { user, symbol, onPick, onClose }: Props = $props();

  let baskets = $state<SavedBasket[]>([]);
  let activeId = $state<string | null>(null);
  let name = $state('');
  let draft = $state<SymbolRow[]>([]);

  let error = $state<string | null>(null);
  let busy = $state(false);
  let loading = $state(false);

  const active = $derived(baskets.find((basket) => basket.id === activeId) ?? null);
  const tickers = $derived(draft.map((row) => row.ticker));
  const full = $derived(draft.length >= MAX_BASKET_SYMBOLS);

  const dirty = $derived(
    active === null
      ? name.trim() !== '' || draft.length > 0
      : name.trim() !== active.name || !sameBasket(tickers, tickersOf(active)),
  );

  const savable = $derived(name.trim() !== '' && draft.length > 0);

  async function load() {
    if (!user) {
      baskets = [];
      return;
    }

    loading = true;
    try {
      baskets = await fetchBaskets();
      error = null;
    } catch (cause) {
      baskets = [];
      error = describe(cause);
    } finally {
      loading = false;
    }
  }

  function choose(basket: SavedBasket) {
    activeId = basket.id;
    name = basket.name;
    draft = [...basket.symbols];
    error = null;
  }

  function startNew() {
    activeId = null;
    name = '';
    draft = [];
    error = null;
  }

  function add(row: SymbolRow) {
    if (draft.some((held) => held.ticker === row.ticker)) {
      return;
    }
    if (full) {
      error = `A basket holds at most ${MAX_BASKET_SYMBOLS} symbols.`;
      return;
    }
    error = null;
    draft = [...draft, row];
  }

  function drop(ticker: string) {
    draft = draft.filter((row) => row.ticker !== ticker);
  }

  function move(index: number, by: number) {
    const to = index + by;
    if (to < 0 || to >= draft.length) {
      return;
    }
    const next = [...draft];
    [next[index], next[to]] = [next[to], next[index]];
    draft = next;
  }

  async function save() {
    const trimmed = name.trim();
    if (busy || trimmed === '' || draft.length === 0) {
      return;
    }

    busy = true;
    error = null;

    try {
      const saved = activeId
        ? await updateBasket(activeId, trimmed, tickers)
        : await createBasket(trimmed, tickers);

      baskets = replaceBasket(baskets, saved);
      choose(saved);
    } catch (cause) {
      error = describe(cause);
    } finally {
      busy = false;
    }
  }

  async function remove() {
    const id = activeId;
    if (!id || busy) {
      return;
    }

    busy = true;
    error = null;

    try {
      await deleteBasket(id);
      baskets = baskets.filter((basket) => basket.id !== id);
      startNew();
    } catch (cause) {
      error = describe(cause);
    } finally {
      busy = false;
    }
  }

  function onKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      event.stopPropagation();
      onClose();
    }
  }

  onMount(() => {
    void load();
  });
</script>

<svelte:window onkeydown={onKeydown} />

<div class="backdrop">
  <button type="button" class="scrim" aria-label="Close" onclick={onClose}></button>

  <div class="dialog" role="dialog" aria-modal="true" aria-label="Baskets">
    <header>
      <h2>Baskets</h2>
      <button type="button" class="close" onclick={onClose} aria-label="Close">×</button>
    </header>

    {#if !user}
      <p class="hint">Sign in to keep baskets of the tickers you follow.</p>
    {:else}
      <div class="body">
        <div class="side">
          <ol class="list">
            {#each baskets as basket (basket.id)}
              <li>
                <button
                  type="button"
                  class:on={basket.id === activeId}
                  onclick={() => choose(basket)}
                >
                  <span class="who">{basket.name}</span>
                  <span class="how">{basket.count}</span>
                  <span class="what">{tickersOf(basket).join(', ')}</span>
                </button>
              </li>
            {:else}
              <li class="hint">{loading ? 'Loading…' : 'No baskets yet.'}</li>
            {/each}
          </ol>

          <button type="button" class="new" onclick={startNew} disabled={busy}>New basket</button>
        </div>

        <div class="editor">
          <label class="naming">
            <span>Name</span>
            <input
              bind:value={name}
              type="text"
              maxlength={MAX_BASKET_NAME}
              placeholder="Blue chips"
              spellcheck="false"
            />
          </label>

          <div class="adding">
            <span class="label">Add a symbol</span>
            <SymbolSearch onSelect={add} />
          </div>

          <p class="count">
            {draft.length} of {MAX_BASKET_SYMBOLS} · click a ticker to chart it
          </p>

          <ul class="chips">
            {#each draft as row, i (row.ticker)}
              <li class:current={row.ticker === symbol}>
                <button
                  type="button"
                  class="chart"
                  title="Chart {row.ticker}"
                  onclick={() => onPick(row.ticker)}
                >
                  <span class="ticker">{row.ticker}</span>
                  <span class="name">{row.name}</span>
                </button>
                <button
                  type="button"
                  class="nudge"
                  title="Move up"
                  aria-label="Move {row.ticker} up"
                  disabled={i === 0}
                  onclick={() => move(i, -1)}>↑</button
                >
                <button
                  type="button"
                  class="nudge"
                  title="Move down"
                  aria-label="Move {row.ticker} down"
                  disabled={i === draft.length - 1}
                  onclick={() => move(i, 1)}>↓</button
                >
                <button
                  type="button"
                  class="drop"
                  aria-label="Remove {row.ticker}"
                  onclick={() => drop(row.ticker)}>×</button
                >
              </li>
            {:else}
              <li class="empty">Search above to put a ticker in this basket.</li>
            {/each}
          </ul>

          <div class="actions">
            <button
              type="button"
              class="go"
              onclick={() => void save()}
              disabled={busy || !savable || !dirty}
            >
              {busy ? 'Saving…' : activeId ? 'Save' : 'Create'}
            </button>
            <button
              type="button"
              class="danger"
              onclick={() => void remove()}
              disabled={busy || activeId === null}
            >
              Delete
            </button>
          </div>

          {#if error}
            <p class="error">{error}</p>
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
    width: min(52rem, 100%);
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

  .body {
    display: grid;
    grid-template-columns: minmax(12rem, 16rem) minmax(0, 1fr);
    gap: 1rem;
    flex: 1;
    min-height: 0;
  }

  .side {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
    min-height: 0;
  }

  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    overflow-y: auto;
    flex: 1;
    border: 1px solid var(--line);
    border-radius: 5px;
  }

  .list li + li {
    border-top: 1px solid var(--line);
  }

  .list button {
    display: grid;
    grid-template-columns: 1fr auto;
    gap: 0.1rem 0.5rem;
    width: 100%;
    font: inherit;
    text-align: left;
    padding: 0.45rem 0.6rem;
    border: 0;
    background: none;
    color: var(--fg);
    cursor: pointer;
  }

  .list button:hover,
  .list button.on {
    background: var(--hover);
  }

  .who {
    font-size: 0.82rem;
    font-weight: 600;
  }

  .how {
    font-size: 0.7rem;
    color: var(--muted);
    font-variant-numeric: tabular-nums;
  }

  .what {
    grid-column: 1 / -1;
    font-size: 0.68rem;
    color: var(--muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .new {
    font: inherit;
    font-size: 0.72rem;
    color: var(--muted);
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: 999px;
    padding: 0.2rem 0.6rem;
    cursor: pointer;
  }

  .new:hover:not(:disabled) {
    color: var(--fg);
    background: var(--hover);
  }

  .editor {
    display: flex;
    flex-direction: column;
    gap: 0.55rem;
    min-width: 0;
    overflow-y: auto;
  }

  .naming {
    display: flex;
    flex-direction: column;
    gap: 0.2rem;
  }

  .naming span,
  .label {
    font-size: 0.68rem;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--muted);
  }

  .naming input {
    font: inherit;
    font-size: 0.82rem;
    padding: 0.32rem 0.45rem;
    border: 1px solid var(--line);
    border-radius: 4px;
    background: var(--panel);
    color: var(--fg);
  }

  .naming input:focus {
    outline: none;
    border-color: var(--accent);
  }

  .adding {
    display: flex;
    flex-direction: column;
    gap: 0.2rem;
  }

  .count {
    margin: 0;
    font-size: 0.7rem;
    color: var(--muted);
  }

  .chips {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
  }

  .chips li {
    display: grid;
    grid-template-columns: 1fr auto auto auto;
    align-items: center;
    gap: 0.2rem;
    border: 1px solid var(--line);
    border-radius: 4px;
    background: var(--panel);
  }

  .chips li.current {
    border-color: var(--accent);
  }

  .chips .empty {
    display: block;
    padding: 0.5rem 0.6rem;
    font-size: 0.78rem;
    color: var(--muted);
    background: none;
    border-style: dashed;
  }

  .chart {
    display: grid;
    grid-template-columns: 5rem 1fr;
    gap: 0.5rem;
    align-items: baseline;
    font: inherit;
    text-align: left;
    padding: 0.35rem 0.5rem;
    border: 0;
    background: none;
    color: var(--fg);
    cursor: pointer;
    min-width: 0;
  }

  .chart:hover .ticker {
    color: var(--accent);
  }

  .ticker {
    font-size: 0.82rem;
    font-weight: 600;
    font-variant-numeric: tabular-nums;
  }

  .name {
    font-size: 0.72rem;
    color: var(--muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .nudge,
  .drop {
    font: inherit;
    font-size: 0.8rem;
    line-height: 1;
    padding: 0.25rem 0.4rem;
    border: 0;
    background: none;
    color: var(--muted);
    cursor: pointer;
  }

  .nudge:hover:not(:disabled),
  .drop:hover {
    color: var(--fg);
  }

  .nudge:disabled {
    opacity: 0.3;
    cursor: not-allowed;
  }

  .drop:hover {
    color: var(--bad);
  }

  .actions {
    display: flex;
    gap: 0.3rem;
  }

  .actions button {
    font: inherit;
    font-size: 0.78rem;
    padding: 0.35rem 0.9rem;
    border: 1px solid var(--line);
    border-radius: 4px;
    background: var(--panel);
    color: var(--muted);
    cursor: pointer;
  }

  .actions button:disabled {
    opacity: 0.45;
    cursor: not-allowed;
  }

  .go:not(:disabled) {
    border-color: var(--accent);
    background: var(--accent);
    color: var(--accent-fg);
    font-weight: 600;
  }

  .danger:hover:not(:disabled) {
    color: var(--bad);
    border-color: var(--bad);
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
