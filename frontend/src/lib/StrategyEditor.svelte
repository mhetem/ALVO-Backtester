<script lang="ts">
  import { onMount } from 'svelte';

  import RuleNode from './RuleNode.svelte';
  import { describe, HttpError } from './api';
  import { findEntry, type Catalog, type CatalogEntry } from './catalog';
  import type { User } from './session';
  import {
    blankRule,
    blankSpec,
    createStrategy,
    deleteStrategy,
    draftInput,
    fetchStrategies,
    flagged,
    inputNames,
    inputRefs,
    nextInputName,
    shareStrategy,
    shareURL,
    specFromJSON,
    specToJSON,
    unshareStrategy,
    updateStrategy,
    validateSpec,
    PRICE_FIELDS,
    SIZING_LABELS,
    SIZING_TYPES,
    SpecError,
    type InputDraft,
    type PlanSummary,
    type RuleDraft,
    type SavedStrategy,
    type Share,
    type SideDraft,
    type SizingType,
    type SpecDraft,
  } from './strategy';

  type Props = {
    catalog: Catalog | null;
    user: User | null;
    onClose: () => void;
    onHelp?: () => void;
  };

  let { catalog, user, onClose, onHelp }: Props = $props();

  let strategies = $state<SavedStrategy[]>([]);
  let activeId = $state<string | null>(null);
  let name = $state('');
  let description = $state('');
  let version = $state(1);
  let spec = $state<SpecDraft>(blankSpec());

  let tab = $state<'builder' | 'json'>('builder');
  let raw = $state('');
  let plan = $state<PlanSummary | null>(null);
  let error = $state<string | null>(null);
  let pointer = $state('');
  let note = $state<string | null>(null);
  let busy = $state(false);
  let share = $state<Share | null>(null);
  let copied = $state(false);

  const names = $derived(inputNames(spec));
  const refs = $derived(inputRefs(spec, catalog));
  const groups = $derived(catalog?.groups ?? []);

  const SIDES = [
    { key: 'long', hint: 'No long rule, so this strategy never buys to open.' },
    { key: 'short', hint: 'No short rule, so this strategy never sells to open.' },
  ] as const;

  function reset() {
    error = null;
    pointer = '';
    note = null;
  }

  function edit(next: SpecDraft) {
    spec = next;
    plan = null;
  }

  function startNew() {
    reset();
    activeId = null;
    name = '';
    description = '';
    share = null;
    version = 1;
    spec = blankSpec();
    plan = null;
    tab = 'builder';
  }

  function select(saved: SavedStrategy) {
    reset();
    activeId = saved.id;
    name = saved.name;
    description = saved.description;
    version = saved.version;
    plan = saved.plan ?? null;
    share = saved.share ?? null;

    try {
      spec = specFromJSON(saved.spec);
      tab = 'builder';
    } catch (cause) {
      raw = JSON.stringify(saved.spec, null, 2);
      tab = 'json';
      note = `${describe(cause)} — edit it as JSON.`;
    }
  }

  function toJSON() {
    if (tab === 'json') {
      return;
    }
    raw = JSON.stringify(specToJSON(spec), null, 2);
    tab = 'json';
    reset();
  }

  function toBuilder() {
    if (tab === 'builder') {
      return;
    }

    try {
      spec = specFromJSON(JSON.parse(raw));
      tab = 'builder';
      reset();
    } catch (cause) {
      error = describe(cause);
      pointer = '';
    }
  }

  function current(): unknown {
    if (tab === 'json') {
      return JSON.parse(raw);
    }
    return specToJSON(spec);
  }

  function fail(cause: unknown) {
    if (cause instanceof SpecError) {
      error = cause.message;
      pointer = cause.pointer;
      return;
    }
    if (cause instanceof HttpError && cause.status === 401) {
      error = 'Your session ended. Sign in again to validate or save.';
      pointer = '';
      return;
    }
    error = describe(cause);
    pointer = '';
  }

  async function run(work: () => Promise<void>) {
    if (busy) {
      return;
    }

    busy = true;
    reset();

    try {
      await work();
    } catch (cause) {
      fail(cause);
    } finally {
      busy = false;
    }
  }

  // A link is stored in the clear rather than hashed, because the editor has to be able to
  // show it again. It grants read access to one spec and nothing else, which is a different
  // thing from a credential.
  function toggleShare() {
    const id = activeId;
    if (!id) {
      return;
    }

    void run(async () => {
      if (share) {
        await unshareStrategy(id);
        share = null;
        note = 'That link no longer opens anything.';
        return;
      }

      share = await shareStrategy(id);
      note = 'Anyone with this link can read the spec.';
    });
  }

  function copyLink() {
    if (!share) {
      return;
    }

    void navigator.clipboard
      .writeText(shareURL(share))
      .then(() => {
        copied = true;
        setTimeout(() => (copied = false), 1500);
      })
      .catch((cause: unknown) => fail(cause));
  }

  function adopt(saved: SavedStrategy) {
    strategies = [...strategies.filter((held) => held.id !== saved.id), saved].sort((a, b) =>
      a.name.localeCompare(b.name),
    );
    activeId = saved.id;
    version = saved.version;
    plan = saved.plan ?? null;
    share = saved.share ?? null;
    note = `Saved as version ${saved.version}.`;
  }

  function validate() {
    void run(async () => {
      const checked = await validateSpec(current());
      plan = checked.plan;
      note = 'This strategy compiles.';
    });
  }

  function save(branch: boolean) {
    void run(async () => {
      const body = current();
      const id = branch ? null : activeId;
      adopt(
        id === null
          ? await createStrategy(name, description, body)
          : await updateStrategy(id, name, description, body),
      );
    });
  }

  function remove() {
    const id = activeId;
    if (!id) {
      return;
    }

    void run(async () => {
      await deleteStrategy(id);
      strategies = strategies.filter((held) => held.id !== id);
      startNew();
    });
  }

  function addInput() {
    const entry = findEntry(catalog, 'ema') ?? catalog?.indicators[0];
    if (!entry) {
      return;
    }
    edit({ ...spec, inputs: [...spec.inputs, draftInput(nextInputName(spec), entry)] });
  }

  function setInput(index: number, next: InputDraft) {
    edit({ ...spec, inputs: spec.inputs.map((held, i) => (i === index ? next : held)) });
  }

  function dropInput(index: number) {
    edit({ ...spec, inputs: spec.inputs.filter((_, i) => i !== index) });
  }

  function changeIndicator(index: number, indicator: string) {
    const entry = findEntry(catalog, indicator);
    if (entry) {
      setInput(index, draftInput(spec.inputs[index].name, entry));
    }
  }

  function setParam(index: number, param: string, text: string) {
    const value = Number(text);
    if (!Number.isFinite(value)) {
      return;
    }
    const held = spec.inputs[index];
    setInput(index, { ...held, params: { ...held.params, [param]: value } });
  }

  function entryOf(input: InputDraft): CatalogEntry | null {
    return findEntry(catalog, input.indicator);
  }

  function sourcesFor(input: InputDraft): string[] {
    return [...PRICE_FIELDS.filter((field) => field !== 'volume'), ...names.filter((held) => held !== input.name)];
  }

  function setSide(key: 'long' | 'short', patch: Partial<SideDraft>) {
    edit({ ...spec, [key]: { ...spec[key], ...patch } });
  }

  function toggleSide(key: 'long' | 'short') {
    // Dropping a side takes its exit with it: an exit for a position that can never open
    // is a rule the parser rejects rather than quietly ignores.
    edit(
      spec[key].entry
        ? { ...spec, [key]: { entry: null, exit: null } }
        : { ...spec, [key]: { entry: blankRule(refs), exit: null } },
    );
  }

  function toggleExit(key: 'long' | 'short') {
    setSide(key, { exit: spec[key].exit ? null : blankRule(refs) });
  }

  function setSizing(type: SizingType) {
    const value = type === 'fixed_qty' ? 100 : type === 'fixed_cash' ? 100000 : 0.95;
    edit({ ...spec, sizing: { type, value } });
  }

  function setSizingValue(text: string) {
    const value = Number(text);
    if (Number.isFinite(value)) {
      edit({ ...spec, sizing: { ...spec.sizing, value } });
    }
  }

  function setCost(field: 'brokerage_cents' | 'fee_bps' | 'slippage_bps', text: string) {
    const value = Number(text);
    if (!Number.isFinite(value)) {
      return;
    }

    const costs = { ...spec.costs };
    costs[field] = value;
    edit({ ...spec, costs });
  }

  function onKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      event.stopPropagation();
      onClose();
    }
  }

  onMount(() => {
    if (!user) {
      return;
    }

    void (async () => {
      try {
        strategies = await fetchStrategies();
      } catch (cause) {
        error = cause instanceof HttpError ? cause.message : describe(cause);
      }
    })();
  });
</script>

<svelte:window onkeydown={onKeydown} />

<div class="backdrop">
  <button type="button" class="scrim" aria-label="Close" onclick={onClose}></button>

  <div class="dialog" role="dialog" aria-modal="true" aria-label="Strategies">
    <header>
      <h2>
        Strategies
        {#if onHelp}
          <button type="button" class="help" onclick={onHelp} title="How strategies work">?</button>
        {/if}
      </h2>
      <div class="tabs" role="group" aria-label="Editor mode">
        <button type="button" class:on={tab === 'builder'} onclick={toBuilder}>Builder</button>
        <button type="button" class:on={tab === 'json'} onclick={toJSON}>JSON</button>
      </div>
      <button type="button" class="close" onclick={onClose} aria-label="Close">×</button>
    </header>

    <div class="body">
      <aside>
        {#if user}
          <button type="button" class="new" onclick={startNew}>New strategy</button>
          <ul>
            {#each strategies as saved (saved.id)}
              <li>
                <button type="button" class:on={saved.id === activeId} onclick={() => select(saved)}>
                  <span class="title">{saved.name}</span>
                  <span class="meta">v{saved.version}</span>
                </button>
              </li>
            {:else}
              <li class="none">Nothing saved yet.</li>
            {/each}
          </ul>
        {:else}
          <p class="none">
            Sign in to validate a strategy against the server and to keep it. The builder works
            either way — the JSON tab shows exactly what would be saved.
          </p>
        {/if}
      </aside>

      <section>
        <div class="identity">
          <label>
            <span>Name</span>
            <input
              bind:value={name}
              type="text"
              maxlength="80"
              spellcheck="false"
              placeholder="Name this strategy"
            />
          </label>
          <label class="wide">
            <span>Description</span>
            <input bind:value={description} type="text" maxlength="500" />
          </label>
          {#if activeId}
            <span class="version">version {version}</span>
          {/if}
        </div>

        {#if activeId}
          <div class="share">
            <button type="button" class="plain" onclick={toggleShare} disabled={busy}>
              {share ? 'Stop sharing' : 'Share a read-only link'}
            </button>
            {#if share}
              <input class="link" type="text" readonly value={shareURL(share)} />
              <button type="button" class="plain" onclick={copyLink}>
                {copied ? 'Copied' : 'Copy'}
              </button>
            {/if}
          </div>
        {/if}

        {#if tab === 'json'}
          <label class="raw">
            <span>Spec</span>
            <textarea bind:value={raw} spellcheck="false" rows="24"></textarea>
          </label>
        {:else}
          <div class="block">
            <h3>Inputs</h3>
            {#each spec.inputs as input, index (index)}
              {@const entry = entryOf(input)}
              <div class="input" class:flagged={flagged(pointer, `/inputs/${input.name}`)}>
                <input
                  class="who"
                  type="text"
                  value={input.name}
                  spellcheck="false"
                  aria-label="Input name"
                  oninput={(event) => setInput(index, { ...input, name: event.currentTarget.value })}
                />

                <select
                  value={input.indicator}
                  onchange={(event) => changeIndicator(index, event.currentTarget.value)}
                  aria-label="Indicator"
                >
                  {#each groups as group (group)}
                    <optgroup label={group}>
                      {#each catalog?.indicators.filter((held) => held.group === group) ?? [] as held (held.name)}
                        <option value={held.name}>{held.title}</option>
                      {/each}
                    </optgroup>
                  {/each}
                </select>

                {#each entry?.params ?? [] as param (param.name)}
                  <label class="param">
                    <input
                      type="number"
                      min={param.min}
                      max={param.max}
                      step={param.kind === 'int' ? 1 : 'any'}
                      value={input.params[param.name] ?? param.default}
                      oninput={(event) => setParam(index, param.name, event.currentTarget.value)}
                    />
                    <span>{param.name}</span>
                  </label>
                {/each}

                {#if entry?.sourced}
                  <select
                    value={input.source || 'close'}
                    onchange={(event) => setInput(index, { ...input, source: event.currentTarget.value })}
                    aria-label="Source"
                  >
                    {#each sourcesFor(input) as source (source)}
                      <option value={source}>{source}</option>
                    {/each}
                  </select>
                {/if}

                {#if entry && entry.outputs.length > 1}
                  <select
                    value={input.output || entry.outputs[0]}
                    onchange={(event) => setInput(index, { ...input, output: event.currentTarget.value })}
                    aria-label="Output"
                  >
                    {#each entry.outputs as output (output)}
                      <option value={output}>{output}</option>
                    {/each}
                  </select>
                {/if}

                <span class="spacer"></span>
                <button type="button" class="drop" onclick={() => dropInput(index)} aria-label="Remove input">
                  ×
                </button>
              </div>
            {/each}

            {#if spec.inputs.length === 0}
              <p class="hint">
                An input is one indicator this strategy reads. Add the ones its rules compare.
              </p>
            {/if}

            <button type="button" class="add" onclick={addInput}>Add an input</button>
          </div>

          {#each SIDES as side (side.key)}
            {@const leg = spec[side.key]}
            <div class="block">
              <h3>
                Enter {side.key} when
                <button type="button" class="toggle" onclick={() => toggleSide(side.key)}>
                  {leg.entry ? 'remove' : 'add'}
                </button>
              </h3>
              {#if leg.entry}
                <RuleNode
                  rule={leg.entry}
                  names={refs}
                  fault={pointer}
                  brackets={false}
                  at={`/entry/${side.key}`}
                  onChange={(next: RuleDraft) => setSide(side.key, { entry: next })}
                  onRemove={null}
                />
              {:else}
                <p class="hint">{side.hint}</p>
              {/if}
            </div>

            {#if leg.entry}
              <div class="block">
                <h3>
                  Exit the {side.key} when
                  <button type="button" class="toggle" onclick={() => toggleExit(side.key)}>
                    {leg.exit ? 'remove' : 'add'}
                  </button>
                </h3>
                {#if leg.exit}
                  <RuleNode
                    rule={leg.exit}
                    names={refs}
                    fault={pointer}
                    brackets={true}
                    at={`/exit/${side.key}`}
                    onChange={(next: RuleDraft) => setSide(side.key, { exit: next })}
                    onRemove={null}
                  />
                {:else}
                  <p class="hint">
                    With no exit rule the position is held to the end of the backtest — which is how
                    you write buy and hold.
                  </p>
                {/if}
              </div>
            {/if}
          {/each}

          <div class="block row">
            <div class="field" class:flagged={flagged(pointer, '/sizing')}>
              <h3>Size each position as</h3>
              <div class="line">
                <select
                  value={spec.sizing.type}
                  onchange={(event) => setSizing(event.currentTarget.value as SizingType)}
                  aria-label="Sizing"
                >
                  {#each SIZING_TYPES as type (type)}
                    <option value={type}>{SIZING_LABELS[type]}</option>
                  {/each}
                </select>
                <input
                  type="number"
                  step="any"
                  value={spec.sizing.value}
                  oninput={(event) => setSizingValue(event.currentTarget.value)}
                  aria-label="Sizing value"
                />
              </div>
            </div>

            <div class="field" class:flagged={flagged(pointer, '/costs')}>
              <h3>Costs</h3>
              <div class="line">
                <label class="param">
                  <input
                    type="number"
                    min="0"
                    step="1"
                    value={spec.costs.brokerage_cents}
                    oninput={(event) => setCost('brokerage_cents', event.currentTarget.value)}
                  />
                  <span>brokerage, cents</span>
                </label>
                <label class="param">
                  <input
                    type="number"
                    min="0"
                    step="0.05"
                    value={spec.costs.fee_bps}
                    oninput={(event) => setCost('fee_bps', event.currentTarget.value)}
                  />
                  <span>fees, bps</span>
                </label>
                <label class="param">
                  <input
                    type="number"
                    min="0"
                    step="0.5"
                    value={spec.costs.slippage_bps}
                    oninput={(event) => setCost('slippage_bps', event.currentTarget.value)}
                  />
                  <span>slippage, bps</span>
                </label>
              </div>
            </div>
          </div>
        {/if}
      </section>
    </div>

    <footer>
      {#if error}
        <p class="says bad">
          {error}
          {#if pointer}<code>{pointer}</code>{/if}
        </p>
      {:else if note}
        <p class="says">{note}</p>
      {:else if plan}
        <p class="says">
          {plan.indicators.length} indicator{plan.indicators.length === 1 ? '' : 's'} ·
          {plan.slots} line{plan.slots === 1 ? '' : 's'} ·
          {plan.warmup} bars of warmup · reads {plan.depth} back
          {#each SIDES as side (side.key)}
            {#if plan[side.key].trades}
              · {side.key}{plan[side.key].stop_loss ? ' + stop' : ''}{plan[side.key].take_profit
                ? ' + target'
                : ''}
            {/if}
          {/each}
        </p>
      {:else if !user}
        <p class="says">Sign in to validate or save. The builder works either way.</p>
      {:else}
        <p class="says">Validate to compile this against the indicator library.</p>
      {/if}

      <span class="spacer"></span>

      <button type="button" onclick={validate} disabled={busy || !user}>Validate</button>
      <button type="button" onclick={() => save(false)} disabled={busy || !user}>
        {activeId ? 'Save' : 'Create'}
      </button>
      {#if activeId}
        <button type="button" onclick={() => save(true)} disabled={busy || !user}>Save as new</button>
        <button type="button" class="danger" onclick={remove} disabled={busy || !user}>Delete</button>
      {/if}
    </footer>
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
    width: min(62rem, 100%);
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
    gap: 1rem;
    padding: 0.7rem 0.9rem;
    border-bottom: 1px solid var(--line);
  }

  header h2 {
    margin: 0;
    font-size: 0.95rem;
    font-weight: 600;
  }

  .tabs {
    display: flex;
    gap: 0.2rem;
    margin-left: auto;
    padding: 0.2rem;
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: 999px;
  }

  .tabs button {
    font: inherit;
    font-size: 0.75rem;
    font-weight: 500;
    color: var(--muted);
    background: none;
    border: 0;
    border-radius: 999px;
    padding: 0.2rem 0.7rem;
    cursor: pointer;
  }

  .tabs button.on {
    color: var(--accent-fg);
    background: var(--accent);
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
    font-size: 1.2rem;
    line-height: 1;
    color: var(--muted);
    background: none;
    border: 0;
    padding: 0.2rem 0.35rem;
    cursor: pointer;
  }

  .body {
    display: flex;
    min-height: 0;
    flex: 1;
  }

  aside {
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
    width: 13rem;
    flex-shrink: 0;
    padding: 0.7rem;
    border-right: 1px solid var(--line);
    overflow-y: auto;
  }

  aside ul {
    margin: 0;
    padding: 0;
    list-style: none;
  }

  aside li button {
    display: flex;
    align-items: baseline;
    gap: 0.4rem;
    width: 100%;
    font: inherit;
    font-size: 0.8rem;
    color: inherit;
    text-align: left;
    background: none;
    border: 0;
    border-radius: 0.25rem;
    padding: 0.3rem 0.4rem;
    cursor: pointer;
  }

  aside li button:hover {
    background: var(--hover);
  }

  aside li button.on {
    color: var(--accent);
    font-weight: 600;
  }

  .new {
    font: inherit;
    font-size: 0.75rem;
    font-weight: 600;
    color: var(--accent-fg);
    background: var(--accent);
    border: 0;
    border-radius: 0.3rem;
    padding: 0.3rem 0.6rem;
    cursor: pointer;
  }

  .none {
    margin: 0;
    padding: 0.4rem;
    color: var(--muted);
    font-size: 0.75rem;
    line-height: 1.45;
  }

  section {
    display: flex;
    flex-direction: column;
    gap: 0.9rem;
    flex: 1;
    min-width: 0;
    padding: 0.9rem;
    overflow-y: auto;
  }

  .identity {
    display: flex;
    flex-wrap: wrap;
    align-items: flex-end;
    gap: 0.6rem;
  }

  .identity label {
    display: flex;
    flex-direction: column;
    gap: 0.2rem;
  }

  .identity label.wide {
    flex: 1;
    min-width: 12rem;
  }

  .identity span {
    color: var(--muted);
    font-size: 0.68rem;
    letter-spacing: 0.06em;
    text-transform: uppercase;
  }

  .version {
    color: var(--muted);
    font-size: 0.7rem;
    padding-bottom: 0.35rem;
  }

  .share {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    flex-wrap: wrap;
  }

  .share .plain {
    font: inherit;
    font-size: 0.72rem;
    padding: 0.25rem 0.6rem;
    border: 1px solid var(--line);
    border-radius: 4px;
    background: var(--panel);
    color: var(--muted);
    cursor: pointer;
  }

  .share .plain:hover:not(:disabled) {
    color: var(--fg);
  }

  .share .plain:disabled {
    opacity: 0.5;
    cursor: default;
  }

  .share .link {
    flex: 1 1 18rem;
    min-width: 0;
    font: inherit;
    font-size: 0.75rem;
    padding: 0.25rem 0.45rem;
    border: 1px solid var(--line);
    border-radius: 4px;
    background: var(--bg);
    color: var(--muted);
  }

  h3 {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    margin: 0 0 0.4rem;
    color: var(--muted);
    font-size: 0.68rem;
    font-weight: 600;
    letter-spacing: 0.08em;
    text-transform: uppercase;
  }

  .block.row {
    display: flex;
    flex-wrap: wrap;
    gap: 1rem;
  }

  .field {
    flex: 1;
    min-width: 15rem;
    border-radius: 0.35rem;
  }

  .field.flagged {
    outline: 2px solid color-mix(in srgb, var(--bad) 45%, transparent);
    outline-offset: 0.3rem;
  }

  .line {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0.4rem;
  }

  .input {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0.35rem;
    padding: 0.35rem 0.4rem;
    border: 1px solid var(--line);
    border-radius: 0.35rem;
    margin-bottom: 0.35rem;
  }

  .input.flagged {
    border-color: var(--bad);
    box-shadow: 0 0 0 2px color-mix(in srgb, var(--bad) 18%, transparent);
  }

  .who {
    width: 7rem;
    font-weight: 600;
  }

  select,
  input,
  textarea {
    font: inherit;
    font-size: 0.78rem;
    color: inherit;
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: 0.25rem;
    padding: 0.2rem 0.4rem;
  }

  select:focus,
  input:focus,
  textarea:focus {
    outline: none;
    border-color: var(--accent);
  }

  input[type='number'] {
    width: 5rem;
    font-variant-numeric: tabular-nums;
  }

  .param {
    display: inline-flex;
    align-items: center;
    gap: 0.25rem;
  }

  .param span {
    color: var(--muted);
    font-size: 0.68rem;
  }

  .raw {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
    flex: 1;
  }

  .raw span {
    color: var(--muted);
    font-size: 0.68rem;
    letter-spacing: 0.06em;
    text-transform: uppercase;
  }

  textarea {
    width: 100%;
    min-height: 22rem;
    resize: vertical;
    font-family: ui-monospace, SFMono-Regular, 'SF Mono', Menlo, monospace;
    font-size: 0.75rem;
    line-height: 1.5;
  }

  .hint {
    margin: 0;
    color: var(--muted);
    font-size: 0.75rem;
    line-height: 1.5;
  }

  .spacer {
    flex: 1;
  }

  .drop {
    font: inherit;
    font-size: 0.95rem;
    line-height: 1;
    color: var(--muted);
    background: none;
    border: 0;
    padding: 0.1rem 0.3rem;
    cursor: pointer;
  }

  .drop:hover {
    color: var(--bad);
  }

  .add,
  .toggle {
    font: inherit;
    font-size: 0.72rem;
    font-weight: 500;
    color: var(--accent);
    background: none;
    border: 1px dashed var(--line);
    border-radius: 0.25rem;
    padding: 0.2rem 0.55rem;
    cursor: pointer;
  }

  .toggle {
    text-transform: none;
    letter-spacing: 0;
  }

  footer {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0.5rem;
    padding: 0.6rem 0.9rem;
    border-top: 1px solid var(--line);
    background: var(--panel);
  }

  footer button {
    font: inherit;
    font-size: 0.75rem;
    font-weight: 500;
    color: inherit;
    background: var(--bg);
    border: 1px solid var(--line);
    border-radius: 0.3rem;
    padding: 0.25rem 0.7rem;
    cursor: pointer;
  }

  footer button:hover:not(:disabled) {
    border-color: var(--accent);
    color: var(--accent);
  }

  footer button:disabled {
    opacity: 0.45;
    cursor: not-allowed;
  }

  footer button.danger:hover:not(:disabled) {
    border-color: var(--bad);
    color: var(--bad);
  }

  .says {
    margin: 0;
    color: var(--muted);
    font-size: 0.75rem;
  }

  .says.bad {
    color: var(--bad);
  }

  .says code {
    margin-left: 0.35rem;
  }

  @media (max-width: 52rem) {
    .body {
      flex-direction: column;
    }

    aside {
      width: auto;
      border-right: 0;
      border-bottom: 1px solid var(--line);
    }
  }
</style>
