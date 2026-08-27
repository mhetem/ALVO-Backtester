import { HttpError, errorMessage, type SymbolRow } from './api';
import { authorized, decode } from './session';

export const MAX_BASKET_SYMBOLS = 20;
export const MAX_BASKET_NAME = 60;

export type SavedBasket = {
  id: string;
  name: string;
  count: number;
  symbols: SymbolRow[];
  created_at: string;
  updated_at: string;
};

type BasketsResponse = {
  count: number;
  limit: number;
  baskets: SavedBasket[];
};

export function tickersOf(basket: SavedBasket): string[] {
  return basket.symbols.map((row) => row.ticker);
}

// The backtest and sweep forms both carry a basket as one comma separated field, which is
// the same list the API takes as `symbols`.
export function toField(basket: SavedBasket): string {
  return tickersOf(basket).join(', ');
}

export function parseField(text: string): string[] {
  const seen = new Set<string>();
  const tickers: string[] = [];

  for (const raw of text.split(',')) {
    const ticker = raw.trim().toUpperCase();
    if (ticker !== '' && !seen.has(ticker)) {
      seen.add(ticker);
      tickers.push(ticker);
    }
  }

  return tickers;
}

// Order is part of a basket — it is the order the API stores and reads back — so a
// reordered list is a changed list.
export function sameBasket(a: string[], b: string[]): boolean {
  return a.length === b.length && a.every((ticker, i) => ticker === b[i]);
}

export function sortBaskets(baskets: SavedBasket[]): SavedBasket[] {
  return [...baskets].sort((a, b) => a.name.localeCompare(b.name));
}

export function replaceBasket(baskets: SavedBasket[], saved: SavedBasket): SavedBasket[] {
  return sortBaskets([...baskets.filter((basket) => basket.id !== saved.id), saved]);
}

export async function fetchBaskets(): Promise<SavedBasket[]> {
  const body = await decode<BasketsResponse>(
    await authorized('/api/v1/baskets', { method: 'GET' }),
  );
  return body.baskets ?? [];
}

export async function createBasket(name: string, symbols: string[]): Promise<SavedBasket> {
  return decode<SavedBasket>(
    await authorized('/api/v1/baskets', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name, symbols }),
    }),
  );
}

export async function updateBasket(
  id: string,
  name: string,
  symbols: string[],
): Promise<SavedBasket> {
  return decode<SavedBasket>(
    await authorized(`/api/v1/baskets/${encodeURIComponent(id)}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name, symbols }),
    }),
  );
}

export async function deleteBasket(id: string): Promise<void> {
  const res = await authorized(`/api/v1/baskets/${encodeURIComponent(id)}`, { method: 'DELETE' });
  if (!res.ok && res.status !== 404) {
    throw new HttpError(res.status, await errorMessage(res));
  }
}
