import type { Timeframe } from './api';
import { authorized, decode } from './session';
import type { SpecDraft } from './strategy';

export const SWEEP_KINDS = ['grid', 'walk_forward'] as const;

export const OBJECTIVES = [
  'sharpe',
  'sortino',
  'calmar',
  'return_pct',
  'cagr_pct',
  'profit_factor',
  'expectancy_cents',
] as const;

export type SweepKind = (typeof SWEEP_KINDS)[number];
export type Objective = (typeof OBJECTIVES)[number];

export const KIND_LABELS: Record<SweepKind, string> = {
  grid: 'Grid',
  walk_forward: 'Walk-forward',
};

export const OBJECTIVE_LABELS: Record<Objective, string> = {
  sharpe: 'Sharpe',
  sortino: 'Sortino',
  calmar: 'Calmar',
  return_pct: 'Total return',
  cagr_pct: 'CAGR',
  profit_factor: 'Profit factor',
  expectancy_cents: 'Expectancy',
};

export const MAX_AXES = 3;

export type Axis = {
  path: string;
  values: number[];
};

export type AxisDraft = {
  path: string;
  from: number;
  to: number;
  step: number;
};

export type Fold = {
  fold: number;
  in_start: string;
  in_end: string;
  out_start: string;
  out_end: string;
};

export type SweepProgress = {
  total: number;
  queued: number;
  running: number;
  done: number;
  failed: number;
};

export type SweepRun = {
  id: string;
  point: number;
  fold?: number;
  phase?: 'in_sample' | 'out_of_sample';
  status: 'queued' | 'running' | 'done' | 'error';
  params: Record<string, number>;
  score?: number;
  return_pct?: number;
  trades: number;
  start: string;
  end: string;
  error?: string;
};

export type Sweep = {
  id: string;
  strategy_id: string;
  kind: SweepKind;
  objective: Objective;
  symbol: string;
  symbols: string[];
  timeframe: Timeframe;
  start: string;
  end: string;
  capital_cents: number;
  max_positions: number;
  points: number;
  axes: Axis[];
  folds?: Fold[];
  progress: SweepProgress;
  runs?: SweepRun[];
  created_at: string;
};

export type SweepRequest = {
  strategy_id: string;
  kind: SweepKind;
  objective: Objective;
  symbols: string[];
  timeframe: Timeframe;
  start: string;
  end: string;
  capital_cents: number;
  max_positions: number;
  axes: AxisDraft[];
  in_sample_days?: number;
  out_of_sample_days?: number;
};

type SweepsResponse = {
  count: number;
  limit: number;
  offset: number;
  sweeps: Sweep[];
};

// What a sweep may vary, read off the strategy it is sweeping. Only numbers a spec actually
// tunes appear: an indicator's parameters, the position size, and the costs.
export function sweepablePaths(spec: SpecDraft): string[] {
  const paths: string[] = [];

  for (const input of spec.inputs) {
    if (input.name === '') {
      continue;
    }
    for (const param of Object.keys(input.params).sort()) {
      paths.push(`/inputs/${input.name}/params/${param}`);
    }
  }

  paths.push('/sizing/value');
  for (const cost of ['fee_bps', 'slippage_bps', 'brokerage_cents'] as const) {
    paths.push(`/costs/${cost}`);
  }

  return paths;
}

export function pathLabel(path: string): string {
  const parts = path.split('/').filter((part) => part !== '');
  if (parts.length === 4 && parts[0] === 'inputs') {
    return `${parts[1]}.${parts[3]}`;
  }
  return parts.join('.');
}

export function valueAt(spec: SpecDraft, path: string): number {
  const parts = path.split('/').filter((part) => part !== '');

  if (parts.length === 4 && parts[0] === 'inputs') {
    const input = spec.inputs.find((held) => held.name === parts[1]);
    return input?.params[parts[3]] ?? 0;
  }
  if (parts[0] === 'sizing') {
    return spec.sizing.value;
  }
  if (parts[0] === 'costs') {
    return spec.costs[parts[1] as keyof SpecDraft['costs']] ?? 0;
  }

  return 0;
}

// How many runs a draft would queue, so the form can say so before the server refuses it.
export function gridSize(axes: AxisDraft[]): number {
  return axes.reduce((total, axis) => total * axisSize(axis), 1);
}

export function axisSize(axis: AxisDraft): number {
  if (axis.step <= 0 || axis.to < axis.from) {
    return 0;
  }
  return Math.floor((axis.to - axis.from) / axis.step + 1e-9) + 1;
}

export function axisValues(axis: AxisDraft): number[] {
  const values: number[] = [];
  for (let i = 0; i < axisSize(axis); i++) {
    values.push(Math.round((axis.from + i * axis.step) * 1e6) / 1e6);
  }
  return values;
}

export function running(sweep: Sweep): boolean {
  return sweep.progress.queued > 0 || sweep.progress.running > 0;
}

export function finishedOf(sweep: Sweep): SweepRun[] {
  return (sweep.runs ?? []).filter((run) => run.status === 'done');
}

// The winning point of a fold's training window, chosen the way the worker chooses it, so
// the table and the queued out-of-sample run agree on who won.
export function winnerOf(runs: SweepRun[]): SweepRun | null {
  let best: SweepRun | null = null;

  for (const run of runs) {
    if (run.score === undefined) {
      continue;
    }
    if (best === null || run.score > (best.score ?? -Infinity)) {
      best = run;
    }
  }

  return best;
}

export async function launchSweep(body: SweepRequest): Promise<Sweep> {
  return decode<Sweep>(
    await authorized('/api/v1/sweeps', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    }),
  );
}

export async function fetchSweeps(limit = 20): Promise<Sweep[]> {
  const body = await decode<SweepsResponse>(
    await authorized(`/api/v1/sweeps?limit=${limit}`, { method: 'GET' }),
  );
  return body.sweeps ?? [];
}

export async function fetchSweep(id: string): Promise<Sweep> {
  return decode<Sweep>(await authorized(`/api/v1/sweeps/${encodeURIComponent(id)}`, { method: 'GET' }));
}

export async function deleteSweep(id: string): Promise<void> {
  const res = await authorized(`/api/v1/sweeps/${encodeURIComponent(id)}`, { method: 'DELETE' });
  if (!res.ok && res.status !== 404) {
    throw new Error(`deleting the sweep failed with ${res.status}`);
  }
}
