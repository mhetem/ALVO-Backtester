import type { Timeframe } from './api';
import { authorized, decode } from './session';

export const RUN_STATUSES = ['queued', 'running', 'done', 'error'] as const;

export type RunStatus = (typeof RUN_STATUSES)[number];

export type Drawdown = {
  pct: number;
  cents: number;
  peak_ts: string;
  trough_ts: string;
  bars: number;
  recovered: boolean;
};

export type Benchmark = {
  kind: 'buy_and_hold' | 'index';
  symbol: string;
  basis: string;
  return_pct: number;
  cagr_pct: number;
  volatility_pct: number;
  max_drawdown_pct: number;
  sharpe: number;
  excess_pct: number;
  correlation: number;
  beta: number;
  dividends_cents?: number;
  fees_cents?: number;
  unavailable?: string;
};

// One stock's own run inside a basket: it gets a slice of the capital and nothing else is
// shared, so it carries the same risk numbers the aggregate does.
export type SymbolStats = {
  symbol: string;
  basis: string;
  capital_cents: number;
  final_equity_cents: number;
  trades: number;
  wins: number;
  losses: number;
  win_rate_pct: number;
  pnl_cents: number;
  fees_cents: number;
  dividends_cents: number;
  borrow_cents: number;
  borrow_annual_pct: number;
  bars_in_market: number;
  time_in_market_pct: number;
  contribution_pct: number;
  return_pct: number;
  cagr_pct: number;
  volatility_pct: number;
  max_drawdown: Drawdown;
  sharpe: number;
  sortino: number;
  calmar: number;
  profit_factor: number | null;
  expectancy_cents: number;
  benchmarks?: Benchmark[];
};

export type Metrics = {
  basis: string;
  bars: number;
  bars_in_market: number;
  time_in_market_pct: number;
  bars_per_year: number;

  capital_cents: number;
  final_equity_cents: number;
  pnl_cents: number;
  fees_cents: number;
  dividends_cents: number;
  dividend_events: number;
  borrow_cents: number;
  borrow_stale: boolean;
  split_cash_cents: number;
  return_pct: number;
  cagr_pct: number;
  volatility_pct: number;

  max_drawdown: Drawdown;
  longest_drawdown_bars: number;
  sharpe: number;
  sortino: number;
  calmar: number;

  risk_free_pct: number;
  risk_free_stale: boolean;

  trades: number;
  wins: number;
  losses: number;
  scratches: number;
  win_rate_pct: number;
  profit_factor: number | null;
  expectancy_cents: number;
  avg_win_cents: number;
  avg_loss_cents: number;
  largest_win_cents: number;
  largest_loss_cents: number;
  max_consecutive_losses: number;
  avg_holding_bars: number;

  exits_by_signal: number;
  exits_by_stop: number;
  exits_by_target: number;
  exits_by_split: number;
  exits_at_end: number;
  ambiguous_bars: number;
  skipped_entries: number;
  shorts_unavailable: number;
  unadjusted_bars: number;
  unpriced_actions: number;
  split_events: number;
  splits_applied: number;

  symbols: SymbolStats[];
  benchmarks: Benchmark[];
};

export type Run = {
  id: string;
  strategy_id: string;
  symbol: string;
  symbols: string[];
  timeframe: Timeframe;
  start: string;
  end: string;
  capital_cents: number;
  status: RunStatus;
  spec?: unknown;
  metrics?: Metrics;
  error?: string;
  created_at: string;
  started_at?: string;
  finished_at?: string;
};

export type Trade = {
  seq: number;
  symbol: string;
  side: string;
  qty: number;
  entry_ts: string;
  entry_price: number;
  exit_ts?: string;
  exit_price?: number;
  pnl_cents: number;
  fees_cents: number;
  dividends_cents: number;
  borrow_cents: number;
  split_cash_cents: number;
  exit_reason?: string;
};

export type Curve = {
  run_id: string;
  symbol: string;
  count: number;
  total: number;
  sampled: boolean;
  ts: number[];
  equity: number[];
  hold?: number[];
  index?: number[];
  drawdown: number[];
  symbols?: SleeveCurve[];
};

// Stored on the run's timeline and sampled the same way, so a sleeve lines up with the
// curve's `ts` point for point and needs no stamps of its own.
export type SleeveCurve = {
  symbol: string;
  equity: number[];
};

export type LaunchRequest = {
  strategy_id: string;
  symbols: string[];
  timeframe: Timeframe;
  start: string;
  end: string;
  capital_cents: number;
};

type RunsResponse = {
  count: number;
  limit: number;
  offset: number;
  runs: Run[];
};

type TradesResponse = {
  run_id: string;
  symbol: string;
  symbols: string[];
  count: number;
  trades: Trade[];
};

export type TradeMark = {
  seq: number;
  symbol: string;
  entry: number;
  exit: number | null;
  won: boolean;
  reason: string;
};

// The chart speaks in epoch seconds; trades come back as RFC 3339 strings. A portfolio run
// fills several symbols, and only the one on screen has candles to hang a marker on.
export function marksOf(trades: Trade[], symbol?: string): TradeMark[] {
  return trades
    .filter((trade) => symbol === undefined || trade.symbol === symbol)
    .map((trade) => ({
      seq: trade.seq,
      symbol: trade.symbol,
      entry: Math.floor(new Date(trade.entry_ts).getTime() / 1000),
      exit: trade.exit_ts ? Math.floor(new Date(trade.exit_ts).getTime() / 1000) : null,
      won: trade.pnl_cents >= 0,
      reason: trade.exit_reason ?? 'exit',
    }));
}

export function settled(run: Run | null): boolean {
  return run !== null && (run.status === 'done' || run.status === 'error');
}

export async function launchBacktest(body: LaunchRequest): Promise<Run> {
  return decode<Run>(
    await authorized('/api/v1/backtests', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    }),
  );
}

export async function fetchRuns(limit = 20): Promise<Run[]> {
  const body = await decode<RunsResponse>(
    await authorized(`/api/v1/backtests?limit=${limit}`, { method: 'GET' }),
  );
  return body.runs ?? [];
}

export async function fetchRun(id: string): Promise<Run> {
  return decode<Run>(await authorized(`/api/v1/backtests/${encodeURIComponent(id)}`, { method: 'GET' }));
}

export async function fetchTrades(id: string): Promise<Trade[]> {
  const body = await decode<TradesResponse>(
    await authorized(`/api/v1/backtests/${encodeURIComponent(id)}/trades`, { method: 'GET' }),
  );
  return body.trades ?? [];
}

export async function fetchCurve(id: string, points = 2000): Promise<Curve> {
  return decode<Curve>(
    await authorized(`/api/v1/backtests/${encodeURIComponent(id)}/equity?points=${points}`, {
      method: 'GET',
    }),
  );
}
