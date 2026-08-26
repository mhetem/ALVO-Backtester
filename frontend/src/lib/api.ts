export const TIMEFRAMES = ['5m', '15m', '30m', '1h', '1d'] as const;

export type Timeframe = (typeof TIMEFRAMES)[number];

export type ChartMode = 'candles' | 'bars' | 'line';

export type SymbolRow = {
  ticker: string;
  name: string;
  kind: string;
  currency: string;
  lot_size: number;
  tick_size: number;
  active: boolean;
  tracked: boolean;
};

export type IndicatorSeries = {
  name: string;
  start: number;
  values: number[];
};

export type IndicatorResult = {
  key: string;
  name: string;
  title: string;
  params: Record<string, number>;
  source?: string;
  overlay: boolean;
  warmup: number;
  offsets: number[];
  series: IndicatorSeries[];
};

export type CandlesResponse = {
  symbol: string;
  timeframe: Timeframe;
  base: string;
  count: number;
  ts: number[];
  o: number[];
  h: number[];
  l: number[];
  c: number[];
  v: number[];
  next_cursor?: string;
  future?: number[];
  indicators?: IndicatorResult[];
};

export type Bar = {
  time: number;
  open: number;
  high: number;
  low: number;
  close: number;
  volume: number;
};

export type Page = {
  bars: Bar[];
  cursor: string | null;
  base: string;
  future: number[];
  indicators: IndicatorResult[];
};

export const MAX_PAGE_LIMIT = 5000;

export class HttpError extends Error {
  status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = 'HttpError';
    this.status = status;
  }
}

type SymbolsResponse = {
  query: string;
  kind?: string;
  count: number;
  symbols: SymbolRow[];
};

type Params = Record<string, string | number | undefined>;

async function getJSON<T>(path: string, params: Params, signal?: AbortSignal): Promise<T> {
  const url = new URL(path, window.location.origin);
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== '') {
      url.searchParams.set(key, String(value));
    }
  }

  const res = await fetch(url, { signal });
  if (!res.ok) {
    throw new HttpError(res.status, await errorMessage(res));
  }

  return (await res.json()) as T;
}

export async function errorMessage(res: Response): Promise<string> {
  try {
    const body = (await res.json()) as { error?: string };
    if (body.error) {
      return body.error;
    }
  } catch {
    void 0;
  }
  return `request failed with ${res.status}`;
}

export function describe(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause);
}

export async function searchSymbols(q: string, signal?: AbortSignal): Promise<SymbolRow[]> {
  const body = await getJSON<SymbolsResponse>('/api/v1/symbols', { q, limit: 12 }, signal);
  return body.symbols;
}

export async function fetchCandles(
  symbol: string,
  timeframe: Timeframe,
  cursor: string | null,
  limit: number,
  indicators: string,
  signal?: AbortSignal,
): Promise<Page> {
  const body = await getJSON<CandlesResponse>(
    '/api/v1/candles',
    { symbol, timeframe, limit, cursor: cursor ?? undefined, indicators: indicators || undefined },
    signal,
  );

  return {
    bars: toBars(body),
    cursor: body.next_cursor ?? null,
    base: body.base,
    future: body.future ?? [],
    indicators: body.indicators ?? [],
  };
}

export function toBars(res: CandlesResponse): Bar[] {
  const bars: Bar[] = [];
  for (let i = 0; i < res.ts.length; i++) {
    bars.push({
      time: res.ts[i],
      open: res.o[i],
      high: res.h[i],
      low: res.l[i],
      close: res.c[i],
      volume: res.v[i],
    });
  }
  return bars;
}
