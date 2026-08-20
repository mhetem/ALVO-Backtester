<script lang="ts">
  import { onDestroy } from 'svelte';

  import { searchSymbols, type SymbolRow } from './api';

  type Props = {
    onSelect: (row: SymbolRow) => void;
  };

  let { onSelect }: Props = $props();

  let input = $state<HTMLInputElement | null>(null);
  let query = $state('');
  let results = $state<SymbolRow[]>([]);
  let open = $state(false);
  let active = $state(0);
  let error = $state<string | null>(null);

  let timer: ReturnType<typeof setTimeout> | null = null;
  let pending: AbortController | null = null;

  async function run(text: string) {
    pending?.abort();
    const controller = new AbortController();
    pending = controller;

    try {
      results = await searchSymbols(text, controller.signal);
      active = 0;
      error = null;
    } catch (cause) {
      if (!controller.signal.aborted) {
        results = [];
        error = cause instanceof Error ? cause.message : String(cause);
      }
    }
  }

  function schedule(text: string) {
    if (timer) {
      clearTimeout(timer);
    }
    timer = setTimeout(() => void run(text), 150);
  }

  function onInput() {
    open = true;
    schedule(query);
  }

  function choose(row: SymbolRow) {
    onSelect(row);
    query = '';
    results = [];
    open = false;
    input?.blur();
  }

  function onKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      open = false;
      input?.blur();
      return;
    }
    if (!open || results.length === 0) {
      return;
    }

    if (event.key === 'ArrowDown') {
      event.preventDefault();
      active = (active + 1) % results.length;
    } else if (event.key === 'ArrowUp') {
      event.preventDefault();
      active = (active - 1 + results.length) % results.length;
    } else if (event.key === 'Enter') {
      event.preventDefault();
      choose(results[active]);
    }
  }

  function onFocusout(event: FocusEvent) {
    const next = event.relatedTarget;
    if (next instanceof Node && event.currentTarget instanceof Node && event.currentTarget.contains(next)) {
      return;
    }
    open = false;
  }

  onDestroy(() => {
    if (timer) {
      clearTimeout(timer);
    }
    pending?.abort();
  });
</script>

<div class="search" onfocusout={onFocusout}>
  <input
    bind:this={input}
    bind:value={query}
    oninput={onInput}
    onfocus={onInput}
    onkeydown={onKeydown}
    type="search"
    placeholder="Search a ticker"
    autocomplete="off"
    spellcheck="false"
    aria-label="Symbol search"
  />

  {#if open && (results.length > 0 || error)}
    <ul class="results">
      {#if error}
        <li class="bad">{error}</li>
      {:else}
        {#each results as row, i (row.ticker)}
          <li>
            <button
              type="button"
              class:active={i === active}
              onmousedown={(event) => event.preventDefault()}
              onclick={() => choose(row)}
            >
              <span class="ticker">{row.ticker}</span>
              <span class="name">{row.name}</span>
              <span class="kind">{row.kind}{row.tracked ? '' : ' · untracked'}</span>
            </button>
          </li>
        {/each}
      {/if}
    </ul>
  {/if}
</div>

<style>
  .search {
    position: relative;
    width: min(22rem, 100%);
  }

  input {
    width: 100%;
    font: inherit;
    font-size: 0.875rem;
    color: inherit;
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: 999px;
    padding: 0.4rem 0.95rem;
    transition:
      border-color 120ms ease,
      box-shadow 120ms ease;
  }

  input::placeholder {
    color: var(--muted);
  }

  input:focus {
    outline: none;
    border-color: var(--accent);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent) 18%, transparent);
  }

  .results {
    position: absolute;
    z-index: 10;
    top: calc(100% + 0.25rem);
    left: 0;
    right: 0;
    margin: 0;
    padding: 0.25rem;
    list-style: none;
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: 0.375rem;
    box-shadow: 0 0.5rem 1.5rem rgb(0 0 0 / 0.18);
    max-height: 20rem;
    overflow-y: auto;
  }

  .results li {
    margin: 0;
  }

  .results .bad {
    color: var(--bad);
    padding: 0.4rem 0.5rem;
    font-size: 0.875rem;
  }

  button {
    display: grid;
    grid-template-columns: 5rem 1fr auto;
    gap: 0.6rem;
    align-items: baseline;
    width: 100%;
    font: inherit;
    color: inherit;
    text-align: left;
    background: none;
    border: 0;
    border-radius: 0.25rem;
    padding: 0.35rem 0.5rem;
    cursor: pointer;
  }

  button:hover,
  button.active {
    background: var(--hover);
  }

  button.active .ticker {
    color: var(--accent);
  }

  .ticker {
    font-variant-numeric: tabular-nums;
    font-weight: 600;
    letter-spacing: 0.02em;
  }

  .name {
    color: var(--muted);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .kind {
    color: var(--muted);
    font-size: 0.68rem;
    letter-spacing: 0.06em;
    text-transform: uppercase;
  }
</style>
