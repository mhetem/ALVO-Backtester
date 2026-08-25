import { HttpError, errorMessage } from './api';
import { authorize, resume } from './session';
import type { StoredEntry } from './indicators';

const workingKey = 'alvo.indicators';
const localKey = 'alvo.layouts';
const layoutVersion = 1;

export type SavedLayout = {
  id: string;
  name: string;
  version: number;
  indicators: StoredEntry[];
  created_at: string;
  updated_at: string;
};

type LayoutsResponse = {
  count: number;
  limit: number;
  layouts: SavedLayout[];
};

type Workspace = {
  version: number;
  activeId: string | null;
  indicators: StoredEntry[];
};

function read<T>(key: string): T | null {
  try {
    const raw = window.localStorage.getItem(key);
    return raw ? (JSON.parse(raw) as T) : null;
  } catch {
    return null;
  }
}

function write(key: string, value: unknown): void {
  try {
    window.localStorage.setItem(key, JSON.stringify(value));
  } catch {
    void 0;
  }
}

export function readWorking(): Workspace {
  const stored = read<Workspace>(workingKey);
  if (!stored || stored.version !== layoutVersion || !Array.isArray(stored.indicators)) {
    return { version: layoutVersion, activeId: null, indicators: [] };
  }
  return { ...stored, activeId: stored.activeId ?? null };
}

export function writeWorking(indicators: StoredEntry[], activeId: string | null): void {
  write(workingKey, { version: layoutVersion, activeId, indicators });
}

export function readLocalLayouts(): SavedLayout[] {
  const stored = read<SavedLayout[]>(localKey);
  return Array.isArray(stored) ? stored : [];
}

export function writeLocalLayouts(layouts: SavedLayout[]): void {
  write(localKey, layouts);
}

function stamp(): string {
  return new Date().toISOString();
}

export function saveLocalLayout(
  layouts: SavedLayout[],
  id: string | null,
  name: string,
  indicators: StoredEntry[],
): SavedLayout[] {
  const clash = layouts.find((layout) => layout.name === name && layout.id !== id);
  if (clash) {
    throw new HttpError(409, `a layout named ${name} already exists`);
  }

  const existing = id === null ? undefined : layouts.find((layout) => layout.id === id);
  if (existing) {
    return layouts.map((layout) =>
      layout.id === id ? { ...layout, name, indicators, updated_at: stamp() } : layout,
    );
  }

  const now = stamp();
  return [
    ...layouts,
    {
      id: crypto.randomUUID(),
      name,
      version: layoutVersion,
      indicators,
      created_at: now,
      updated_at: now,
    },
  ];
}

async function authorized(path: string, init: RequestInit, retry = true): Promise<Response> {
  const headers = authorize(new Headers(init.headers));
  const res = await fetch(path, { ...init, headers, credentials: 'same-origin' });

  if (res.status === 401 && retry && (await resume())) {
    return authorized(path, init, false);
  }

  return res;
}

async function decode<T>(res: Response): Promise<T> {
  if (!res.ok) {
    throw new HttpError(res.status, await errorMessage(res));
  }
  return (await res.json()) as T;
}

export async function fetchLayouts(): Promise<SavedLayout[]> {
  const body = await decode<LayoutsResponse>(
    await authorized('/api/v1/chart-layouts', { method: 'GET' }),
  );
  return body.layouts ?? [];
}

export async function createLayout(
  name: string,
  indicators: StoredEntry[],
): Promise<SavedLayout> {
  return decode<SavedLayout>(
    await authorized('/api/v1/chart-layouts', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name, version: layoutVersion, indicators }),
    }),
  );
}

export async function updateLayout(
  id: string,
  name: string,
  indicators: StoredEntry[],
): Promise<SavedLayout> {
  return decode<SavedLayout>(
    await authorized(`/api/v1/chart-layouts/${encodeURIComponent(id)}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ name, version: layoutVersion, indicators }),
    }),
  );
}

export async function deleteLayout(id: string): Promise<void> {
  const res = await authorized(`/api/v1/chart-layouts/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  });
  if (!res.ok && res.status !== 404) {
    throw new HttpError(res.status, await errorMessage(res));
  }
}

export function sameSet(a: StoredEntry[], b: StoredEntry[]): boolean {
  return JSON.stringify(a) === JSON.stringify(b);
}
