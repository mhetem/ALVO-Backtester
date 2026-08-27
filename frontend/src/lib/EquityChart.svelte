<script lang="ts">
  import { onDestroy } from 'svelte';
  import {
    AreaSeries,
    LineSeries,
    createChart,
    type IChartApi,
    type ISeriesApi,
    type SeriesType,
    type UTCTimestamp,
  } from 'lightweight-charts';

  import { formatCentsShort, formatPct, toChartTime } from './format';
  import { chartOptions, palette, seriesPalette } from './theme';
  import type { Curve, SleeveCurve } from './backtest';

  type Props = {
    curve: Curve;
    symbol: string;
    index: string;
    intraday: boolean;
  };

  let { curve, symbol, index, intraday }: Props = $props();

  let equityBox: HTMLDivElement;
  let underwaterBox: HTMLDivElement;

  let equityChart: IChartApi | null = null;
  let underwaterChart: IChartApi | null = null;
  let lines: ISeriesApi<SeriesType>[] = [];
  let showSleeves = $state(false);

  const sleeves = $derived(curve.symbols ?? []);

  function sleeveColor(i: number): string {
    const colors = seriesPalette();
    return colors[(i + 4) % colors.length];
  }

  const legend = $derived(
    [
      { name: 'Strategy', color: seriesPalette()[0], on: true },
      { name: `${symbol} buy & hold`, color: seriesPalette()[1], on: (curve.hold?.length ?? 0) > 0 },
      { name: index, color: seriesPalette()[3], on: (curve.index?.length ?? 0) > 0 },
      ...sleeves.map((sleeve, i) => ({
        name: sleeve.symbol,
        color: sleeveColor(i),
        on: showSleeves,
      })),
    ].filter((row) => row.on),
  );

  function points(values: number[] | undefined) {
    if (!values || values.length !== curve.ts.length) {
      return [];
    }
    return values.map((value, i) => ({
      time: toChartTime(curve.ts[i]) as UTCTimestamp,
      value: value / 100,
    }));
  }

  // A sleeve holds a fraction of the capital, so plotted in cents it would sit in a band of
  // its own and flatten the aggregate. Rebasing every sleeve to the run's opening value puts
  // them all on one axis, where the question is which stock bent the curve and when.
  function sleevePoints(sleeve: SleeveCurve) {
    const base = sleeve.equity[0];
    const start = curve.equity[0];
    if (!base || !start || sleeve.equity.length !== curve.ts.length) {
      return [];
    }
    return sleeve.equity.map((value, i) => ({
      time: toChartTime(curve.ts[i]) as UTCTimestamp,
      value: ((value / base) * start) / 100,
    }));
  }

  function teardown() {
    equityChart?.remove();
    underwaterChart?.remove();
    equityChart = null;
    underwaterChart = null;
    lines = [];
  }

  function build() {
    teardown();
    if (!equityBox || !underwaterBox || curve.ts.length === 0) {
      return;
    }

    const tone = palette();
    const colors = seriesPalette();

    equityChart = createChart(equityBox, {
      ...chartOptions(tone, intraday),
      rightPriceScale: { borderColor: tone.border, scaleMargins: { top: 0.1, bottom: 0.1 } },
      handleScale: false,
      handleScroll: false,
    });

    const money = {
      lineWidth: 2 as const,
      priceLineVisible: false,
      lastValueVisible: true,
      crosshairMarkerVisible: true,
      priceFormat: { type: 'custom', minMove: 0.01, formatter: formatCentsShort } as const,
    };

    const strategy = equityChart.addSeries(LineSeries, { ...money, color: colors[0] });
    strategy.setData(points(curve.equity));
    lines.push(strategy);

    const hold = points(curve.hold);
    if (hold.length > 0) {
      const series = equityChart.addSeries(LineSeries, { ...money, color: colors[1], lineWidth: 1 });
      series.setData(hold);
      lines.push(series);
    }

    const bench = points(curve.index);
    if (bench.length > 0) {
      const series = equityChart.addSeries(LineSeries, { ...money, color: colors[3], lineWidth: 1 });
      series.setData(bench);
      lines.push(series);
    }

    if (showSleeves) {
      for (const [i, sleeve] of sleeves.entries()) {
        const data = sleevePoints(sleeve);
        if (data.length === 0) {
          continue;
        }
        const series = equityChart.addSeries(LineSeries, {
          ...money,
          color: sleeveColor(i),
          lineWidth: 1,
          lastValueVisible: false,
        });
        series.setData(data);
        lines.push(series);
      }
    }

    underwaterChart = createChart(underwaterBox, {
      ...chartOptions(tone, intraday),
      rightPriceScale: { borderColor: tone.border, scaleMargins: { top: 0.05, bottom: 0.05 } },
      handleScale: false,
      handleScroll: false,
    });

    const water = underwaterChart.addSeries(AreaSeries, {
      lineColor: tone.down,
      topColor: 'transparent',
      bottomColor: `${tone.down}55`,
      lineWidth: 1,
      priceLineVisible: false,
      priceFormat: { type: 'custom', minMove: 0.01, formatter: (v: number) => formatPct(v, 1) },
    });
    water.setData(
      curve.drawdown.map((value, i) => ({
        time: toChartTime(curve.ts[i]) as UTCTimestamp,
        value,
      })),
    );

    equityChart.timeScale().fitContent();
    underwaterChart.timeScale().fitContent();
    sync(equityChart, underwaterChart);
    sync(underwaterChart, equityChart);
  }

  function sync(from: IChartApi, to: IChartApi) {
    from.timeScale().subscribeVisibleLogicalRangeChange((range) => {
      if (range) {
        to.timeScale().setVisibleLogicalRange(range);
      }
    });
  }

  $effect(() => {
    void curve;
    void intraday;
    void showSleeves;
    build();
  });

  onDestroy(teardown);
</script>

<div class="curves">
  <div class="legend">
    {#each legend as row (row.name)}
      <span><i style:background={row.color}></i>{row.name}</span>
    {/each}
    {#if sleeves.length > 0}
      <button type="button" class="toggle" class:on={showSleeves} onclick={() => (showSleeves = !showSleeves)}>
        {showSleeves ? 'Hide' : 'Show'} each stock
      </button>
    {/if}
    {#if curve.sampled}
      <span class="note">{curve.count} of {curve.total} points shown</span>
    {/if}
  </div>

  {#if showSleeves}
    <p class="rebased">Each stock is rebased to the run's opening value, so the lines compare shape rather than size.</p>
  {/if}

  <div class="pane equity" bind:this={equityBox}></div>

  <div class="label">Drawdown</div>
  <div class="pane water" bind:this={underwaterBox}></div>
</div>

<style>
  .curves {
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
  }

  .legend {
    display: flex;
    flex-wrap: wrap;
    gap: 0.9rem;
    font-size: 0.75rem;
    color: var(--muted);
  }

  .legend span {
    display: inline-flex;
    align-items: center;
    gap: 0.35rem;
  }

  .legend i {
    width: 0.7rem;
    height: 0.2rem;
    border-radius: 1px;
  }

  .legend .note {
    margin-left: auto;
    font-variant-numeric: tabular-nums;
  }

  .toggle {
    font: inherit;
    font-size: 0.7rem;
    color: var(--muted);
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: 999px;
    padding: 0.1rem 0.55rem;
    cursor: pointer;
  }

  .toggle:hover,
  .toggle.on {
    color: var(--fg);
    background: var(--hover);
  }

  .rebased {
    margin: 0;
    font-size: 0.7rem;
    color: var(--muted);
  }

  .label {
    font-size: 0.7rem;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--muted);
  }

  .pane {
    width: 100%;
  }

  .equity {
    height: 260px;
  }

  .water {
    height: 110px;
  }
</style>
