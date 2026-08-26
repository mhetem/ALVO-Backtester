<script lang="ts">
  import { onDestroy } from 'svelte';
  import {
    BarSeries,
    CandlestickSeries,
    HistogramSeries,
    LineSeries,
    createChart,
    createSeriesMarkers,
    LineStyle,
    TickMarkType,
    type ISeriesMarkersPluginApi,
    type SeriesMarker,
    type IChartApi,
    type ISeriesApi,
    type Logical,
    type LogicalRange,
    type MouseEventParams,
    type SeriesType,
    type Time,
    type UTCTimestamp,
  } from 'lightweight-charts';

  import {
    describe,
    fetchCandles,
    MAX_PAGE_LIMIT,
    type Bar,
    type ChartMode,
    type IndicatorResult,
    type Timeframe,
  } from './api';
  import {
    axisClock,
    axisDay,
    axisPeriod,
    axisPeriodKey,
    formatPrice,
    formatValue,
    toChartTime,
  } from './format';
  import { drawnOutputs, slotOf, HISTOGRAM_OUTPUT, type ActiveIndicator } from './indicators';
  import type { TradeMark } from './backtest';
  import { Band, bandPairOf, withAlpha, BAND_ALPHA, type BandPoint } from './band';
  import {
    barOptions,
    candlestickOptions,
    chartOptions,
    histogramOptions,
    lineOptions,
    palette,
    type Palette,
  } from './theme';

  type Props = {
    symbol: string;
    timeframe: Timeframe;
    mode: ChartMode;
    indicators: ActiveIndicator[];
    colors: string[];
    trades: TradeMark[];
    onHover: (bar: Bar | null) => void;
    onLoaded: (latest: Bar | null, count: number) => void;
  };

  let { symbol, timeframe, mode, indicators, colors, trades, onHover, onLoaded }: Props = $props();

  type PeriodTick = {
    key: string;
    x: number;
    label: string;
  };

  type LegendPart = {
    name: string;
    color: string;
    text: string;
  };

  type LegendRow = {
    key: string;
    parts: LegendPart[];
  };

  type Column = (number | null)[];

  type Handle = {
    key: string;
    output: string;
    api: ISeriesApi<SeriesType>;
  };

  type Cloud = {
    key: string;
    pair: [string, string];
    host: ISeriesApi<SeriesType>;
    band: Band;
  };

  const pageLimit = 1500;
  const prefetchBars = 30;
  const minTickGap = 64;
  const pricePaneStretch = 4;

  let host = $state<HTMLDivElement | null>(null);
  let loading = $state(false);
  let error = $state<string | null>(null);
  let empty = $state(false);
  let ticks = $state<PeriodTick[]>([]);
  let legend = $state<LegendRow[]>([]);

  let chart: IChartApi | null = null;
  let series: ISeriesApi<SeriesType> | null = null;
  let markers: ISeriesMarkersPluginApi<Time> | null = null;
  let scheme: MediaQueryList | null = null;
  let sizing: ResizeObserver | null = null;
  let bars: Bar[] = [];
  let index = new Map<number, number>();
  let values = new Map<string, Column[]>();
  let offsets = new Map<string, number[]>();
  let future: number[] = [];
  let handles: Handle[] = [];
  let clouds: Cloud[] = [];
  let cursor: string | null = null;
  let drawn: ChartMode | null = null;
  let drawnShape = '';
  let loadedSymbol = '';
  let loadedTimeframe: Timeframe | null = null;
  let requested = '';
  let hovered: number | null = null;
  let generation = 0;
  let pending: AbortController | null = null;

  function requestKeys(list: ActiveIndicator[]): string {
    return list.map((active) => active.key).join(',');
  }

  function shapeOf(list: ActiveIndicator[]): string {
    return list
      .filter((active) => active.visible)
      .map((active) => `${active.key}=${drawnOutputs(active).join('|')}`)
      .join(',');
  }

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
    index = new Map(bars.map((bar, i) => [toChartTime(bar.time), i]));
  }

  function align(results: IndicatorResult[], count: number): Map<string, Column[]> {
    const out = new Map<string, Column[]>();

    for (const result of results) {
      const columns = result.series.map((line) => {
        const column: Column = new Array(count).fill(null);
        for (let i = 0; i < line.values.length; i++) {
          const at = line.start + i;
          if (at >= 0 && at < count) {
            column[at] = line.values[i];
          }
        }
        return column;
      });
      out.set(result.key, columns);
    }

    return out;
  }

  function timeAt(at: number): UTCTimestamp | null {
    if (at < 0) {
      return null;
    }
    if (at < bars.length) {
      return toChartTime(bars[at].time) as UTCTimestamp;
    }
    const ahead = at - bars.length;
    return ahead < future.length ? (future[ahead] as UTCTimestamp) : null;
  }

  function offsetOf(active: ActiveIndicator, output: string): number {
    const at = active.outputs.indexOf(output);
    return offsets.get(active.key)?.[at] ?? 0;
  }

  function readOffsets(results: IndicatorResult[]): Map<string, number[]> {
    return new Map(results.map((result) => [result.key, result.offsets ?? []]));
  }

  function blank(count: number): Column {
    return new Array(count).fill(null);
  }

  function mergeValues(
    older: Map<string, Column[]>,
    olderCount: number,
    newer: Map<string, Column[]>,
    newerCount: number,
  ): Map<string, Column[]> {
    const out = new Map<string, Column[]>();

    for (const key of new Set([...older.keys(), ...newer.keys()])) {
      const head = older.get(key);
      const tail = newer.get(key);
      const width = Math.max(head?.length ?? 0, tail?.length ?? 0);
      const columns: Column[] = [];

      for (let i = 0; i < width; i++) {
        columns.push([
          ...(head?.[i] ?? blank(olderCount)),
          ...(tail?.[i] ?? blank(newerCount)),
        ]);
      }

      out.set(key, columns);
    }

    return out;
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
        ? chart.addSeries(BarSeries, barOptions(tone), 0)
        : chart.addSeries(CandlestickSeries, candlestickOptions(tone), 0);
    drawn = mode;
    markers = createSeriesMarkers(series, []);

    if (bars.length > 0) {
      series.setData(seriesData(bars));
    }
    drawMarkers();
  }

  // Trades are drawn against the loaded page, so a fill outside the visible range simply
  // has no marker rather than snapping to the nearest bar it can find.
  function drawMarkers() {
    if (!markers) {
      return;
    }
    if (trades.length === 0 || bars.length === 0) {
      markers.setMarkers([]);
      return;
    }

    const tone = palette();
    const first = bars[0].time;
    const last = bars[bars.length - 1].time;
    const marks: SeriesMarker<Time>[] = [];

    for (const trade of trades) {
      if (trade.entry >= first && trade.entry <= last) {
        marks.push({
          time: toChartTime(trade.entry) as UTCTimestamp,
          position: 'belowBar',
          shape: 'arrowUp',
          color: tone.up,
          text: `#${trade.seq} in`,
        });
      }
      if (trade.exit !== null && trade.exit >= first && trade.exit <= last) {
        marks.push({
          time: toChartTime(trade.exit) as UTCTimestamp,
          position: 'aboveBar',
          shape: 'arrowDown',
          color: trade.won ? tone.up : tone.down,
          text: `#${trade.seq} ${trade.reason}`,
        });
      }
    }

    marks.sort((a, b) => Number(a.time) - Number(b.time));
    markers.setMarkers(marks);
  }

  function applyScheme() {
    if (!chart) {
      return;
    }
    const tone = palette();
    chart.applyOptions(chartOptions(tone, timeframe !== '1d'));
    series?.applyOptions(mode === 'bars' ? barOptions(tone) : candlestickOptions(tone));
    drawMarkers();
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

  function rebuildIndicators() {
    if (!chart) {
      return;
    }

    for (const cloud of clouds) {
      cloud.host.detachPrimitive(cloud.band);
    }
    clouds = [];

    for (const handle of handles) {
      chart.removeSeries(handle.api);
    }
    handles = [];

    while (chart.panes().length > 1) {
      chart.removePane(chart.panes().length - 1);
    }

    let nextPane = 1;

    for (const active of indicators) {
      if (!active.visible) {
        continue;
      }

      const outputs = drawnOutputs(active);
      if (outputs.length === 0) {
        continue;
      }

      const paneIndex = active.overlay ? 0 : nextPane++;
      while (chart.panes().length <= paneIndex) {
        chart.addPane();
      }

      for (const output of outputs) {
        const color = colors[slotOf(active, output)] ?? colors[0];
        const api =
          output === HISTOGRAM_OUTPUT
            ? chart.addSeries(HistogramSeries, histogramOptions(color, active.overlay), paneIndex)
            : chart.addSeries(
                LineSeries,
                lineOptions(color, active.overlay, active.style === 'dotted'),
                paneIndex,
              );
        handles.push({ key: active.key, output, api });
      }

      const pair = bandPairOf(outputs);
      const anchor = handles.find(
        (handle) => handle.key === active.key && handle.output === pair?.[0],
      );
      if (pair && anchor) {
        const band = new Band();
        anchor.api.attachPrimitive(band);
        clouds.push({ key: active.key, pair, host: anchor.api, band });
      }
    }

    const panes = chart.panes();
    if (panes.length > 1) {
      panes[0].setStretchFactor(pricePaneStretch);
      for (let i = 1; i < panes.length; i++) {
        panes[i].setStretchFactor(1);
      }
    }
  }

  function applyIndicatorLook() {
    for (const handle of handles) {
      const active = indicators.find((candidate) => candidate.key === handle.key);
      if (!active) {
        continue;
      }

      const color = colors[slotOf(active, handle.output)] ?? colors[0];
      handle.api.applyOptions(
        handle.output === HISTOGRAM_OUTPUT
          ? { color }
          : { color, lineStyle: active.style === 'dotted' ? LineStyle.Dotted : LineStyle.Solid },
      );
    }
  }

  function applyClouds() {
    for (const cloud of clouds) {
      const active = indicators.find((candidate) => candidate.key === cloud.key);
      if (!active) {
        cloud.band.setData([], 'transparent', 'transparent');
        continue;
      }

      const columns = values.get(cloud.key);
      const above = columns?.[active.outputs.indexOf(cloud.pair[0])];
      const below = columns?.[active.outputs.indexOf(cloud.pair[1])];
      const points: BandPoint[] = [];

      if (above && below) {
        const shift = offsetOf(active, cloud.pair[0]);
        for (let i = 0; i < bars.length && i < above.length && i < below.length; i++) {
          const a = above[i];
          const b = below[i];
          const time = a === null || b === null ? null : timeAt(i + shift);
          if (a !== null && b !== null && time !== null) {
            points.push({ time, a, b });
          }
        }
      }

      cloud.band.setData(
        points,
        withAlpha(colors[slotOf(active, cloud.pair[0])] ?? colors[0], BAND_ALPHA),
        withAlpha(colors[slotOf(active, cloud.pair[1])] ?? colors[0], BAND_ALPHA),
      );
    }
  }

  function applyIndicatorData() {
    for (const handle of handles) {
      const active = indicators.find((candidate) => candidate.key === handle.key);
      const columns = values.get(handle.key);
      const column = active ? columns?.[active.outputs.indexOf(handle.output)] : undefined;

      if (!active || !column) {
        handle.api.setData([]);
        continue;
      }

      const shift = offsetOf(active, handle.output);
      const points: { time: UTCTimestamp; value: number }[] = [];

      for (let i = 0; i < bars.length && i < column.length; i++) {
        const value = column[i];
        const time = value === null ? null : timeAt(i + shift);
        if (value !== null && time !== null) {
          points.push({ time, value });
        }
      }
      handle.api.setData(points);
    }

    applyClouds();
  }

  function syncIndicators() {
    if (!chart) {
      return;
    }

    const shape = shapeOf(indicators);
    if (shape !== drawnShape) {
      rebuildIndicators();
      drawnShape = shape;
    }

    applyIndicatorLook();
    applyIndicatorData();
    updateLegend();
  }

  function updateLegend() {
    const at = hovered ?? bars.length - 1;
    if (at < 0) {
      legend = [];
      return;
    }

    const rows: LegendRow[] = [];

    for (const active of indicators) {
      if (!active.visible) {
        continue;
      }

      const columns = values.get(active.key);
      const format = active.overlay ? formatPrice : formatValue;
      const parts = drawnOutputs(active).map((output) => {
        const source = at - offsetOf(active, output);
        const value = columns?.[active.outputs.indexOf(output)]?.[source];
        return {
          name: output,
          color: colors[slotOf(active, output)] ?? colors[0],
          text: value === null || value === undefined ? '—' : format(value),
        };
      });

      rows.push({ key: active.key, parts });
    }

    legend = rows;
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
    const at = typeof time === 'number' ? (index.get(time) ?? null) : null;

    hovered = at;
    onHover(at === null ? null : bars[at]);
    updateLegend();
  }

  function onRange(range: LogicalRange | null) {
    updateAxis();
    if (!range || range.from > prefetchBars) {
      return;
    }
    void loadOlder();
  }

  async function reload(ticker: string, tf: Timeframe, keys: string) {
    generation += 1;
    const mine = generation;

    pending?.abort();
    bars = [];
    index = new Map();
    values = new Map();
    offsets = new Map();
    future = [];
    cursor = null;
    hovered = null;
    empty = false;
    error = null;
    loading = true;
    ticks = [];
    legend = [];
    onHover(null);
    series?.setData([]);

    const controller = new AbortController();
    pending = controller;

    try {
      const page = await fetchCandles(ticker, tf, null, pageLimit, keys, controller.signal);
      if (mine !== generation) {
        return;
      }

      bars = page.bars;
      cursor = page.cursor;
      values = align(page.indicators, bars.length);
      offsets = readOffsets(page.indicators);
      future = page.future.map(toChartTime);
      reindex();
      empty = bars.length === 0;

      series?.setData(seriesData(bars));
      drawMarkers();
      chart?.timeScale().fitContent();
      applyIndicatorData();
      onLoaded(bars.at(-1) ?? null, bars.length);
      updateAxis();
      updateLegend();
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

  async function refresh(keys: string) {
    if (bars.length === 0) {
      void reload(symbol, timeframe, keys);
      return;
    }

    generation += 1;
    const mine = generation;

    pending?.abort();
    error = null;
    loading = true;

    const controller = new AbortController();
    pending = controller;
    const before = chart?.timeScale().getVisibleLogicalRange() ?? null;
    const want = Math.min(bars.length, MAX_PAGE_LIMIT);

    try {
      const page = await fetchCandles(symbol, timeframe, null, want, keys, controller.signal);
      if (mine !== generation) {
        return;
      }

      const shift = page.bars.length - bars.length;
      bars = page.bars;
      cursor = page.cursor;
      values = align(page.indicators, bars.length);
      offsets = readOffsets(page.indicators);
      future = page.future.map(toChartTime);
      reindex();
      empty = bars.length === 0;
      hovered = null;

      series?.setData(seriesData(bars));
      drawMarkers();
      applyIndicatorData();
      onLoaded(bars.at(-1) ?? null, bars.length);

      if (before && shift !== 0) {
        chart?.timeScale().setVisibleLogicalRange({
          from: (before.from + shift) as Logical,
          to: (before.to + shift) as Logical,
        });
      }
      updateAxis();
      updateLegend();
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
    const keys = requested;
    loading = true;

    const controller = new AbortController();
    pending = controller;

    try {
      const page = await fetchCandles(symbol, timeframe, from, pageLimit, keys, controller.signal);
      if (mine !== generation) {
        return;
      }

      const keep: number[] = [];
      for (let i = 0; i < page.bars.length; i++) {
        if (!index.has(toChartTime(page.bars[i].time))) {
          keep.push(i);
        }
      }

      cursor = page.cursor;
      if (keep.length === 0) {
        return;
      }

      offsets = new Map([...offsets, ...readOffsets(page.indicators)]);

      const olderValues = new Map<string, Column[]>();
      for (const [key, columns] of align(page.indicators, page.bars.length)) {
        olderValues.set(
          key,
          columns.map((column) => keep.map((at) => column[at])),
        );
      }

      const before = chart?.timeScale().getVisibleLogicalRange() ?? null;
      const older = keep.map((at) => page.bars[at]);

      values = mergeValues(olderValues, older.length, values, bars.length);
      bars = [...older, ...bars];
      reindex();

      series?.setData(seriesData(bars));
      drawMarkers();
      applyIndicatorData();
      onLoaded(bars.at(-1) ?? null, bars.length);

      if (before) {
        chart?.timeScale().setVisibleLogicalRange({
          from: (before.from + older.length) as Logical,
          to: (before.to + older.length) as Logical,
        });
      }
      updateAxis();
      updateLegend();
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
    const keys = requestKeys(indicators);
    if (!host) {
      return;
    }

    ensureChart();

    if (ticker !== loadedSymbol || tf !== loadedTimeframe) {
      loadedSymbol = ticker;
      loadedTimeframe = tf;
      requested = keys;
      chart?.applyOptions({
        timeScale: { timeVisible: tf !== '1d', tickMarkFormatter: tickLabel },
      });
      void reload(ticker, tf, keys);
      return;
    }

    if (keys === requested) {
      return;
    }
    requested = keys;

    if (keys === '') {
      values = new Map();
      offsets = new Map();
      future = [];
      syncIndicators();
      return;
    }
    void refresh(keys);
  });

  $effect(() => {
    void trades.map((trade) => `${trade.seq}:${trade.entry}:${trade.exit}`).join(',');
    if (!chart) {
      return;
    }
    drawMarkers();
  });

  $effect(() => {
    if (mode === drawn || !chart) {
      return;
    }
    attachSeries(palette());
    drawnShape = '';
    syncIndicators();
  });

  $effect(() => {
    void indicators
      .map((active) => `${active.key}|${active.visible}|${active.style}|${active.colors.join('-')}`)
      .join(',');
    void colors.join(',');
    if (!chart) {
      return;
    }
    syncIndicators();
  });

  onDestroy(() => {
    pending?.abort();
    sizing?.disconnect();
    scheme?.removeEventListener('change', applyScheme);
    chart?.remove();
    chart = null;
    series = null;
    markers = null;
    handles = [];
    clouds = [];
  });
</script>

<div class="chart">
  <div class="canvas" bind:this={host}></div>

  {#if legend.length > 0}
    <div class="legend" aria-live="off">
      {#each legend as row (row.key)}
        <div class="row">
          <span class="key">{row.key}</span>
          {#each row.parts as part (part.name)}
            <span class="part">
              <i class="dot" style="background: {part.color}"></i>
              <span class="value">{part.text}</span>
            </span>
          {/each}
        </div>
      {/each}
    </div>
  {/if}

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
    min-width: 0;
  }

  .canvas {
    flex: 1;
    min-height: 0;
  }

  .legend {
    position: absolute;
    top: 0.6rem;
    left: 0.85rem;
    display: flex;
    flex-direction: column;
    gap: 0.15rem;
    pointer-events: none;
    font-size: 0.7rem;
    font-variant-numeric: tabular-nums;
    max-width: calc(100% - 8rem);
  }

  .legend .row {
    display: flex;
    align-items: baseline;
    gap: 0.55rem;
    white-space: nowrap;
  }

  .legend .key {
    color: var(--muted);
    letter-spacing: 0.04em;
  }

  .legend .part {
    display: inline-flex;
    align-items: baseline;
    gap: 0.28rem;
  }

  .legend .dot {
    width: 0.4rem;
    height: 0.4rem;
    border-radius: 50%;
    box-shadow: 0 0 0 1px var(--line);
    align-self: center;
  }

  .legend .value {
    color: var(--fg);
    font-weight: 600;
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
    right: 4.5rem;
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
