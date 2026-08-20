<script lang="ts">
  import { onDestroy } from 'svelte';
  import {
    BarSeries,
    CandlestickSeries,
    createChart,
    TickMarkType,
    type IChartApi,
    type ISeriesApi,
    type Logical,
    type LogicalRange,
    type MouseEventParams,
    type SeriesType,
    type Time,
    type UTCTimestamp,
  } from 'lightweight-charts';

  import { fetchCandles, type Bar, type ChartMode, type Timeframe } from './api';
  import { axisClock, axisDay, axisPeriod, axisPeriodKey, toChartTime } from './format';
  import { barOptions, candlestickOptions, chartOptions, palette, type Palette } from './theme';

  type Props = {
    symbol: string;
    timeframe: Timeframe;
    mode: ChartMode;
    onHover: (bar: Bar | null) => void;
    onLoaded: (latest: Bar | null, count: number) => void;
  };

  let { symbol, timeframe, mode, onHover, onLoaded }: Props = $props();

  type PeriodTick = {
    key: string;
    x: number;
    label: string;
  };

  const pageLimit = 1500;
  const prefetchBars = 30;
  const minTickGap = 64;

  let host = $state<HTMLDivElement | null>(null);
  let loading = $state(false);
  let error = $state<string | null>(null);
  let empty = $state(false);
  let ticks = $state<PeriodTick[]>([]);

  let chart: IChartApi | null = null;
  let series: ISeriesApi<SeriesType> | null = null;
  let scheme: MediaQueryList | null = null;
  let sizing: ResizeObserver | null = null;
  let bars: Bar[] = [];
  let index = new Map<number, Bar>();
  let cursor: string | null = null;
  let drawn: ChartMode | null = null;
  let generation = 0;
  let pending: AbortController | null = null;

  function seriesData(list: Bar[]) {
    return list.map((bar) => ({
      time: toChartTime(bar.time) as UTCTimestamp,
      open: bar.open,
      high: bar.high,
      low: bar.low,
      close: bar.close,
    }));
  }

  function reindex() {
    index = new Map(bars.map((bar) => [toChartTime(bar.time), bar]));
  }

  function attachSeries(tone: Palette) {
    if (!chart) {
      return;
    }
    if (series) {
      chart.removeSeries(series);
      series = null;
    }

    series =
      mode === 'bars'
        ? chart.addSeries(BarSeries, barOptions(tone))
        : chart.addSeries(CandlestickSeries, candlestickOptions(tone));
    drawn = mode;

    if (bars.length > 0) {
      series.setData(seriesData(bars));
    }
  }

  function applyScheme() {
    if (!chart) {
      return;
    }
    const tone = palette();
    chart.applyOptions(chartOptions(tone, timeframe !== '1d'));
    series?.applyOptions(mode === 'bars' ? barOptions(tone) : candlestickOptions(tone));
  }

  function ensureChart() {
    if (chart || !host) {
      return;
    }

    const tone = palette();
    chart = createChart(host, chartOptions(tone, timeframe !== '1d'));
    chart.applyOptions({ timeScale: { tickMarkFormatter: tickLabel } });
    attachSeries(tone);

    chart.timeScale().subscribeVisibleLogicalRangeChange(onRange);
    chart.subscribeCrosshairMove(onCrosshair);

    scheme = window.matchMedia('(prefers-color-scheme: dark)');
    scheme.addEventListener('change', applyScheme);

    sizing = new ResizeObserver(() => updateAxis());
    sizing.observe(host);
  }

  function tickLabel(time: Time, type: TickMarkType): string {
    const seconds = typeof time === 'number' ? time : 0;
    if (timeframe === '1d') {
      return axisDay(seconds);
    }
    return type === TickMarkType.Time || type === TickMarkType.TimeWithSeconds
      ? axisClock(seconds)
      : axisDay(seconds);
  }

  function updateAxis() {
    const scale = chart?.timeScale();
    const range = scale?.getVisibleLogicalRange();
    if (!scale || !range || bars.length === 0) {
      ticks = [];
      return;
    }

    const first = Math.max(0, Math.floor(range.from));
    const last = Math.min(bars.length - 1, Math.ceil(range.to));
    if (last < first) {
      ticks = [];
      return;
    }

    const intraday = timeframe !== '1d';
    const found: PeriodTick[] = [];
    let previous = '';

    for (let i = first; i <= last; i++) {
      const at = toChartTime(bars[i].time);
      const key = axisPeriodKey(at, intraday);
      if (key === previous) {
        continue;
      }
      previous = key;

      const x = scale.timeToCoordinate(at as UTCTimestamp);
      if (x === null) {
        continue;
      }

      const previousTick = found[found.length - 1];
      if (previousTick && x - previousTick.x < minTickGap) {
        continue;
      }

      found.push({ key, x: Math.max(0, x), label: axisPeriod(at, intraday) });
    }

    ticks = found;
  }

  function onCrosshair(param: MouseEventParams) {
    const time = param.time;
    if (typeof time !== 'number') {
      onHover(null);
      return;
    }
    onHover(index.get(time) ?? null);
  }

  function onRange(range: LogicalRange | null) {
    updateAxis();
    if (!range || range.from > prefetchBars) {
      return;
    }
    void loadOlder();
  }

  function describe(cause: unknown): string {
    return cause instanceof Error ? cause.message : String(cause);
  }

  async function reload(ticker: string, tf: Timeframe) {
    generation += 1;
    const mine = generation;

    pending?.abort();
    bars = [];
    index = new Map();
    cursor = null;
    empty = false;
    error = null;
    loading = true;
    ticks = [];
    onHover(null);
    series?.setData([]);

    const controller = new AbortController();
    pending = controller;

    try {
      const page = await fetchCandles(ticker, tf, null, pageLimit, controller.signal);
      if (mine !== generation) {
        return;
      }

      bars = page.bars;
      cursor = page.cursor;
      reindex();
      empty = bars.length === 0;

      series?.setData(seriesData(bars));
      chart?.timeScale().fitContent();
      onLoaded(bars.at(-1) ?? null, bars.length);
      updateAxis();
    } catch (cause) {
      if (mine === generation && !controller.signal.aborted) {
        error = describe(cause);
      }
    } finally {
      if (mine === generation) {
        loading = false;
        pending = null;
      }
    }
  }

  async function loadOlder() {
    if (loading || !cursor) {
      return;
    }

    const mine = generation;
    const from = cursor;
    loading = true;

    const controller = new AbortController();
    pending = controller;

    try {
      const page = await fetchCandles(symbol, timeframe, from, pageLimit, controller.signal);
      if (mine !== generation) {
        return;
      }

      const older = page.bars.filter((bar) => !index.has(toChartTime(bar.time)));
      cursor = page.cursor;
      if (older.length === 0) {
        return;
      }

      const before = chart?.timeScale().getVisibleLogicalRange() ?? null;
      bars = [...older, ...bars];
      reindex();
      series?.setData(seriesData(bars));
      onLoaded(bars.at(-1) ?? null, bars.length);

      if (before) {
        chart?.timeScale().setVisibleLogicalRange({
          from: (before.from + older.length) as Logical,
          to: (before.to + older.length) as Logical,
        });
      }
      updateAxis();
    } catch (cause) {
      if (mine === generation && !controller.signal.aborted) {
        error = describe(cause);
      }
    } finally {
      if (mine === generation) {
        loading = false;
        pending = null;
      }
    }
  }

  $effect(() => {
    const ticker = symbol;
    const tf = timeframe;
    if (!host) {
      return;
    }

    ensureChart();
    chart?.applyOptions({
      timeScale: { timeVisible: tf !== '1d', tickMarkFormatter: tickLabel },
    });
    void reload(ticker, tf);
  });

  $effect(() => {
    if (mode === drawn || !chart) {
      return;
    }
    attachSeries(palette());
  });

  onDestroy(() => {
    pending?.abort();
    sizing?.disconnect();
    scheme?.removeEventListener('change', applyScheme);
    chart?.remove();
    chart = null;
    series = null;
  });
</script>

<div class="chart">
  <div class="canvas" bind:this={host}></div>

  <div class="axis" aria-hidden="true">
    {#each ticks as tick (tick.key)}
      <span class="tick" style="left: {tick.x}px">{tick.label}</span>
    {/each}
  </div>

  {#if error}
    <div class="overlay">
      <span class="arc" aria-hidden="true"></span>
      <p class="bad">{error}</p>
    </div>
  {:else if empty}
    <div class="overlay">
      <span class="arc" aria-hidden="true"></span>
      <p class="lead">No {timeframe} candles stored for {symbol}.</p>
      {#if timeframe !== '1d'}
        <p class="hint">
          Intraday is resampled from stored 5m bars, and the free brapi tier only retains about 60
          days of them. Backfill 5m for this symbol, or switch to 1d.
        </p>
      {:else}
        <p class="hint">Run <code>make backfill ARGS="--symbol {symbol} --timeframe 1d"</code>.</p>
      {/if}
    </div>
  {/if}

  {#if loading}
    <div class="spinner">loading…</div>
  {/if}
</div>

<style>
  .chart {
    position: relative;
    display: flex;
    flex-direction: column;
    flex: 1;
    min-height: 0;
  }

  .canvas {
    flex: 1;
    min-height: 0;
  }

  .axis {
    position: relative;
    height: 1.4rem;
    overflow: hidden;
    flex-shrink: 0;
  }

  .tick {
    position: absolute;
    top: 0;
    padding-left: 0.35rem;
    border-left: 1px solid var(--line);
    color: var(--muted);
    font-size: 0.68rem;
    line-height: 1.4rem;
    letter-spacing: 0.06em;
    white-space: nowrap;
    pointer-events: none;
  }

  .overlay {
    position: absolute;
    inset: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 0.6rem;
    padding: 2rem;
    text-align: center;
    overflow: hidden;
    background: var(--bg);
  }

  .arc {
    position: absolute;
    right: -14rem;
    bottom: -20rem;
    width: 38rem;
    height: 38rem;
    border: 1px solid var(--accent);
    border-radius: 50%;
    opacity: 0.35;
    pointer-events: none;
  }

  .overlay p {
    position: relative;
    margin: 0;
    max-width: 34rem;
  }

  .lead {
    font-weight: 600;
    letter-spacing: -0.01em;
  }

  .hint {
    color: var(--muted);
    font-size: 0.875rem;
  }

  .spinner {
    position: absolute;
    top: 0.85rem;
    left: 0.85rem;
    padding: 0.15rem 0.55rem;
    border-radius: 999px;
    background: var(--panel);
    border: 1px solid var(--line);
    color: var(--muted);
    font-size: 0.7rem;
    letter-spacing: 0.06em;
    text-transform: uppercase;
  }

  .bad {
    color: var(--bad);
    font-weight: 600;
  }
</style>
