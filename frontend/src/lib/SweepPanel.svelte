<script lang="ts">
  import { onDestroy, onMount, untrack } from 'svelte';

  import { describe, HttpError, TIMEFRAMES, type Timeframe } from './api';
  import { formatCents, formatPct, formatRatio, formatSignedPct } from './format';
  import {
    fetchStrategies,
    specFromJSON,
    type SavedStrategy,
    type SpecDraft,
  } from './strategy';
  import {
    axisSize,
    deleteSweep,
    fetchSweep,
    fetchSweeps,
    finishedOf,
    gridSize,
    KIND_LABELS,
    launchSweep,
    MAX_AXES,
    OBJECTIVE_LABELS,
    OBJECTIVES,
    pathLabel,
    running,
    sweepablePaths,
    SWEEP_KINDS,
    valueAt,
    winnerOf,
    type AxisDraft,
    type Objective,
    type Sweep,
    type SweepKind,
    type SweepRun,
  } from './sweep';
  import type { User } from './session';

  const POLL_MS = 2000;

  type Props = {
    symbol: string;
    timeframe: Timeframe;
    user: User | null;
    onClose: () => void;
  };

  let { symbol, timeframe, user, onClose }: Props = $props();

  type Cell = { row: number; col: number; run: SweepRun | null };

  let strategies = $state<SavedStrategy[]>([]);
  let sweeps = $state<Sweep[]>([]);
  let active = $state<Sweep | null>(null);
  let picked = $state<SweepRun | null>(null);

  let strategyId = $state('');
  let sweepSymbols = $state(untrack(() => symbol));
  let sweepTimeframe = $state<Timeframe>(untrack(() => timeframe));
  let start = $state(defaultStart());
  let end = $state(today());
  let capital = $state(100000);
  let maxPositions = $state(1);
  let kind = $state<SweepKind>('grid');
  let objective = $state<Objective>('sharpe');
  let inSampleDays = $state(180);
  let outOfSampleDays = $state(60);
  let axes = $state<AxisDraft[]>([]);

  let error = $state<string | null>(null);
  let busy = $state(false);
  let timer: ReturnType<typeof setTimeout> | null = null;

  const chosen = $derived(strategies.find((saved) => saved.id === strategyId) ?? null);
  const draft = $derived(specOf(chosen));
  const paths = $derived(draft ? sweepablePaths(draft) : []);
  const basket = $derived(
    sweepSymbols
      .split(',')
      .map((ticker) => ticker.trim().toUpperCase())
      .filter((ticker) => ticker !== ''),
  );
  const points = $derived(axes.length === 0 ? 0 : gridSize(axes));
  const folds = $derived(kind === 'walk_forward' ? foldCount() : 0);
  const total = $derived(kind === 'walk_forward' ? points * folds : points);

  const canRun = $derived(
    user !== null &&
      strategyId !== '' &&
      basket.length > 0 &&
      axes.length > 0 &&
      points > 0 &&
      !busy,
  );

  const rowAxis = $derived(active?.axes[0] ?? null);
  const colAxis = $derived(active?.axes[1] ?? null);
  const heat = $derived(heatmapOf(active));
  const span = $derived(spanOf(heat));

  function today(): string {
    return new Date().toISOString().slice(0, 10);
  }

  function defaultStart(): string {
    const at = new Date();
    at.setFullYear(at.getFullYear() - 3);
    return at.toISOString().slice(0, 10);
  }

  function specOf(saved: SavedStrategy | null): SpecDraft | null {
    if (!saved) {
      return null;
    }
    try {
      return specFromJSON(saved.spec);
    } catch {
      return null;
    }
  }

  function foldCount(): number {
    const from = new Date(start).getTime();
    const to = new Date(end).getTime();
    if (Number.isNaN(from) || Number.isNaN(to) || inSampleDays < 5 || outOfSampleDays < 5) {
      return 0;
    }

    const days = Math.floor((to - from) / 86400000) + 1;
    if (days < inSampleDays + outOfSampleDays) {
      return 0;
    }

    return Math.min(Math.floor((days - inSampleDays - outOfSampleDays) / outOfSampleDays) + 1, 12);
  }

  function addAxis() {
    const taken = new Set(axes.map((axis) => axis.path));
    const path = paths.find((candidate) => !taken.has(candidate));
    if (!path || !draft) {
      return;
    }

    const at = valueAt(draft, path);
    const step = at >= 4 ? Math.max(1, Math.round(at / 4)) : Math.max(at / 4, 0.01);
    axes = [...axes, { path, from: at, to: at + step * 3, step }];
  }

  function setAxis(index: number, patch: Partial<AxisDraft>) {
    axes = axes.map((axis, i) => (i === index ? { ...axis, ...patch } : axis));
  }

  function dropAxis(index: number) {
    axes = axes.filter((_, i) => i !== index);
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

  async function loadSweeps() {
    if (!user) {
      sweeps = [];
      return;
    }
    try {
      sweeps = await fetchSweeps();
    } catch (cause) {
      error = describe(cause);
    }
  }

  async function select(held: Sweep) {
    stopPolling();
    error = null;
    picked = null;

    try {
      active = await fetchSweep(held.id);
      if (running(active)) {
        poll(active.id);
      }
    } catch (cause) {
      error = describe(cause);
    }
  }

  function poll(id: string) {
    stopPolling();
    timer = setTimeout(() => void tick(id), POLL_MS);
  }

  async function tick(id: string) {
    try {
      const held = await fetchSweep(id);
      if (active?.id !== id) {
        return;
      }

      active = held;
      sweeps = sweeps.map((row) => (row.id === held.id ? { ...row, progress: held.progress } : row));

      if (running(held)) {
        poll(id);
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
      const held = await launchSweep({
        strategy_id: strategyId,
        kind,
        objective,
        symbols: basket,
        timeframe: sweepTimeframe,
        start,
        end,
        capital_cents: Math.round(capital * 100),
        max_positions: Math.min(maxPositions, basket.length),
        axes,
        ...(kind === 'walk_forward'
          ? { in_sample_days: inSampleDays, out_of_sample_days: outOfSampleDays }
          : {}),
      });

      sweeps = [held, ...sweeps];
      await select(held);
    } catch (cause) {
      error =
        cause instanceof HttpError && cause.status === 401
          ? 'Sign in to run a sweep.'
          : describe(cause);
    } finally {
      busy = false;
    }
  }

  async function remove(held: Sweep) {
    try {
      await deleteSweep(held.id);
      sweeps = sweeps.filter((row) => row.id !== held.id);
      if (active?.id === held.id) {
        stopPolling();
        active = null;
      }
    } catch (cause) {
      error = describe(cause);
    }
  }

  // The heatmap is drawn over the first two axes; a third is collapsed by keeping the best
  // point behind each cell, which is the number you would act on anyway.
  function heatmapOf(held: Sweep | null): Cell[][] {
    if (!held || held.axes.length === 0) {
      return [];
    }

    const rows = held.axes[0].values;
    const cols = held.axes[1]?.values ?? [0];
    const best = new Map<string, SweepRun>();

    for (const run of finishedOf(held)) {
      if (held.kind === 'walk_forward' && run.phase !== 'in_sample') {
        continue;
      }
      if (run.score === undefined) {
        continue;
      }

      const row = run.params[held.axes[0].path];
      const col = held.axes[1] ? run.params[held.axes[1].path] : 0;
      const key = `${row}|${col}`;

      const standing = best.get(key);
      if (!standing || (standing.score ?? -Infinity) < run.score) {
        best.set(key, run);
      }
    }

    return rows.map((row) =>
      cols.map((col) => ({ row, col, run: best.get(`${row}|${col}`) ?? null })),
    );
  }

  function spanOf(cells: Cell[][]): number {
    let widest = 0;
    for (const row of cells) {
      for (const cell of row) {
        widest = Math.max(widest, Math.abs(cell.run?.score ?? 0));
      }
    }
    return widest;
  }

  function shade(cell: Cell): string {
    const score = cell.run?.score;
    if (score === undefined || span === 0) {
      return 'var(--panel)';
    }

    const weight = Math.min(Math.round((Math.abs(score) / span) * 80), 80);
    const tone = score >= 0 ? 'var(--good)' : 'var(--bad)';
    return `color-mix(in srgb, ${tone} ${weight}%, var(--panel))`;
  }

  type FoldRow = {
    fold: number;
    winner: SweepRun | null;
    test: SweepRun | null;
    settled: boolean;
  };

  function foldsOf(held: Sweep): FoldRow[] {
    return (held.folds ?? []).map((plan) => {
      const runs = (held.runs ?? []).filter((run) => run.fold === plan.fold);
      const trained = runs.filter((run) => run.phase === 'in_sample');

      return {
        fold: plan.fold,
        winner: winnerOf(trained.filter((run) => run.status === 'done')),
        test: runs.find((run) => run.phase === 'out_of_sample') ?? null,
        // A fold whose grid has stopped but produced no winner is finished, not pending:
        // nothing it tried ever opened a position, so there is nothing to test.
        settled: trained.length > 0 && trained.every((run) => run.status === 'done' || run.status === 'error'),
      };
    });
  }

  // The number a walk-forward exists to produce: what the strategy returned over windows it
  // was never tuned on, compounded across every fold that has finished.
  function outOfSampleReturn(held: Sweep): number | null {
    const tested = (held.runs ?? []).filter(
      (run) => run.phase === 'out_of_sample' && run.status === 'done' && run.return_pct !== undefined,
    );
    if (tested.length === 0) {
      return null;
    }

    let growth = 1;
    for (const run of tested) {
      growth *= 1 + (run.return_pct ?? 0) / 100;
    }

    return (growth - 1) * 100;
  }

  function describeParams(run: SweepRun | null): string {
    if (!run) {
      return '—';
    }
    return Object.entries(run.params)
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([path, value]) => `${pathLabel(path)} ${value}`)
      .join(' · ');
  }

  function onKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      event.stopPropagation();
      onClose();
    }
  }

  onMount(() => {
    void loadStrategies();
    void loadSweeps();
  });

  onDestroy(stopPolling);
</script>

<svelte:window onkeydown={onKeydown} />

<div class="backdrop">
  <button type="button" class="scrim" aria-label="Close" onclick={onClose}></button>

  <div class="dialog" role="dialog" aria-modal="true" aria-label="Sweeps">
    <header>
      <h2>Sweeps</h2>
      <button type="button" class="close" onclick={onClose} aria-label="Close">×</button>
    </header>

    {#if !user}
      <p class="hint">Sign in to sweep a strategy across a range of parameters.</p>
    {:else}
      <form
        class="launch"
        onsubmit={(event) => {
          event.preventDefault();
          void launch();
        }}
      >
        <div class="row">
          <label>
            <span>Strategy</span>
            <select bind:value={strategyId} onchange={() => (axes = [])}>
              {#each strategies as saved (saved.id)}
                <option value={saved.id}>{saved.name}</option>
              {:else}
                <option value="">No strategies saved yet</option>
              {/each}
            </select>
          </label>

          <label>
            <span>Symbols</span>
            <input bind:value={sweepSymbols} spellcheck="false" autocapitalize="characters" />
          </label>

          <label class="short">
            <span>Timeframe</span>
            <select bind:value={sweepTimeframe}>
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
              <input type="number" min="1" max={basket.length} bind:value={maxPositions} />
            </label>
          {/if}
        </div>

        <div class="row">
          <label class="short">
            <span>Kind</span>
            <select bind:value={kind}>
              {#each SWEEP_KINDS as option (option)}
                <option value={option}>{KIND_LABELS[option]}</option>
              {/each}
            </select>
          </label>

          <label class="short">
            <span>Rank by</span>
            <select bind:value={objective}>
              {#each OBJECTIVES as option (option)}
                <option value={option}>{OBJECTIVE_LABELS[option]}</option>
              {/each}
            </select>
          </label>

          {#if kind === 'walk_forward'}
            <label class="short">
              <span>Train days</span>
              <input type="number" min="5" step="5" bind:value={inSampleDays} />
            </label>
            <label class="short">
              <span>Test days</span>
              <input type="number" min="5" step="5" bind:value={outOfSampleDays} />
            </label>
          {/if}

          <span class="tally" class:bad={total === 0}>
            {#if kind === 'walk_forward'}
              {folds} fold{folds === 1 ? '' : 's'} × {points} point{points === 1 ? '' : 's'} =
              {total} run{total === 1 ? '' : 's'}
            {:else}
              {total} run{total === 1 ? '' : 's'}
            {/if}
          </span>

          <button type="submit" class="run" disabled={!canRun}>
            {busy ? 'Queueing…' : 'Sweep'}
          </button>
        </div>

        <div class="axes">
          {#each axes as axis, i (i)}
            <div class="axis">
              <select
                value={axis.path}
                onchange={(event) => setAxis(i, { path: event.currentTarget.value })}
              >
                {#each paths as path (path)}
                  <option value={path} disabled={path !== axis.path && axes.some((held) => held.path === path)}>
                    {pathLabel(path)}
                  </option>
                {/each}
              </select>

              <label>
                <span>from</span>
                <input
                  type="number"
                  step="any"
                  value={axis.from}
                  onchange={(event) => setAxis(i, { from: Number(event.currentTarget.value) })}
                />
              </label>
              <label>
                <span>to</span>
                <input
                  type="number"
                  step="any"
                  value={axis.to}
                  onchange={(event) => setAxis(i, { to: Number(event.currentTarget.value) })}
                />
              </label>
              <label>
                <span>step</span>
                <input
                  type="number"
                  step="any"
                  min="0"
                  value={axis.step}
                  onchange={(event) => setAxis(i, { step: Number(event.currentTarget.value) })}
                />
              </label>

              <span class="count" class:bad={axisSize(axis) === 0}>{axisSize(axis)} values</span>

              <button type="button" class="plain" onclick={() => dropAxis(i)} aria-label="Remove axis">
                ×
              </button>
            </div>
          {/each}

          {#if axes.length < MAX_AXES && paths.length > axes.length}
            <button type="button" class="plain add" onclick={addAxis}>+ axis</button>
          {/if}
          {#if axes.length === 0}
            <span class="hint inline">Add an axis to say which number varies.</span>
          {/if}
        </div>
      </form>

      {#if error}
        <p class="error">{error}</p>
      {/if}

      <div class="body">
        <ol class="list">
          {#each sweeps as held (held.id)}
            <li>
              <button type="button" class:on={held.id === active?.id} onclick={() => void select(held)}>
                <span class="who">{held.symbols.join(', ')} · {held.timeframe}</span>
                <span class="what">{KIND_LABELS[held.kind]} · {held.points} pts</span>
                <span class="when">{held.start} → {held.end}</span>
                <span class="how" class:live={running(held)}>
                  {held.progress.done}/{held.progress.total}
                </span>
              </button>
            </li>
          {:else}
            <li class="hint">No sweeps yet.</li>
          {/each}
        </ol>

        <div class="detail">
          {#if !active}
            <p class="hint">Pick a sweep, or start one above.</p>
          {:else}
            <div class="progress">
              <strong>{OBJECTIVE_LABELS[active.objective] ?? active.objective}</strong>
              <span>
                {active.progress.done} done · {active.progress.running} running ·
                {active.progress.queued} queued{active.progress.failed > 0
                  ? ` · ${active.progress.failed} failed`
                  : ''}
              </span>
              <button
                type="button"
                class="plain"
                onclick={() => {
                  if (active) {
                    void remove(active);
                  }
                }}
              >
                Delete
              </button>
            </div>

            {#if active.kind === 'walk_forward'}
              {@const tested = outOfSampleReturn(active)}
              <p class="lede">
                Each fold is tuned on its training window and then run once, unseen, on the window
                after it.
                {#if tested !== null}
                  Compounded out of sample: <strong>{formatSignedPct(tested)}</strong>.
                {/if}
              </p>

              <table class="folds">
                <thead>
                  <tr>
                    <th>Fold</th>
                    <th>Trained on</th>
                    <th>Winner</th>
                    <th class="num">In sample</th>
                    <th>Tested on</th>
                    <th class="num">Out of sample</th>
                  </tr>
                </thead>
                <tbody>
                  {#each foldsOf(active) as row (row.fold)}
                    {@const plan = active.folds?.[row.fold]}
                    <tr>
                      <td>{row.fold + 1}</td>
                      <td class="dates">{plan?.in_start} → {plan?.in_end}</td>
                      <td class="params">{describeParams(row.winner)}</td>
                      <td class="num">
                        {row.winner?.score !== undefined ? formatRatio(row.winner.score) : '—'}
                      </td>
                      <td class="dates">{plan?.out_start} → {plan?.out_end}</td>
                      <td
                        class="num"
                        class:good={(row.test?.return_pct ?? 0) > 0}
                        class:bad={(row.test?.return_pct ?? 0) < 0}
                      >
                        {#if row.test?.status === 'done' && row.test.return_pct !== undefined}
                          {formatSignedPct(row.test.return_pct)}
                        {:else if row.test}
                          {row.test.status}
                        {:else if row.settled && !row.winner}
                          nothing traded
                        {:else}
                          waiting
                        {/if}
                      </td>
                    </tr>
                  {/each}
                </tbody>
              </table>
            {:else if heat.length > 0}
              <div class="map" style={`--cols: ${(colAxis?.values ?? [0]).length}`}>
                <div class="corner">
                  {colAxis ? pathLabel(colAxis.path) : ''}
                </div>
                {#each colAxis?.values ?? [0] as col (col)}
                  <div class="head">{colAxis ? col : 'value'}</div>
                {/each}

                {#each heat as row, i (i)}
                  <div class="side">{rowAxis?.values[i]}</div>
                  {#each row as cell (`${cell.row}|${cell.col}`)}
                    <button
                      type="button"
                      class="cell"
                      class:on={picked?.id === cell.run?.id && cell.run !== null}
                      style={`background: ${shade(cell)}`}
                      disabled={cell.run === null}
                      onclick={() => (picked = cell.run)}
                    >
                      {cell.run?.score !== undefined ? formatRatio(cell.run.score) : '·'}
                    </button>
                  {/each}
                {/each}
              </div>

              <p class="axis-note">
                Rows: {rowAxis ? pathLabel(rowAxis.path) : ''}{#if active.axes.length > 2}
                  · {active.axes.length - 2} further
                  {active.axes.length === 3 ? 'axis is' : 'axes are'} collapsed to the best point
                  behind each cell{/if}
              </p>

              {#if picked}
                <dl class="picked">
                  <dt>Parameters</dt>
                  <dd>{describeParams(picked)}</dd>
                  <dt>{OBJECTIVE_LABELS[active.objective] ?? active.objective}</dt>
                  <dd>{picked.score !== undefined ? formatRatio(picked.score) : '—'}</dd>
                  <dt>Return</dt>
                  <dd>{picked.return_pct !== undefined ? formatSignedPct(picked.return_pct) : '—'}</dd>
                  <dt>Trades</dt>
                  <dd>{picked.trades}</dd>
                </dl>
              {:else}
                <p class="hint">
                  Every cell is one backtest. Empty cells never traded, so they are not ranked.
                </p>
              {/if}
            {:else}
              <p class="hint">Nothing has finished yet.</p>
            {/if}

            <p class="footnote">
              {active.symbols.join(', ')} · {active.timeframe} · {formatCents(active.capital_cents)}
              {#if active.max_positions > 1}
                · at most {active.max_positions} held at once{/if}
              {#if active.progress.failed > 0}
                · {formatPct((active.progress.failed / Math.max(active.progress.total, 1)) * 100, 0)}
                of the runs failed{/if}
            </p>
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
    width: min(84rem, 100%);
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

  .launch {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }

  .row {
    display: flex;
    flex-wrap: wrap;
    align-items: end;
    gap: 0.5rem;
  }

  .launch label {
    display: flex;
    flex-direction: column;
    gap: 0.2rem;
    flex: 1 1 10rem;
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
    width: 6.5rem;
  }

  .tally {
    font-size: 0.75rem;
    color: var(--muted);
    padding-bottom: 0.4rem;
    margin-left: auto;
    font-variant-numeric: tabular-nums;
  }

  .tally.bad,
  .count.bad {
    color: var(--bad);
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

  .axes {
    display: flex;
    flex-direction: column;
    gap: 0.35rem;
  }

  .axis {
    display: flex;
    align-items: end;
    gap: 0.4rem;
    padding: 0.35rem 0.5rem;
    border: 1px solid var(--line);
    border-radius: 4px;
    background: var(--panel);
  }

  .axis > select {
    font: inherit;
    font-size: 0.8rem;
    padding: 0.28rem 0.4rem;
    border: 1px solid var(--line);
    border-radius: 4px;
    background: var(--bg);
    color: var(--fg);
    flex: 0 1 16rem;
  }

  .axis label {
    flex: 0 0 auto;
    flex-direction: row;
    align-items: center;
    gap: 0.3rem;
  }

  .axis input[type='number'] {
    width: 5.5rem;
  }

  .count {
    font-size: 0.72rem;
    color: var(--muted);
    margin-left: auto;
    font-variant-numeric: tabular-nums;
  }

  .plain {
    font: inherit;
    font-size: 0.78rem;
    padding: 0.25rem 0.5rem;
    border: 1px solid var(--line);
    border-radius: 4px;
    background: none;
    color: var(--muted);
    cursor: pointer;
  }

  .add {
    align-self: start;
  }

  .body {
    display: grid;
    grid-template-columns: minmax(13rem, 18rem) minmax(0, 1fr);
    gap: 1rem;
    flex: 1;
    min-height: 0;
  }

  .list {
    list-style: none;
    margin: 0;
    padding: 0;
    overflow-y: auto;
    border: 1px solid var(--line);
    border-radius: 5px;
  }

  .list li + li {
    border-top: 1px solid var(--line);
  }

  .list button {
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

  .list button:hover,
  .list button.on {
    background: var(--hover);
  }

  .who {
    font-size: 0.82rem;
    font-weight: 600;
  }

  .what,
  .when {
    font-size: 0.7rem;
    color: var(--muted);
  }

  .how {
    font-size: 0.8rem;
    text-align: right;
    font-variant-numeric: tabular-nums;
    grid-row: span 2;
    align-self: center;
  }

  .how.live {
    color: var(--accent);
  }

  .detail {
    overflow-y: auto;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 0.7rem;
  }

  .progress {
    display: flex;
    align-items: baseline;
    gap: 0.6rem;
    font-size: 0.78rem;
    color: var(--muted);
  }

  .progress strong {
    color: var(--fg);
    font-size: 0.85rem;
  }

  .progress .plain {
    margin-left: auto;
  }

  .lede,
  .axis-note,
  .footnote {
    margin: 0;
    font-size: 0.76rem;
    color: var(--muted);
    line-height: 1.5;
  }

  .footnote {
    margin-top: auto;
    padding-top: 0.4rem;
    border-top: 1px solid var(--line);
  }

  .map {
    display: grid;
    grid-template-columns: auto repeat(var(--cols), minmax(3.2rem, 1fr));
    gap: 2px;
    overflow-x: auto;
  }

  .corner,
  .head,
  .side {
    font-size: 0.68rem;
    color: var(--muted);
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 0.2rem 0.35rem;
    font-variant-numeric: tabular-nums;
  }

  .corner {
    justify-content: flex-end;
    text-transform: none;
  }

  .side {
    justify-content: flex-end;
  }

  .cell {
    font: inherit;
    font-size: 0.75rem;
    font-variant-numeric: tabular-nums;
    padding: 0.4rem 0.3rem;
    border: 1px solid transparent;
    border-radius: 3px;
    color: var(--fg);
    cursor: pointer;
  }

  .cell:disabled {
    color: var(--muted);
    cursor: default;
  }

  .cell.on {
    border-color: var(--accent);
  }

  .picked {
    display: grid;
    grid-template-columns: auto 1fr;
    gap: 0.2rem 0.8rem;
    margin: 0;
    font-size: 0.78rem;
  }

  .picked dt {
    color: var(--muted);
  }

  .picked dd {
    margin: 0;
    font-variant-numeric: tabular-nums;
  }

  table {
    width: 100%;
    border-collapse: collapse;
    font-size: 0.76rem;
  }

  th,
  td {
    text-align: left;
    padding: 0.3rem 0.45rem;
    border-bottom: 1px solid var(--line);
  }

  th {
    color: var(--muted);
    font-weight: 500;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    font-size: 0.66rem;
  }

  .num {
    text-align: right;
    font-variant-numeric: tabular-nums;
  }

  .dates,
  .params {
    color: var(--muted);
    white-space: nowrap;
  }

  td.good {
    color: var(--good);
  }

  td.bad {
    color: var(--bad);
  }

  .hint {
    margin: 0;
    padding: 0.6rem;
    font-size: 0.8rem;
    color: var(--muted);
  }

  .hint.inline {
    padding: 0.25rem 0;
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
