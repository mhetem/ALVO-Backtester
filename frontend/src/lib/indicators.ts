import { AUTO_SLOTS, SERIES_SLOTS } from './theme';
import {
  clampParam,
  indicatorKey,
  parseKey,
  DEFAULT_SOURCE,
  type Catalog,
  type CatalogEntry,
} from './catalog';

export const SIGNAL_OUTPUT = 'direction';
export const HISTOGRAM_OUTPUT = 'histogram';

export const LINE_STYLES = ['solid', 'dotted'] as const;

export type LineStyleName = (typeof LINE_STYLES)[number];

export type ActiveIndicator = {
  key: string;
  name: string;
  title: string;
  group: string;
  overlay: boolean;
  sourced: boolean;
  outputs: string[];
  params: Record<string, number>;
  source: string;
  visible: boolean;
  style: LineStyleName;
  colors: number[];
};

export type StoredEntry = {
  key: string;
  visible: boolean;
  style: LineStyleName;
  colors: number[];
};

export function drawnOutputs(active: ActiveIndicator): string[] {
  return active.outputs.filter((output) => output !== SIGNAL_OUTPUT);
}

export function slotOf(active: ActiveIndicator, output: string): number {
  const at = active.outputs.indexOf(output);
  return (active.colors[at] ?? 0) % SERIES_SLOTS;
}

function takenSlots(list: ActiveIndicator[]): Set<number> {
  const taken = new Set<number>();
  for (const active of list) {
    for (const slot of active.colors) {
      taken.add(slot % SERIES_SLOTS);
    }
  }
  return taken;
}

function assignColors(outputs: string[], list: ActiveIndicator[]): number[] {
  const taken = takenSlots(list);
  const colors: number[] = [];

  for (let i = 0; i < outputs.length; i++) {
    let slot = 0;
    while (slot < AUTO_SLOTS && taken.has(slot)) {
      slot += 1;
    }
    if (slot === AUTO_SLOTS) {
      slot = (colors.length + taken.size) % AUTO_SLOTS;
    }
    taken.add(slot);
    colors.push(slot);
  }

  return colors;
}

function build(
  entry: CatalogEntry,
  params: Record<string, number>,
  source: string,
  colors: number[],
  visible: boolean,
  style: LineStyleName,
): ActiveIndicator {
  return {
    key: indicatorKey(entry, params, source),
    name: entry.name,
    title: entry.title,
    group: entry.group,
    overlay: entry.overlay,
    sourced: entry.sourced,
    outputs: entry.outputs,
    params,
    source,
    visible,
    style,
    colors,
  };
}

export function addIndicator(
  entry: CatalogEntry,
  list: ActiveIndicator[],
): ActiveIndicator | null {
  const taken = new Set(list.map((active) => active.key));
  const params: Record<string, number> = {};
  for (const param of entry.params) {
    params[param.name] = param.default;
  }

  const bumpable = entry.params.find((param) => param.kind === 'int');
  let key = indicatorKey(entry, params, DEFAULT_SOURCE);

  while (taken.has(key)) {
    if (!bumpable || params[bumpable.name] >= bumpable.max) {
      return null;
    }
    params[bumpable.name] = clampParam(bumpable, params[bumpable.name] + 1);
    key = indicatorKey(entry, params, DEFAULT_SOURCE);
  }

  return build(entry, params, DEFAULT_SOURCE, assignColors(entry.outputs, list), true, 'solid');
}

export function retune(
  active: ActiveIndicator,
  catalog: Catalog,
  params: Record<string, number>,
  source: string,
): ActiveIndicator {
  const entry = catalog.indicators.find((candidate) => candidate.name === active.name);
  if (!entry) {
    return active;
  }

  const bounded: Record<string, number> = {};
  for (const param of entry.params) {
    bounded[param.name] = clampParam(param, params[param.name] ?? param.default);
  }

  return {
    ...active,
    params: bounded,
    source: entry.sourced ? source : DEFAULT_SOURCE,
    key: indicatorKey(entry, bounded, entry.sourced ? source : DEFAULT_SOURCE),
  };
}

export function restyle(active: ActiveIndicator, style: LineStyleName): ActiveIndicator {
  return { ...active, style };
}

export function recolor(active: ActiveIndicator, output: string, slot: number): ActiveIndicator {
  const at = active.outputs.indexOf(output);
  if (at < 0) {
    return active;
  }

  const colors = active.outputs.map((_, i) => active.colors[i] ?? 0);
  colors[at] = slot;
  return { ...active, colors };
}

export function toStored(list: ActiveIndicator[]): StoredEntry[] {
  return list.map((active) => ({
    key: active.key,
    visible: active.visible,
    style: active.style,
    colors: active.outputs.map((_, i) => active.colors[i] ?? 0),
  }));
}

export function fromStored(entries: StoredEntry[], catalog: Catalog): ActiveIndicator[] {
  const list: ActiveIndicator[] = [];

  for (const entry of entries) {
    const parsed = parseKey(entry.key, catalog);
    if (!parsed) {
      continue;
    }

    const colors =
      entry.colors.length === parsed.entry.outputs.length
        ? entry.colors.map((slot) => ((slot % SERIES_SLOTS) + SERIES_SLOTS) % SERIES_SLOTS)
        : assignColors(parsed.entry.outputs, list);

    const style = LINE_STYLES.includes(entry.style) ? entry.style : 'solid';
    list.push(
      build(parsed.entry, parsed.params, parsed.source, colors, entry.visible !== false, style),
    );
  }

  return list;
}
