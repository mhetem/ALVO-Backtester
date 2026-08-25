import { HttpError, errorMessage } from './api';

export const DEFAULT_SOURCE = 'close';

export type ParamKind = 'int' | 'float';

export type CatalogParam = {
  name: string;
  kind: ParamKind;
  default: number;
  min: number;
  max: number;
};

export type CatalogEntry = {
  name: string;
  title: string;
  group: string;
  overlay: boolean;
  sourced: boolean;
  key: string;
  warmup: number;
  params: CatalogParam[];
  outputs: string[];
  offsets: number[];
};

export type Catalog = {
  count: number;
  max_per_request: number;
  groups: string[];
  sources: string[];
  indicators: CatalogEntry[];
};

export async function fetchCatalog(signal?: AbortSignal): Promise<Catalog> {
  const res = await fetch('/api/v1/indicators', { signal });
  if (!res.ok) {
    throw new HttpError(res.status, await errorMessage(res));
  }
  return (await res.json()) as Catalog;
}

export function findEntry(catalog: Catalog | null, name: string): CatalogEntry | null {
  return catalog?.indicators.find((entry) => entry.name === name) ?? null;
}

export function formatParam(param: CatalogParam, value: number): string {
  return param.kind === 'int' ? String(Math.round(value)) : String(value);
}

export function indicatorKey(
  entry: CatalogEntry,
  params: Record<string, number>,
  source: string,
): string {
  const parts = [entry.name];
  for (const param of entry.params) {
    parts.push(formatParam(param, params[param.name] ?? param.default));
  }
  if (entry.sourced && source && source !== DEFAULT_SOURCE) {
    parts.push(`source=${source}`);
  }
  return parts.join(':');
}

export type ParsedKey = {
  entry: CatalogEntry;
  params: Record<string, number>;
  source: string;
};

export function parseKey(key: string, catalog: Catalog): ParsedKey | null {
  const parts = key.trim().split(':');
  const entry = findEntry(catalog, parts[0].trim().toLowerCase());
  if (!entry) {
    return null;
  }

  const params: Record<string, number> = {};
  let source = DEFAULT_SOURCE;
  let positional = 0;

  for (const raw of parts.slice(1)) {
    const part = raw.trim();
    if (part === '') {
      continue;
    }

    const cut = part.indexOf('=');
    let name: string;
    let text: string;

    if (cut >= 0) {
      name = part.slice(0, cut).trim().toLowerCase();
      text = part.slice(cut + 1).trim();
    } else {
      if (positional >= entry.params.length) {
        return null;
      }
      name = entry.params[positional].name;
      text = part;
      positional += 1;
    }

    if (name === 'source') {
      source = text;
      continue;
    }

    const value = Number(text);
    if (!Number.isFinite(value)) {
      return null;
    }
    params[name] = value;
  }

  for (const param of entry.params) {
    if (params[param.name] === undefined) {
      params[param.name] = param.default;
    }
  }

  return { entry, params, source };
}

export function clampParam(param: CatalogParam, value: number): number {
  if (!Number.isFinite(value)) {
    return param.default;
  }
  const bounded = Math.min(Math.max(value, param.min), param.max);
  return param.kind === 'int' ? Math.round(bounded) : bounded;
}

export function summarize(entry: CatalogEntry): string {
  if (entry.params.length === 0) {
    return 'no parameters';
  }
  return entry.params.map((param) => `${param.name} ${formatParam(param, param.default)}`).join(' · ');
}
