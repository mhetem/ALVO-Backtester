import type { Time, UTCTimestamp } from 'lightweight-charts';

export const BAND_PAIRS: [string, string][] = [
  ['senkou_a', 'senkou_b'],
  ['upper', 'lower'],
];

export const BAND_ALPHA = 0.16;

export type BandPoint = {
  time: UTCTimestamp;
  a: number;
  b: number;
};

type MediaScope = {
  context: CanvasRenderingContext2D;
};

type RenderTarget = {
  useMediaCoordinateSpace(handler: (scope: MediaScope) => void): void;
};

type PriceScale = {
  priceToCoordinate(price: number): number | null;
};

type TimeScale = {
  timeToCoordinate(time: Time): number | null;
};

type Host = {
  chart: { timeScale(): TimeScale };
  series: PriceScale;
  requestUpdate: () => void;
};

export function bandPairOf(outputs: string[]): [string, string] | null {
  for (const pair of BAND_PAIRS) {
    if (outputs.includes(pair[0]) && outputs.includes(pair[1])) {
      return pair;
    }
  }
  return null;
}

export function withAlpha(color: string, alpha: number): string {
  const hex = color.trim();
  const short = /^#([0-9a-f])([0-9a-f])([0-9a-f])$/i.exec(hex);
  const long = /^#([0-9a-f]{2})([0-9a-f]{2})([0-9a-f]{2})$/i.exec(hex);

  if (long) {
    const [, r, g, b] = long;
    return `rgba(${parseInt(r, 16)}, ${parseInt(g, 16)}, ${parseInt(b, 16)}, ${alpha})`;
  }
  if (short) {
    const [, r, g, b] = short;
    return `rgba(${parseInt(r + r, 16)}, ${parseInt(g + g, 16)}, ${parseInt(b + b, 16)}, ${alpha})`;
  }

  return `rgba(128, 128, 128, ${alpha})`;
}

export class Band {
  private points: BandPoint[] = [];
  private above = withAlpha('#808080', BAND_ALPHA);
  private below = withAlpha('#808080', BAND_ALPHA);
  private host: Host | null = null;

  private readonly view = {
    zOrder: () => 'bottom' as const,
    renderer: () => ({
      draw: () => {},
      drawBackground: (target: RenderTarget) => this.paint(target),
    }),
  };

  attached(host: Host) {
    this.host = host;
  }

  detached() {
    this.host = null;
  }

  paneViews() {
    return [this.view];
  }

  setData(points: BandPoint[], above: string, below: string) {
    this.points = points;
    this.above = above;
    this.below = below;
    this.host?.requestUpdate();
  }

  private paint(target: RenderTarget) {
    const host = this.host;
    if (!host || this.points.length < 2) {
      return;
    }

    const scale = host.chart.timeScale();
    const placed: { x: number; a: number; b: number }[] = [];

    for (const point of this.points) {
      const x = scale.timeToCoordinate(point.time);
      const a = host.series.priceToCoordinate(point.a);
      const b = host.series.priceToCoordinate(point.b);
      if (x === null || a === null || b === null) {
        continue;
      }
      placed.push({ x, a, b });
    }

    if (placed.length < 2) {
      return;
    }

    target.useMediaCoordinateSpace(({ context }) => {
      let start = 0;

      for (let i = 1; i <= placed.length; i++) {
        const ended = i === placed.length;
        const flipped = !ended && placed[i].a <= placed[i].b !== (placed[start].a <= placed[start].b);
        if (!ended && !flipped) {
          continue;
        }

        const run = placed.slice(start, i + (ended ? 0 : 1));
        if (run.length >= 2) {
          context.beginPath();
          context.moveTo(run[0].x, run[0].a);
          for (let j = 1; j < run.length; j++) {
            context.lineTo(run[j].x, run[j].a);
          }
          for (let j = run.length - 1; j >= 0; j--) {
            context.lineTo(run[j].x, run[j].b);
          }
          context.closePath();
          context.fillStyle = run[0].a <= run[0].b ? this.above : this.below;
          context.fill();
        }

        start = i;
      }
    });
  }
}
