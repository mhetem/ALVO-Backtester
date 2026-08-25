<script lang="ts">
  import { summarize, type Catalog, type CatalogEntry } from './catalog';

  type Props = {
    catalog: Catalog | null;
    error: string | null;
    full: boolean;
    onAdd: (entry: CatalogEntry) => void;
    onClose: () => void;
  };

  let { catalog, error, full, onAdd, onClose }: Props = $props();

  let query = $state('');
  let group = $state('');
  let field = $state<HTMLInputElement | null>(null);

  const groups = $derived(catalog?.groups ?? []);

  const matches = $derived.by(() => {
    const needle = query.trim().toLowerCase();
    return (catalog?.indicators ?? []).filter((entry) => {
      if (group !== '' && entry.group !== group) {
        return false;
      }
      if (needle === '') {
        return true;
      }
      return entry.name.includes(needle) || entry.title.toLowerCase().includes(needle);
    });
  });

  function onKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      event.stopPropagation();
      onClose();
    }
  }

  $effect(() => {
    field?.focus();
  });
</script>

<svelte:window onkeydown={onKeydown} />

<div class="backdrop">
  <button type="button" class="scrim" aria-label="Close" onclick={onClose}></button>

  <div class="dialog" role="dialog" aria-modal="true" aria-label="Add an indicator">
    <header>
      <input
        bind:this={field}
        bind:value={query}
        type="search"
        placeholder="Search {catalog?.count ?? 0} indicators"
        autocomplete="off"
        spellcheck="false"
        aria-label="Search indicators"
      />
      <button type="button" class="close" onclick={onClose} aria-label="Close">×</button>
    </header>

    <div class="groups" role="group" aria-label="Indicator group">
      <button type="button" class:on={group === ''} onclick={() => (group = '')}>all</button>
      {#each groups as name (name)}
        <button type="button" class:on={group === name} onclick={() => (group = name)}>
          {name}
        </button>
      {/each}
    </div>

    {#if full}
      <p class="notice">
        The chart already carries {catalog?.max_per_request ?? 8} indicators — remove one to add another.
      </p>
    {/if}

    {#if error}
      <p class="notice bad">{error}</p>
    {/if}

    <ul class="results">
      {#each matches as entry (entry.name)}
        <li>
          <button type="button" disabled={full} onclick={() => onAdd(entry)}>
            <span class="title">{entry.title}</span>
            <span class="name">{entry.name}</span>
            <span class="meta">{summarize(entry)}</span>
            <span class="tags">
              <span class="tag">{entry.group}</span>
              {#if entry.overlay}<span class="tag">price</span>{:else}<span class="tag">pane</span>{/if}
              <span class="tag">warmup {entry.warmup}</span>
            </span>
          </button>
        </li>
      {:else}
        <li class="none">Nothing matches “{query}”.</li>
      {/each}
    </ul>
  </div>
</div>

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 50;
    display: flex;
    align-items: flex-start;
    justify-content: center;
    padding: 4rem 1rem 1rem;
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
    width: min(38rem, 100%);
    max-height: 100%;
    background: var(--bg);
    border: 1px solid var(--line);
    border-radius: 0.5rem;
    box-shadow: 0 1.5rem 3rem rgb(0 0 0 / 0.3);
    overflow: hidden;
  }

  header {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    padding: 0.75rem 0.9rem;
    border-bottom: 1px solid var(--line);
  }

  header input {
    flex: 1;
    font: inherit;
    font-size: 0.875rem;
    color: inherit;
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: 999px;
    padding: 0.4rem 0.95rem;
  }

  header input:focus {
    outline: none;
    border-color: var(--accent);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent) 18%, transparent);
  }

  .close {
    font: inherit;
    font-size: 1.2rem;
    line-height: 1;
    color: var(--muted);
    background: none;
    border: 0;
    padding: 0.25rem 0.4rem;
    cursor: pointer;
  }

  .close:hover {
    color: var(--fg);
  }

  .groups {
    display: flex;
    flex-wrap: wrap;
    gap: 0.25rem;
    padding: 0.6rem 0.9rem;
    border-bottom: 1px solid var(--line);
  }

  .groups button {
    font: inherit;
    font-size: 0.7rem;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--muted);
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: 999px;
    padding: 0.18rem 0.6rem;
    cursor: pointer;
  }

  .groups button.on {
    color: var(--accent-fg);
    background: var(--accent);
    border-color: var(--accent);
  }

  .notice {
    margin: 0;
    padding: 0.6rem 0.9rem;
    font-size: 0.8125rem;
    color: var(--muted);
    border-bottom: 1px solid var(--line);
  }

  .notice.bad {
    color: var(--bad);
  }

  .results {
    margin: 0;
    padding: 0.35rem;
    list-style: none;
    overflow-y: auto;
  }

  .results li {
    margin: 0;
  }

  .results .none {
    padding: 1rem;
    color: var(--muted);
    font-size: 0.875rem;
    text-align: center;
  }

  .results button {
    display: grid;
    grid-template-columns: 1fr auto;
    gap: 0.15rem 0.6rem;
    width: 100%;
    font: inherit;
    color: inherit;
    text-align: left;
    background: none;
    border: 0;
    border-radius: 0.3rem;
    padding: 0.45rem 0.55rem;
    cursor: pointer;
  }

  .results button:hover:not(:disabled) {
    background: var(--hover);
  }

  .results button:disabled {
    cursor: not-allowed;
    opacity: 0.45;
  }

  .title {
    font-size: 0.875rem;
    font-weight: 600;
  }

  .name {
    grid-column: 2;
    grid-row: 1;
    color: var(--accent);
    font-size: 0.75rem;
    letter-spacing: 0.06em;
  }

  .meta {
    color: var(--muted);
    font-size: 0.75rem;
  }

  .tags {
    grid-column: 2;
    grid-row: 2;
    display: flex;
    gap: 0.3rem;
    justify-content: flex-end;
  }

  .tag {
    color: var(--muted);
    font-size: 0.62rem;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    border: 1px solid var(--line);
    border-radius: 999px;
    padding: 0.02rem 0.4rem;
  }
</style>
