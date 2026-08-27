<script lang="ts">
  import {
    MAX_BASKET_NAME,
    MAX_BASKET_SYMBOLS,
    sameBasket,
    tickersOf,
    type SavedBasket,
  } from './baskets';

  type Props = {
    baskets: SavedBasket[];
    activeId: string | null;
    tickers: string[];
    busy: boolean;
    error: string | null;
    onSelect: (id: string | null) => void;
    onSave: (name: string, id: string | null) => void;
  };

  let { baskets, activeId, tickers, busy, error, onSelect, onSave }: Props = $props();

  let naming = $state(false);
  let draft = $state('');
  let field = $state<HTMLInputElement | null>(null);

  const active = $derived(baskets.find((basket) => basket.id === activeId) ?? null);

  const dirty = $derived(
    active === null ? tickers.length > 0 : !sameBasket(tickers, tickersOf(active)),
  );

  // The API refuses both of these, so there is no point letting the request go out.
  const savable = $derived(tickers.length > 0 && tickers.length <= MAX_BASKET_SYMBOLS);

  function beginSaveAs() {
    draft = active ? `${active.name} copy` : tickers.slice(0, 3).join(' ');
    naming = true;
  }

  function confirm(event: SubmitEvent) {
    event.preventDefault();
    const name = draft.trim();
    if (name === '') {
      return;
    }
    naming = false;
    onSave(name, null);
  }

  function save() {
    if (active) {
      onSave(active.name, active.id);
      return;
    }
    beginSaveAs();
  }

  $effect(() => {
    if (naming) {
      field?.focus();
      field?.select();
    }
  });
</script>

<div class="bar">
  <label class="pick">
    <span>Basket</span>
    <select
      value={activeId ?? ''}
      onchange={(event) => onSelect((event.currentTarget as HTMLSelectElement).value || null)}
    >
      <option value="">{baskets.length === 0 ? 'None saved' : 'Unsaved'}</option>
      {#each baskets as basket (basket.id)}
        <option value={basket.id}
          >{basket.name} · {basket.count}{dirty && basket.id === activeId ? ' •' : ''}</option
        >
      {/each}
    </select>
  </label>

  <div class="actions">
    <button type="button" onclick={save} disabled={busy || !savable || (!dirty && active !== null)}>
      Save
    </button>
    <button type="button" onclick={beginSaveAs} disabled={busy || !savable}>Save as</button>
  </div>

  {#if naming}
    <form class="naming" onsubmit={confirm}>
      <input
        bind:this={field}
        bind:value={draft}
        type="text"
        maxlength={MAX_BASKET_NAME}
        placeholder="Basket name"
        aria-label="Basket name"
      />
      <button type="submit" class="go">Create</button>
      <button type="button" onclick={() => (naming = false)}>Cancel</button>
    </form>
  {/if}

  {#if tickers.length > MAX_BASKET_SYMBOLS}
    <p class="bad">A basket holds at most {MAX_BASKET_SYMBOLS} symbols.</p>
  {:else if error}
    <p class="bad">{error}</p>
  {/if}
</div>

<style>
  .bar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0.4rem 0.6rem;
  }

  .pick {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    font-size: 0.68rem;
  }

  .pick > span {
    color: var(--muted);
    letter-spacing: 0.06em;
    text-transform: uppercase;
  }

  select,
  input {
    font: inherit;
    font-size: 0.82rem;
    color: inherit;
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: 4px;
    padding: 0.32rem 0.45rem;
    min-width: 0;
  }

  select {
    max-width: 16rem;
  }

  select:focus,
  input:focus {
    outline: none;
    border-color: var(--accent);
  }

  .actions,
  .naming {
    display: flex;
    gap: 0.25rem;
  }

  .actions button,
  .naming button {
    font: inherit;
    font-size: 0.72rem;
    color: var(--muted);
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: 999px;
    padding: 0.16rem 0.6rem;
    cursor: pointer;
  }

  .actions button:hover:not(:disabled),
  .naming button:hover:not(:disabled) {
    color: var(--fg);
    background: var(--hover);
  }

  .actions button:disabled,
  .naming button:disabled {
    opacity: 0.45;
    cursor: not-allowed;
  }

  .go {
    color: var(--accent-fg) !important;
    background: var(--accent) !important;
    border-color: var(--accent) !important;
    font-weight: 600;
  }

  .bad {
    flex: 1 1 100%;
    margin: 0;
    color: var(--bad);
    font-size: 0.7rem;
  }
</style>
