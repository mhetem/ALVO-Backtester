import {
  ColorType,
  CrosshairMode,
  LineStyle,
  type BarStyleOptions,
  type CandlestickStyleOptions,
  type ChartOptions,
  type DeepPartial,
  type HistogramStyleOptions,
  type LineStyleOptions,
  type SeriesOptionsCommon,
} from 'lightweight-charts';

import { formatPrice, formatValue } from './format';

const priceFormat = {
  type: 'custom',
  minMove: 0.01,
  formatter: formatPrice,
} as const;

const valueFormat = {
  type: 'custom',
  minMove: 0.01,
  formatter: formatValue,
} as const;

export const SERIES_SLOTS = 10;

export const AUTO_SLOTS = 8;

const fallbackSeries = [
  '#3987e5',
  '#c98500',
  '#d55181',
  '#9085e9',
  '#008300',
  '#e66767',
  '#199e70',
  '#d95926',
  '#ffffff',
  '#000000',
];

export type Palette = {
  background: string;
  text: string;
  grid: string;
  border: string;
  crosshair: string;
  up: string;
  down: string;
  font: string;
};

const fallback: Palette = {
  background: '#16211f',
  text: '#93a19c',
  grid: '#202d2b',
  border: '#2c3a37',
  crosshair: '#6d7b77',
  up: '#4fa87a',
  down: '#d65420',
  font: 'ui-sans-serif, system-ui, sans-serif',
};

function token(styles: CSSStyleDeclaration, name: string, spare: string): string {
  return styles.getPropertyValue(name).trim() || spare;
}

export function palette(): Palette {
  if (typeof window === 'undefined') {
    return fallback;
  }

  const styles = getComputedStyle(document.documentElement);
  return {
    background: token(styles, '--chart-bg', fallback.background),
    text: token(styles, '--chart-text', fallback.text),
    grid: token(styles, '--chart-grid', fallback.grid),
    border: token(styles, '--chart-border', fallback.border),
    crosshair: token(styles, '--chart-crosshair', fallback.crosshair),
    up: token(styles, '--chart-up', fallback.up),
    down: token(styles, '--chart-down', fallback.down),
    font: token(styles, '--font-ui', fallback.font),
  };
}

export function seriesPalette(): string[] {
  if (typeof window === 'undefined') {
    return [...fallbackSeries];
  }

  const styles = getComputedStyle(document.documentElement);
  return fallbackSeries.map((spare, i) => token(styles, `--series-${i + 1}`, spare));
}

export function lineOptions(
  color: string,
  price: boolean,
  dotted: boolean,
): DeepPartial<LineStyleOptions & SeriesOptionsCommon> {
  return {
    color,
    lineWidth: 2,
    lineStyle: dotted ? LineStyle.Dotted : LineStyle.Solid,
    lastValueVisible: true,
    priceLineVisible: false,
    crosshairMarkerVisible: false,
    priceFormat: price ? priceFormat : valueFormat,
  };
}

export function histogramOptions(
  color: string,
  price: boolean,
): DeepPartial<HistogramStyleOptions & SeriesOptionsCommon> {
  return {
    color,
    base: 0,
    lastValueVisible: true,
    priceLineVisible: false,
    priceFormat: price ? priceFormat : valueFormat,
  };
}

export function chartOptions(tone: Palette, intraday: boolean): DeepPartial<ChartOptions> {
  return {
    layout: {
      background: { type: ColorType.Solid, color: tone.background },
      textColor: tone.text,
      attributionLogo: false,
      fontFamily: tone.font,
    },
    grid: {
      vertLines: { color: tone.grid },
      horzLines: { color: tone.grid },
    },
    rightPriceScale: {
      borderColor: tone.border,
    },
    timeScale: {
      borderColor: tone.border,
      timeVisible: intraday,
      secondsVisible: false,
      rightOffset: 4,
    },
    crosshair: {
      mode: CrosshairMode.Normal,
      vertLine: {
        color: tone.crosshair,
        style: LineStyle.Dashed,
        labelBackgroundColor: tone.crosshair,
      },
      horzLine: {
        color: tone.crosshair,
        style: LineStyle.Dashed,
        labelBackgroundColor: tone.crosshair,
      },
    },
    localization: {
      locale: 'en-US',
    },
    autoSize: true,
  };
}

export function candlestickOptions(
  tone: Palette,
): DeepPartial<CandlestickStyleOptions & SeriesOptionsCommon> {
  return {
    upColor: tone.up,
    downColor: tone.down,
    borderUpColor: tone.up,
    borderDownColor: tone.down,
    wickUpColor: tone.up,
    wickDownColor: tone.down,
    priceLineVisible: false,
    priceFormat,
  };
}

export function barOptions(tone: Palette): DeepPartial<BarStyleOptions & SeriesOptionsCommon> {
  return {
    upColor: tone.up,
    downColor: tone.down,
    thinBars: false,
    priceLineVisible: false,
    priceFormat,
  };
}

export function settlementOptions(
  tone: Palette,
): DeepPartial<LineStyleOptions & SeriesOptionsCommon> {
  return {
    color: tone.up,
    lineWidth: 2,
    priceLineVisible: false,
    crosshairMarkerVisible: true,
    priceFormat,
  };
}
