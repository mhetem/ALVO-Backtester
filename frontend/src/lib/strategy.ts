import { HttpError, errorMessage } from './api';
import { authorized, decode } from './session';
import type { CatalogEntry } from './catalog';

export const SPEC_VERSION = 1;

export const PRICE_FIELDS = [
  'close',
  'open',
  'high',
  'low',
  'hl2',
  'hlc3',
  'ohlc4',
  'volume',
] as const;

export const COMPARATORS = [
  'gt',
  'lt',
  'gte',
  'lte',
  'eq',
  'crosses_above',
  'crosses_below',
  'rising',
  'falling',
  'between',
] as const;

export const SIZING_TYPES = ['fixed_qty', 'pct_equity', 'fixed_cash', 'risk_pct'] as const;

export const LEVEL_TYPES = ['pct', 'atr'] as const;

export const BRACKETS = ['stop_loss', 'take_profit'] as const;

export type Comparator = (typeof COMPARATORS)[number];
export type SizingType = (typeof SIZING_TYPES)[number];
export type LevelType = (typeof LEVEL_TYPES)[number];
export type BracketKind = (typeof BRACKETS)[number];

export const COMPARATOR_LABELS: Record<Comparator, string> = {
  gt: 'is above',
  lt: 'is below',
  gte: 'is at or above',
  lte: 'is at or below',
  eq: 'equals',
  crosses_above: 'crosses above',
  crosses_below: 'crosses below',
  rising: 'has risen for',
  falling: 'has fallen for',
  between: 'is between',
};

export const SIZING_LABELS: Record<SizingType, string> = {
  fixed_qty: 'a fixed number of shares',
  pct_equity: 'a fraction of equity',
  fixed_cash: 'a fixed amount, in cents',
  risk_pct: 'a fraction of equity risked to the stop',
};

export function operandCount(op: Comparator): number {
  return op === 'between' ? 3 : 2;
}

export function countsBars(op: Comparator): boolean {
  return op === 'rising' || op === 'falling';
}

export type OperandDraft =
  | { kind: 'input'; name: string; back: number }
  | { kind: 'field'; field: string; back: number }
  | { kind: 'number'; value: number };

export type LevelDraft = { type: LevelType; value: number; period: number; mult: number };

export type GroupDraft = { kind: 'group'; op: 'all' | 'any'; children: RuleDraft[] };
export type NotDraft = { kind: 'not'; child: RuleDraft };
export type CompareDraft = { kind: 'compare'; op: Comparator; operands: OperandDraft[] };
export type BracketDraft = { kind: 'bracket'; op: BracketKind; level: LevelDraft };

export type RuleDraft = GroupDraft | NotDraft | CompareDraft | BracketDraft;

export type InputDraft = {
  name: string;
  indicator: string;
  params: Record<string, number>;
  source: string;
  output: string;
};

export type SpecDraft = {
  inputs: InputDraft[];
  entry: RuleDraft;
  exit: RuleDraft | null;
  sizing: { type: SizingType; value: number };
  costs: { brokerage_cents: number; fee_bps: number; slippage_bps: number };
};

export type PlanSummary = {
  inputs: number;
  indicators: string[];
  slots: number;
  warmup: number;
  prime_bars: number;
  depth: number;
  rule_exit: boolean;
  stop_loss: boolean;
  take_profit: boolean;
};

export type SavedStrategy = {
  id: string;
  name: string;
  description: string;
  version: number;
  spec: unknown;
  plan?: PlanSummary;
  created_at: string;
  updated_at: string;
};

export class SpecError extends Error {
  pointer: string;

  constructor(message: string, pointer: string) {
    super(message);
    this.name = 'SpecError';
    this.pointer = pointer;
  }
}

export function namedOperand(name: string, back = 0): OperandDraft {
  return (PRICE_FIELDS as readonly string[]).includes(name)
    ? { kind: 'field', field: name, back }
    : { kind: 'input', name, back };
}

export function operandName(operand: OperandDraft): string {
  if (operand.kind === 'input') {
    return operand.name;
  }
  return operand.kind === 'field' ? operand.field : String(operand.value);
}

export function blankLevel(): LevelDraft {
  return { type: 'pct', value: 0.05, period: 14, mult: 2 };
}

export function blankSpec(): SpecDraft {
  return {
    inputs: [
      { name: 'fast', indicator: 'ema', params: { period: 9 }, source: 'close', output: '' },
      { name: 'slow', indicator: 'ema', params: { period: 21 }, source: 'close', output: '' },
    ],
    entry: {
      kind: 'compare',
      op: 'crosses_above',
      operands: [namedOperand('fast'), namedOperand('slow')],
    },
    exit: {
      kind: 'compare',
      op: 'crosses_below',
      operands: [namedOperand('fast'), namedOperand('slow')],
    },
    sizing: { type: 'pct_equity', value: 0.95 },
    costs: { brokerage_cents: 0, fee_bps: 3.25, slippage_bps: 5 },
  };
}

export function blankRule(names: string[]): CompareDraft {
  const first = names[0] ?? 'close';
  return { kind: 'compare', op: 'gt', operands: [namedOperand(first), { kind: 'number', value: 0 }] };
}

export function inputNames(spec: SpecDraft): string[] {
  return spec.inputs.map((input) => input.name).filter((name) => name !== '');
}

export function nextInputName(spec: SpecDraft): string {
  const taken = new Set(inputNames(spec));
  for (let i = 1; i <= 99; i++) {
    const name = `input_${i}`;
    if (!taken.has(name)) {
      return name;
    }
  }
  return 'input';
}

export function draftInput(name: string, entry: CatalogEntry): InputDraft {
  const params: Record<string, number> = {};
  for (const param of entry.params) {
    params[param.name] = param.default;
  }

  return {
    name,
    indicator: entry.name,
    params,
    source: entry.sourced ? 'close' : '',
    output: entry.outputs.length > 1 ? entry.outputs[0] : '',
  };
}

function levelToJSON(level: LevelDraft): unknown {
  return level.type === 'atr'
    ? { type: 'atr', period: level.period, mult: level.mult }
    : { type: 'pct', value: level.value };
}

function operandToJSON(operand: OperandDraft): unknown {
  if (operand.kind === 'number') {
    return operand.value;
  }

  const name = operand.kind === 'input' ? operand.name : operand.field;
  return operand.back > 0 ? { ref: [name, operand.back] } : name;
}

export function ruleToJSON(rule: RuleDraft): unknown {
  switch (rule.kind) {
    case 'group':
      return { [rule.op]: rule.children.map(ruleToJSON) };
    case 'not':
      return { not: ruleToJSON(rule.child) };
    case 'bracket':
      return { [rule.op]: levelToJSON(rule.level) };
    default:
      return { [rule.op]: rule.operands.map(operandToJSON) };
  }
}

export function specToJSON(spec: SpecDraft): Record<string, unknown> {
  const inputs: Record<string, unknown> = {};

  for (const input of spec.inputs) {
    const body: Record<string, unknown> = {
      indicator: input.indicator,
      params: { ...input.params },
    };
    if (input.source) {
      body.source = input.source;
    }
    if (input.output) {
      body.output = input.output;
    }
    inputs[input.name] = body;
  }

  const body: Record<string, unknown> = {
    version: SPEC_VERSION,
    inputs,
    entry: { long: ruleToJSON(spec.entry) },
    sizing: { ...spec.sizing },
    costs: { ...spec.costs },
  };

  if (spec.exit) {
    body.exit = { long: ruleToJSON(spec.exit) };
  }

  return body;
}

function bare(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    throw new SpecError('this part of the spec is not a JSON object', '');
  }
  return value as Record<string, unknown>;
}

function levelFromJSON(value: unknown): LevelDraft {
  const body = bare(value);
  const level = blankLevel();

  level.type = body.type === 'atr' ? 'atr' : 'pct';
  if (typeof body.value === 'number') {
    level.value = body.value;
  }
  if (typeof body.period === 'number') {
    level.period = body.period;
  }
  if (typeof body.mult === 'number') {
    level.mult = body.mult;
  }

  return level;
}

function operandFromJSON(value: unknown): OperandDraft {
  if (typeof value === 'number') {
    return { kind: 'number', value };
  }
  if (typeof value === 'string') {
    return namedOperand(value);
  }

  const body = bare(value);
  const ref = body.ref;
  if (Array.isArray(ref) && ref.length === 2 && typeof ref[0] === 'string' && typeof ref[1] === 'number') {
    return namedOperand(ref[0], ref[1]);
  }

  throw new SpecError('an operand is a name, a number, or a ref', '');
}

export function ruleFromJSON(value: unknown): RuleDraft {
  const body = bare(value);
  const keys = Object.keys(body);
  if (keys.length !== 1) {
    throw new SpecError('a rule is an object carrying exactly one operator', '');
  }

  const op = keys[0];
  const inner = body[op];

  if (op === 'all' || op === 'any') {
    if (!Array.isArray(inner)) {
      throw new SpecError(`${op} takes a JSON array`, '');
    }
    return { kind: 'group', op, children: inner.map(ruleFromJSON) };
  }

  if (op === 'not') {
    return { kind: 'not', child: ruleFromJSON(inner) };
  }

  if ((BRACKETS as readonly string[]).includes(op)) {
    return { kind: 'bracket', op: op as BracketKind, level: levelFromJSON(inner) };
  }

  if (!(COMPARATORS as readonly string[]).includes(op)) {
    throw new SpecError(`no such rule "${op}"`, '');
  }
  if (!Array.isArray(inner)) {
    throw new SpecError(`${op} takes a JSON array`, '');
  }

  const operands = inner.map(operandFromJSON);
  if (countsBars(op as Comparator) && operands.length === 1) {
    operands.push({ kind: 'number', value: 1 });
  }

  return { kind: 'compare', op: op as Comparator, operands };
}

function sideFromJSON(value: unknown): RuleDraft {
  const body = bare(value);
  if (body.long === undefined) {
    throw new SpecError('a side needs a long rule', '');
  }
  return ruleFromJSON(body.long);
}

export function specFromJSON(value: unknown): SpecDraft {
  const body = bare(value);
  const spec = blankSpec();

  spec.inputs = [];
  for (const [name, raw] of Object.entries(bare(body.inputs))) {
    const input = bare(raw);
    const params: Record<string, number> = {};

    for (const [key, param] of Object.entries(bare(input.params ?? {}))) {
      if (typeof param === 'number') {
        params[key] = param;
      }
    }

    spec.inputs.push({
      name,
      indicator: typeof input.indicator === 'string' ? input.indicator : '',
      params,
      source: typeof input.source === 'string' ? input.source : '',
      output: typeof input.output === 'string' ? input.output : '',
    });
  }

  spec.entry = sideFromJSON(body.entry);
  spec.exit = body.exit === undefined || body.exit === null ? null : sideFromJSON(body.exit);

  const sizing = bare(body.sizing);
  if ((SIZING_TYPES as readonly string[]).includes(String(sizing.type))) {
    spec.sizing.type = sizing.type as SizingType;
  }
  if (typeof sizing.value === 'number') {
    spec.sizing.value = sizing.value;
  }

  if (body.costs !== undefined && body.costs !== null) {
    const costs = bare(body.costs);
    for (const key of ['brokerage_cents', 'fee_bps', 'slippage_bps'] as const) {
      const value = costs[key];
      if (typeof value === 'number') {
        spec.costs[key] = value;
      }
    }
  }

  return spec;
}

type StrategiesResponse = {
  count: number;
  limit: number;
  strategies: SavedStrategy[];
};

export type Validation = {
  valid: boolean;
  spec: unknown;
  plan: PlanSummary | null;
};

async function send<T>(path: string, init: RequestInit): Promise<T> {
  const res = await authorized(path, init);

  if (res.status === 400) {
    const body = (await res.json().catch(() => ({}))) as { error?: string; pointer?: string };
    throw new SpecError(body.error ?? 'the spec was rejected', body.pointer ?? '');
  }

  return decode<T>(res);
}

function json(body: unknown): RequestInit {
  return {
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  };
}

export async function validateSpec(spec: unknown): Promise<Validation> {
  return send<Validation>('/api/v1/strategies/validate', {
    method: 'POST',
    ...json({ spec }),
  });
}

export async function fetchStrategies(): Promise<SavedStrategy[]> {
  const body = await decode<StrategiesResponse>(
    await authorized('/api/v1/strategies', { method: 'GET' }),
  );
  return body.strategies ?? [];
}

export async function createStrategy(
  name: string,
  description: string,
  spec: unknown,
): Promise<SavedStrategy> {
  return send<SavedStrategy>('/api/v1/strategies', {
    method: 'POST',
    ...json({ name, description, spec }),
  });
}

export async function updateStrategy(
  id: string,
  name: string,
  description: string,
  spec: unknown,
): Promise<SavedStrategy> {
  return send<SavedStrategy>(`/api/v1/strategies/${encodeURIComponent(id)}`, {
    method: 'PUT',
    ...json({ name, description, spec }),
  });
}

export async function deleteStrategy(id: string): Promise<void> {
  const res = await authorized(`/api/v1/strategies/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  });
  if (!res.ok && res.status !== 404) {
    throw new HttpError(res.status, await errorMessage(res));
  }
}

export function flagged(fault: string, at: string): boolean {
  return fault !== '' && (fault === at || fault.startsWith(`${at}/`));
}
