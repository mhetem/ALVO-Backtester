<script lang="ts">
  import { onDestroy } from 'svelte';

  import { findEntry, formatParam, type Catalog, type CatalogParam } from './catalog';
  import {
    drawnOutputs,
    recolor,
    restyle,
    retune,
    slotOf,
    LINE_STYLES,
    type ActiveIndicator,
    type LineStyleName,
  } from './indicators';
  import { SERIES_SLOTS } from './theme';
  import LayoutBar from './LayoutBar.svelte';
  import type { SavedLayout } from './layouts';

  type Props = {
    catalog: Catalog | null;
    indicators: ActiveIndicator[];
    colors: string[];
    note: string;
    layouts: SavedLayout[];
    activeId: string | null;
    dirty: boolean;
    busy: boolean;
    layoutError: string | null;
    onChange: (next: ActiveIndicator[]) => void;
    onSelectLayout: (id: string | null) => void;
    onSaveLayout: (name: string, id: string | null) => void;
    onDeleteLayout: () => void;
    onAdd: () => void;
    onClose: () => void;
  };

  let {
    catalog,
    indicators,
    colors,
    note,
    layouts,
    activeId,
    dirty,
    busy,
    layoutError,
    onChange,
    onSelectLayout,
    onSaveLayout,
    onDeleteLayout,
    onAdd,
    onClose,
  }: Props = $props();

  const slots = Array.from({ length: SERIES_SLOTS }, (_, i) => i);

  let open = $state<number | null>(null);
  let drafts = $state<Record<string, string>>({});
  let timer: ReturnType<typeof setTimeout> | null = null;

  function draftKey(at: number, param: string): string {
    return `${at}:${param}`;
  }

  function shown(at: number, active: ActiveIndicator, param: CatalogParam): string {
    const draft = drafts[draftKey(at, param.name)];
    return draft ?? formatParam(param, active.params[param.name] ?? param.default);
  }

  function schedule() {
    if (timer) {
      clearTimeout(timer);
    }
    timer = setTimeout(commit, 350);
  }

  function commit() {
    timer = null;

    const pending = drafts;
    drafts = {};
    if (!catalog || Object.keys(pending).length === 0) {
      return;
    }

    const taken = new Set(indicators.map((active) => active.key));
    const next = indicators.map((active, at) => {
      const params = { ...active.params };
      let touched = false;

      for (const name of Object.keys(params)) {
        const text = pending[draftKey(at, name)];
        if (text === undefined) {
          continue;
        }
        const value = Number(text);
        if (text.trim() === '' || !Number.isFinite(value)) {
          continue;
        }
        params[name] = value;
        touched = true;
      }

      if (!touched) {
        return active;
      }

      const tuned = retune(active, catalog, params, active.source);
      if (tuned.key !== active.key && taken.has(tuned.key)) {
        return active;
      }

      taken.delete(active.key);
      taken.add(tuned.key);
      return tuned;
    });

    onChange(next);
  }

  function onParamInput(at: number, param: CatalogParam, event: Event) {
    const input = event.currentTarget as HTMLInputElement;
    drafts = { ...drafts, [draftKey(at, param.name)]: input.value };
    schedule();
  }

  function onSource(at: number, source: string) {
    if (!catalog) {
      return;
    }

    const active = indicators[at];
    const tuned = retune(active, catalog, active.params, source);
    if (tuned.key !== active.key && indicators.some((other) => other.key === tuned.key)) {
      return;
    }

    onChange(indicators.map((entry, i) => (i === at ? tuned : entry)));
  }

  function onStyle(at: number, style: LineStyleName) {
    onChange(indicators.map((entry, i) => (i === at ? restyle(entry, style) : entry)));
  }

  function onColor(at: number, output: string, slot: number) {
    onChange(indicators.map((entry, i) => (i === at ? recolor(entry, output, slot) : entry)));
  }

  function onVisible(at: number) {
    onChange(
      indicators.map((entry, i) => (i === at ? { ...entry, visible: !entry.visible } : entry)),
    );
  }

  function onRemove(at: number) {
    if (open !== null && open >= at) {
      open = open === at ? null : open - 1;
    }
    onChange(indicators.filter((_, i) => i !== at));
  }

  onDestroy(() => {
    if (timer) {
      clearTimeout(timer);
    }
  });
</script>

<aside class="panel" aria-label="Indicators">
  <header>
    <span class="heading">Indicators</span>
    <button type="button" class="add" onclick={onAdd}>Add</button>
    <button type="button" class="close" onclick={onClose} aria-label="Hide the indicator panel">
      ×
    </button>
  </header>

  <LayoutBar
    {layouts}
    {activeId}
    {dirty}
    {busy}
    error={layoutError}
    onSelect={onSelectLayout}
    onSave={onSaveLayout}
    onDelete={onDeleteLayout}
  />

  {#if indicators.length === 0}
    <p class="none">
      Nothing on the chart yet. <button type="button" class="link" onclick={onAdd}>Add one</button> —
      overlays draw on price, oscillators get their own pane.
    </p>
  {:else}
    <ul class="stack">
      {#each indicators as active, at (at)}
        {@const entry = findEntry(catalog, active.name)}
        <li class:off={!active.visible}>
          <div class="row">
            <button
              type="button"
              class="expand"
              aria-expanded={open === at}
              onclick={() => (open = open === at ? null : at)}
            >
              <i class="dot" style="background: {colors[slotOf(active, active.outputs[0])]}"></i>
              <span class="key">{active.key}</span>
              <span class="title">{active.title}</span>
            </button>

            <button
              type="button"
              class="icon"
              aria-pressed={active.visible}
              aria-label={active.visible ? 'Hide' : 'Show'}
              onclick={() => onVisible(at)}
            >
              {active.visible ? '◉' : '○'}
            </button>

            <button
              type="button"
              class="icon"
              aria-label="Remove {active.key}"
              onclick={() => onRemove(at)}
            >
              ×
            </button>
          </div>

          {#if open === at && entry}
            <div class="body">
              {#each entry.params as param (param.name)}
                <label class="field">
                  <span>{param.name}</span>
                  <input
                    type="number"
                    value={shown(at, active, param)}
                    min={param.min}
                    max={param.max}
                    step={param.kind === 'int' ? 1 : 'any'}
                    oninput={(event) => onParamInput(at, param, event)}
                    onblur={commit}
                  />
                  <em>{formatParam(param, param.min)}–{formatParam(param, param.max)}</em>
                </label>
              {/each}

              {#if entry.sourced}
                <label class="field">
                  <span>source</span>
                  <select
                    value={active.source}
                    onchange={(event) => onSource(at, (event.currentTarget as HTMLSelectElement).value)}
                  >
                    {#each catalog?.sources ?? [] as source (source)}
                      <option value={source}>{source}</option>
                    {/each}
                  </select>
                </label>
              {/if}

              <div class="field">
                <span>line</span>
                <div class="styles">
                  {#each LINE_STYLES as name (name)}
                    <button
                      type="button"
                      class:on={active.style === name}
                      onclick={() => onStyle(at, name)}
                    >
                      {name}
                    </button>
                  {/each}
                </div>
              </div>

              {#each drawnOutputs(active) as output (output)}
                <div class="field colours">
                  <span>{output}</span>
                  <div class="swatches">
                    {#each slots as slot (slot)}
                      <button
                        type="button"
                        class="swatch"
                        class:on={slotOf(active, output) === slot}
                        style="background: {colors[slot]}"
                        aria-label="Colour {slot + 1} for {output}"
                        onclick={() => onColor(at, output, slot)}
                      ></button>
                    {/each}
                  </div>
                </div>
              {/each}

              <p class="warmup">
                {entry.overlay ? 'Drawn on price' : 'Own pane'} · warmup {entry.warmup} bars
              </p>
            </div>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}

  <footer>{note}</footer>
</aside>

<style>
  .panel {
    display: flex;
    flex-direction: column;
    width: 20rem;
    flex-shrink: 0;
    border-left: 1px solid var(--line);
    background: var(--bg);
    overflow: hidden;
  }

  header {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.6rem 0.75rem;
    border-bottom: 1px solid var(--line);
  }

  .heading {
    flex: 1;
    font-size: 0.7rem;
    letter-spacing: 0.12em;
    text-transform: uppercase;
    color: var(--muted);
  }

  .add {
    font: inherit;
    font-size: 0.75rem;
    font-weight: 600;
    color: var(--accent-fg);
    background: var(--accent);
    border: 0;
    border-radius: 999px;
    padding: 0.2rem 0.7rem;
    cursor: pointer;
  }

  .close {
    font: inherit;
    font-size: 1.1rem;
    line-height: 1;
    color: var(--muted);
    background: none;
    border: 0;
    padding: 0.1rem 0.25rem;
    cursor: pointer;
  }

  .close:hover {
    color: var(--fg);
  }

  .none {
    margin: 0;
    padding: 1rem 0.85rem;
    color: var(--muted);
    font-size: 0.8125rem;
  }

  .link {
    font: inherit;
    font-size: inherit;
    color: var(--accent);
    background: none;
    border: 0;
    padding: 0;
    cursor: pointer;
    text-decoration: underline;
  }

  .stack {
    flex: 1;
    margin: 0;
    padding: 0.35rem;
    list-style: none;
    overflow-y: auto;
  }

  .stack li {
    margin: 0 0 0.15rem;
    border-radius: 0.35rem;
  }

  .stack li.off {
    opacity: 0.5;
  }

  .row {
    display: flex;
    align-items: center;
    gap: 0.15rem;
  }

  .row:hover {
    background: var(--hover);
    border-radius: 0.35rem;
  }

  .expand {
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 0 0.45rem;
    flex: 1;
    min-width: 0;
    font: inherit;
    color: inherit;
    text-align: left;
    background: none;
    border: 0;
    padding: 0.35rem 0.45rem;
    cursor: pointer;
  }

  .dot {
    grid-row: 1 / span 2;
    align-self: center;
    width: 0.5rem;
    height: 0.5rem;
    border-radius: 50%;
    box-shadow: 0 0 0 1px var(--line);
  }

  .key {
    font-size: 0.8125rem;
    font-weight: 600;
    font-variant-numeric: tabular-nums;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .title {
    color: var(--muted);
    font-size: 0.7rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .icon {
    font: inherit;
    font-size: 0.9rem;
    line-height: 1;
    color: var(--muted);
    background: none;
    border: 0;
    padding: 0.25rem 0.3rem;
    cursor: pointer;
  }

  .icon:hover {
    color: var(--fg);
  }

  .body {
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
    padding: 0.5rem 0.55rem 0.7rem;
    border-top: 1px solid var(--line);
    margin: 0.25rem 0.1rem 0.5rem;
  }

  .field {
    display: grid;
    grid-template-columns: 4.5rem 1fr;
    align-items: center;
    gap: 0.5rem;
    font-size: 0.75rem;
  }

  .field > span {
    color: var(--muted);
    letter-spacing: 0.04em;
  }

  .field input,
  .field select {
    font: inherit;
    font-size: 0.8125rem;
    color: inherit;
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: 0.25rem;
    padding: 0.2rem 0.4rem;
    width: 100%;
  }

  .field input:focus,
  .field select:focus {
    outline: none;
    border-color: var(--accent);
  }

  .field em {
    grid-column: 2;
    color: var(--muted);
    font-style: normal;
    font-size: 0.65rem;
  }

  .colours {
    align-items: start;
  }

  .swatches {
    display: flex;
    flex-wrap: wrap;
    gap: 0.25rem;
  }

  .styles {
    display: flex;
    gap: 0.25rem;
  }

  .styles button {
    font: inherit;
    font-size: 0.7rem;
    color: var(--muted);
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: 999px;
    padding: 0.12rem 0.6rem;
    cursor: pointer;
  }

  .styles button.on {
    color: var(--accent-fg);
    background: var(--accent);
    border-color: var(--accent);
  }

  .swatch {
    width: 1.05rem;
    height: 1.05rem;
    border: 2px solid transparent;
    border-radius: 50%;
    box-shadow: 0 0 0 1px var(--line);
    padding: 0;
    cursor: pointer;
  }

  .swatch.on {
    border-color: var(--fg);
  }

  .warmup {
    margin: 0.1rem 0 0;
    color: var(--muted);
    font-size: 0.65rem;
    letter-spacing: 0.04em;
  }

  footer {
    padding: 0.5rem 0.75rem;
    border-top: 1px solid var(--line);
    color: var(--muted);
    font-size: 0.68rem;
    letter-spacing: 0.04em;
  }

  @media (max-width: 46rem) {
    .panel {
      position: absolute;
      inset: 0 0 0 auto;
      z-index: 20;
      width: min(20rem, 100%);
      box-shadow: -0.5rem 0 1.5rem rgb(0 0 0 / 0.25);
    }
  }
</style>
