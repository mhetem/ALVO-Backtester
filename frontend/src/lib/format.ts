export const EXCHANGE_TZ = 'America/Sao_Paulo';

const months = [
  'Jan',
  'Feb',
  'Mar',
  'Apr',
  'May',
  'Jun',
  'Jul',
  'Aug',
  'Sep',
  'Oct',
  'Nov',
  'Dec',
];

const wallClock = new Intl.DateTimeFormat('en-CA', {
  timeZone: EXCHANGE_TZ,
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
  hourCycle: 'h23',
});

const price = new Intl.NumberFormat('pt-BR', {
  minimumFractionDigits: 2,
  maximumFractionDigits: 2,
});

const volume = new Intl.NumberFormat('pt-BR', {
  notation: 'compact',
  maximumFractionDigits: 1,
});

const large = new Intl.NumberFormat('pt-BR', {
  notation: 'compact',
  maximumFractionDigits: 2,
});

const fine = new Intl.NumberFormat('pt-BR', {
  minimumFractionDigits: 2,
  maximumFractionDigits: 4,
});

const offsets = new Map<number, number>();

function pad(value: number): string {
  return value < 10 ? `0${value}` : String(value);
}

function offsetSeconds(utcSeconds: number): number {
  const day = Math.floor(utcSeconds / 86400);
  const cached = offsets.get(day);
  if (cached !== undefined) {
    return cached;
  }

  const parts = wallClock.formatToParts(new Date(utcSeconds * 1000));
  const field = (type: Intl.DateTimeFormatPartTypes) =>
    Number(parts.find((part) => part.type === type)?.value ?? '0');

  const wall =
    Date.UTC(
      field('year'),
      field('month') - 1,
      field('day'),
      field('hour'),
      field('minute'),
      field('second'),
    ) / 1000;

  const offset = wall - utcSeconds;
  offsets.set(day, offset);
  return offset;
}

export function toChartTime(utcSeconds: number): number {
  return utcSeconds + offsetSeconds(utcSeconds);
}

export function formatStamp(utcSeconds: number, intraday: boolean): string {
  const at = new Date(toChartTime(utcSeconds) * 1000);
  const day = `${pad(at.getUTCDate())} ${months[at.getUTCMonth()]} ${at.getUTCFullYear()}`;
  return intraday ? `${day} ${pad(at.getUTCHours())}:${pad(at.getUTCMinutes())}` : day;
}

export function axisDay(chartSeconds: number): string {
  return String(new Date(chartSeconds * 1000).getUTCDate());
}

export function axisClock(chartSeconds: number): string {
  const at = new Date(chartSeconds * 1000);
  return `${pad(at.getUTCHours())}:${pad(at.getUTCMinutes())}`;
}

export function axisPeriod(chartSeconds: number, intraday: boolean): string {
  const at = new Date(chartSeconds * 1000);
  const label = `${months[at.getUTCMonth()]} ${at.getUTCFullYear()}`;
  return intraday ? `${pad(at.getUTCDate())} ${label}` : label;
}

export function axisPeriodKey(chartSeconds: number, intraday: boolean): string {
  const at = new Date(chartSeconds * 1000);
  const period = `${at.getUTCFullYear()}-${at.getUTCMonth()}`;
  return intraday ? `${period}-${at.getUTCDate()}` : period;
}

export function formatPrice(value: number): string {
  return price.format(value);
}

export function formatVolume(value: number): string {
  return volume.format(value);
}

export function formatValue(value: number): string {
  const size = Math.abs(value);
  if (size >= 1e6) {
    return large.format(value);
  }
  if (size > 0 && size < 1) {
    return fine.format(value);
  }
  return price.format(value);
}

export function formatChange(open: number, close: number): string {
  if (open <= 0) {
    return '';
  }
  const pct = ((close - open) / open) * 100;
  return `${pct >= 0 ? '+' : ''}${price.format(pct)}%`;
}

const money = new Intl.NumberFormat('pt-BR', {
  style: 'currency',
  currency: 'BRL',
  minimumFractionDigits: 2,
  maximumFractionDigits: 2,
});

const compactMoney = new Intl.NumberFormat('pt-BR', {
  style: 'currency',
  currency: 'BRL',
  notation: 'compact',
  maximumFractionDigits: 2,
});

export function formatCents(cents: number): string {
  return money.format(cents / 100);
}

export function formatCentsShort(cents: number): string {
  return Math.abs(cents) >= 1e8 ? compactMoney.format(cents / 100) : money.format(cents / 100);
}

export function formatSignedCents(cents: number): string {
  return `${cents > 0 ? '+' : ''}${formatCents(cents)}`;
}

export function formatPct(value: number, digits = 2): string {
  if (!Number.isFinite(value)) {
    return value > 0 ? '∞' : '—';
  }
  return `${value.toFixed(digits)}%`;
}

export function formatSignedPct(value: number, digits = 2): string {
  if (!Number.isFinite(value)) {
    return value > 0 ? '∞' : '—';
  }
  return `${value > 0 ? '+' : ''}${value.toFixed(digits)}%`;
}

export function formatRatio(value: number, digits = 2): string {
  if (!Number.isFinite(value)) {
    return value > 0 ? '∞' : '—';
  }
  return value.toFixed(digits);
}

export function formatDay(iso: string): string {
  const at = new Date(iso);
  if (Number.isNaN(at.getTime())) {
    return '—';
  }
  return formatStamp(Math.floor(at.getTime() / 1000), false);
}
