<script lang="ts">
  import type { SavedLayout } from './layouts';

  type Props = {
    layouts: SavedLayout[];
    activeId: string | null;
    dirty: boolean;
    busy: boolean;
    error: string | null;
    onSelect: (id: string | null) => void;
    onSave: (name: string, id: string | null) => void;
    onDelete: () => void;
  };

  let { layouts, activeId, dirty, busy, error, onSelect, onSave, onDelete }: Props = $props();

  let naming = $state(false);
  let draft = $state('');
  let field = $state<HTMLInputElement | null>(null);

  const active = $derived(layouts.find((layout) => layout.id === activeId) ?? null);

  function beginSaveAs() {
    draft = active ? `${active.name} copy` : 'My layout';
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
    <span>Layout</span>
    <select
      value={activeId ?? ''}
      onchange={(event) => onSelect((event.currentTarget as HTMLSelectElement).value || null)}
    >
      <option value="">{layouts.length === 0 ? 'None saved' : 'Unsaved'}</option>
      {#each layouts as layout (layout.id)}
        <option value={layout.id}>{layout.name}{dirty && layout.id === activeId ? ' •' : ''}</option>
      {/each}
    </select>
  </label>

  <div class="actions">
    <button type="button" onclick={save} disabled={busy || (!dirty && active !== null)}>
      Save
    </button>
    <button type="button" onclick={beginSaveAs} disabled={busy}>Save as</button>
    <button type="button" class="danger" onclick={onDelete} disabled={busy || active === null}>
      Delete
    </button>
  </div>

  {#if naming}
    <form class="naming" onsubmit={confirm}>
      <input
        bind:this={field}
        bind:value={draft}
        type="text"
        maxlength="60"
        placeholder="Layout name"
        aria-label="Layout name"
      />
      <button type="submit" class="go">Create</button>
      <button type="button" onclick={() => (naming = false)}>Cancel</button>
    </form>
  {/if}

  {#if error}
    <p class="bad">{error}</p>
  {/if}
</div>

<style>
  .bar {
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
    padding: 0.6rem 0.75rem;
    border-bottom: 1px solid var(--line);
  }

  .pick {
    display: grid;
    grid-template-columns: auto 1fr;
    align-items: center;
    gap: 0.5rem;
    font-size: 0.7rem;
  }

  .pick > span {
    color: var(--muted);
    letter-spacing: 0.1em;
    text-transform: uppercase;
  }

  select,
  input {
    font: inherit;
    font-size: 0.8125rem;
    color: inherit;
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: 0.25rem;
    padding: 0.2rem 0.4rem;
    width: 100%;
    min-width: 0;
  }

  select:focus,
  input:focus {
    outline: none;
    border-color: var(--accent);
  }

  .actions {
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

  .danger:hover:not(:disabled) {
    color: var(--bad);
  }

  .go {
    color: var(--accent-fg) !important;
    background: var(--accent) !important;
    border-color: var(--accent) !important;
    font-weight: 600;
  }

  .naming {
    display: flex;
    gap: 0.25rem;
  }

  .bad {
    margin: 0;
    color: var(--bad);
    font-size: 0.7rem;
  }
</style>
