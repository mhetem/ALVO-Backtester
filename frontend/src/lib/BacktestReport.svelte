<script lang="ts">
  import EquityChart from './EquityChart.svelte';
  import {
    formatCents,
    formatDay,
    formatPct,
    formatPrice,
    formatRatio,
    formatSignedCents,
    formatSignedPct,
    formatStamp,
  } from './format';
  import type { Curve, Metrics, Run, Trade } from './backtest';

  type Props = {
    run: Run;
    curve: Curve | null;
    trades: Trade[];
    indexTicker: string;
  };

  let { run, curve, trades, indexTicker }: Props = $props();

  let tab = $state<'summary' | 'trades'>('summary');

  const metrics = $derived(run.metrics ?? null);
  const intraday = $derived(run.timeframe !== '1d');

  const hold = $derived(metrics?.benchmarks?.find((b) => b.kind === 'buy_and_hold') ?? null);
  const index = $derived(metrics?.benchmarks?.find((b) => b.kind === 'index') ?? null);
  const marks = $derived([hold, index].filter((mark) => mark !== null));

  function tone(value: number): 'up' | 'down' | '' {
    if (value > 0) return 'up';
    if (value < 0) return 'down';
    return '';
  }

  function factor(m: Metrics): string {
    if (m.profit_factor === null) {
      return m.wins > 0 ? 'no losing trade' : '—';
    }
    return formatRatio(m.profit_factor);
  }

  function holding(m: Metrics): string {
    if (m.trades === 0) {
      return '—';
    }
    return `${m.avg_holding_bars.toFixed(1)} bars`;
  }

  function drawdownSpan(m: Metrics): string {
    if (m.max_drawdown.pct === 0) {
      return 'never fell below its opening value';
    }
    const state = m.max_drawdown.recovered ? 'recovered' : 'still open at the end';
    return `${m.max_drawdown.bars} bars from ${formatDay(m.max_drawdown.peak_ts)}, ${state}`;
  }
</script>

<div class="report">
  {#if run.status === 'error'}
    <p class="failed">{run.error ?? 'the run failed without saying why'}</p>
  {:else if !metrics}
    <p class="waiting">This run has no metrics yet.</p>
  {:else}
    <div class="tabs" role="group" aria-label="Report section">
      <button type="button" class:on={tab === 'summary'} onclick={() => (tab = 'summary')}>
        Summary
      </button>
      <button type="button" class:on={tab === 'trades'} onclick={() => (tab = 'trades')}>
        Trades{metrics.trades > 0 ? ` · ${metrics.trades}` : ''}
      </button>
    </div>

    {#if tab === 'summary'}
      <div class="tiles">
        <div class="tile">
          <span class="key">Total return</span>
          <strong class={tone(metrics.return_pct)}>{formatSignedPct(metrics.return_pct)}</strong>
          <span class="sub">{formatSignedCents(metrics.pnl_cents)}</span>
        </div>
        <div class="tile">
          <span class="key">CAGR</span>
          <strong class={tone(metrics.cagr_pct)}>{formatSignedPct(metrics.cagr_pct)}</strong>
          <span class="sub">annualised</span>
        </div>
        <div class="tile">
          <span class="key">Volatility</span>
          <strong>{formatPct(metrics.volatility_pct)}</strong>
          <span class="sub">annualised</span>
        </div>
        <div class="tile">
          <span class="key">Max drawdown</span>
          <strong class="down">{formatPct(metrics.max_drawdown.pct)}</strong>
          <span class="sub">{formatCents(metrics.max_drawdown.cents)}</span>
        </div>
        <div class="tile">
          <span class="key">Sharpe</span>
          <strong class={tone(metrics.sharpe)}>{formatRatio(metrics.sharpe)}</strong>
          <span class="sub">over {formatPct(metrics.risk_free_pct, 2)} CDI</span>
        </div>
        <div class="tile">
          <span class="key">Sortino</span>
          <strong class={tone(metrics.sortino)}>{formatRatio(metrics.sortino)}</strong>
          <span class="sub">downside only</span>
        </div>
        <div class="tile">
          <span class="key">Calmar</span>
          <strong class={tone(metrics.calmar)}>{formatRatio(metrics.calmar)}</strong>
          <span class="sub">CAGR over drawdown</span>
        </div>
        <div class="tile">
          <span class="key">Final equity</span>
          <strong>{formatCents(metrics.final_equity_cents)}</strong>
          <span class="sub">from {formatCents(metrics.capital_cents)}</span>
        </div>
      </div>

      {#if curve}
        <EquityChart {curve} symbol={run.symbol} index={indexTicker} {intraday} />
      {/if}

      <h4>Against buying and holding</h4>
      <table class="grid">
        <thead>
          <tr>
            <th>Benchmark</th>
            <th>Return</th>
            <th>CAGR</th>
            <th>Vol</th>
            <th>Max DD</th>
            <th>Sharpe</th>
            <th>Excess</th>
            <th>Corr</th>
            <th>Beta</th>
          </tr>
        </thead>
        <tbody>
          <tr class="self">
            <td>This strategy</td>
            <td class={tone(metrics.return_pct)}>{formatSignedPct(metrics.return_pct)}</td>
            <td>{formatPct(metrics.cagr_pct)}</td>
            <td>{formatPct(metrics.volatility_pct)}</td>
            <td class="down">{formatPct(metrics.max_drawdown.pct)}</td>
            <td>{formatRatio(metrics.sharpe)}</td>
            <td>—</td>
            <td>—</td>
            <td>—</td>
          </tr>
          {#each marks as mark (mark.kind)}
            <tr>
              <td>
                {mark.kind === 'index' ? mark.symbol || indexTicker : `${mark.symbol} buy & hold`}
              </td>
              {#if mark.unavailable}
                <td class="missing" colspan="8">{mark.unavailable}</td>
              {:else}
                <td class={tone(mark.return_pct)}>{formatSignedPct(mark.return_pct)}</td>
                <td>{formatPct(mark.cagr_pct)}</td>
                <td>{formatPct(mark.volatility_pct)}</td>
                <td class="down">{formatPct(mark.max_drawdown_pct)}</td>
                <td>{formatRatio(mark.sharpe)}</td>
                <td class={tone(mark.excess_pct)}>{formatSignedPct(mark.excess_pct)}</td>
                <td>{formatRatio(mark.correlation)}</td>
                <td>{formatRatio(mark.beta)}</td>
              {/if}
            </tr>
          {/each}
        </tbody>
      </table>

      <h4>Trades</h4>
      <dl class="pairs">
        <div><dt>Count</dt><dd>{metrics.trades}</dd></div>
        <div><dt>Win rate</dt><dd>{formatPct(metrics.win_rate_pct, 1)}</dd></div>
        <div>
          <dt>Won / lost / scratched</dt>
          <dd>{metrics.wins} / {metrics.losses} / {metrics.scratches}</dd>
        </div>
        <div><dt>Profit factor</dt><dd>{factor(metrics)}</dd></div>
        <div>
          <dt>Expectancy</dt>
          <dd class={tone(metrics.expectancy_cents)}>{formatSignedCents(metrics.expectancy_cents)}</dd>
        </div>
        <div><dt>Average win</dt><dd class="up">{formatCents(metrics.avg_win_cents)}</dd></div>
        <div><dt>Average loss</dt><dd class="down">{formatCents(metrics.avg_loss_cents)}</dd></div>
        <div><dt>Largest win</dt><dd class="up">{formatCents(metrics.largest_win_cents)}</dd></div>
        <div><dt>Largest loss</dt><dd class="down">{formatCents(metrics.largest_loss_cents)}</dd></div>
        <div><dt>Longest losing streak</dt><dd>{metrics.max_consecutive_losses}</dd></div>
        <div><dt>Average holding</dt><dd>{holding(metrics)}</dd></div>
        <div><dt>Time in market</dt><dd>{formatPct(metrics.time_in_market_pct, 1)}</dd></div>
      </dl>

      <h4>How the run behaved</h4>
      <dl class="pairs">
        <div><dt>Bars</dt><dd>{metrics.bars} at {run.timeframe}</dd></div>
        <div><dt>Longest drawdown</dt><dd>{drawdownSpan(metrics)}</dd></div>
        <div>
          <dt>Exits</dt>
          <dd>
            {metrics.exits_by_signal} signal · {metrics.exits_by_stop} stop ·
            {metrics.exits_by_target} target · {metrics.exits_at_end} at the end
          </dd>
        </div>
        <div><dt>Fees paid</dt><dd>{formatCents(metrics.fees_cents)}</dd></div>
        <div>
          <dt>Dividends credited</dt>
          <dd>{formatCents(metrics.dividends_cents)} over {metrics.dividend_events} ex-dates</dd>
        </div>
      </dl>

      <div class="caveats">
        {#if metrics.basis === 'price_return'}
          <p class="warn">
            No adjusted closes cover this range, so the run is a <strong>price return</strong> —
            dividends are missing from both the strategy and its buy-and-hold benchmark.
          </p>
        {:else if metrics.unadjusted_bars > 0}
          <p class="warn">
            {metrics.unadjusted_bars} of {metrics.bars} bars carry no adjusted close, so those
            stretches are priced without dividends.
          </p>
        {/if}
        {#if metrics.unpriced_actions > 0}
          <p class="warn">
            {metrics.unpriced_actions} bar{metrics.unpriced_actions === 1 ? '' : 's'} moved too far
            to be a dividend — most likely a split. Nothing was credited for them.
          </p>
        {/if}
        {#if metrics.ambiguous_bars > 0}
          <p class="warn">
            {metrics.ambiguous_bars} bar{metrics.ambiguous_bars === 1 ? '' : 's'} hit the stop and
            the target together. The stop was assumed to fill first; a run with many of these is a
            coin flip, not a result.
          </p>
        {/if}
        {#if metrics.skipped_entries > 0}
          <p class="warn">
            {metrics.skipped_entries} entr{metrics.skipped_entries === 1 ? 'y was' : 'ies were'}
            skipped — warm-up, an unpriceable bracket, or not enough cash for one lot.
          </p>
        {/if}
        {#if metrics.risk_free_stale}
          <p class="warn">
            The run reaches past the end of the committed Selic series, so Sharpe and Sortino carry
            the last known rate forward.
          </p>
        {/if}
      </div>
    {:else}
      <table class="grid trades">
        <thead>
          <tr>
            <th>#</th>
            <th>Side</th>
            <th>Qty</th>
            <th>Entry</th>
            <th>Price</th>
            <th>Exit</th>
            <th>Price</th>
            <th>Fees</th>
            <th>Div</th>
            <th>P&amp;L</th>
            <th>Reason</th>
          </tr>
        </thead>
        <tbody>
          {#each trades as trade (trade.seq)}
            <tr>
              <td>{trade.seq}</td>
              <td>{trade.side}</td>
              <td>{trade.qty}</td>
              <td>{formatStamp(Math.floor(new Date(trade.entry_ts).getTime() / 1000), intraday)}</td>
              <td>{formatPrice(trade.entry_price)}</td>
              <td>
                {trade.exit_ts
                  ? formatStamp(Math.floor(new Date(trade.exit_ts).getTime() / 1000), intraday)
                  : '—'}
              </td>
              <td>{trade.exit_price ? formatPrice(trade.exit_price) : '—'}</td>
              <td>{formatCents(trade.fees_cents)}</td>
              <td>{trade.dividends_cents ? formatCents(trade.dividends_cents) : '—'}</td>
              <td class={tone(trade.pnl_cents)}>{formatSignedCents(trade.pnl_cents)}</td>
              <td>{trade.exit_reason ?? '—'}</td>
            </tr>
          {:else}
            <tr><td colspan="11" class="missing">This run took no trades.</td></tr>
          {/each}
        </tbody>
      </table>
    {/if}
  {/if}
</div>

<style>
  .report {
    display: flex;
    flex-direction: column;
    gap: 0.9rem;
    min-width: 0;
  }

  .tabs {
    display: flex;
    gap: 0.3rem;
  }

  .tabs button {
    font: inherit;
    font-size: 0.8rem;
    padding: 0.3rem 0.7rem;
    border: 1px solid var(--line);
    border-radius: 4px;
    background: var(--panel);
    color: var(--muted);
    cursor: pointer;
  }

  .tabs button.on {
    background: var(--hover);
    color: var(--fg);
  }

  .tiles {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(9rem, 1fr));
    gap: 0.5rem;
  }

  .tile {
    display: flex;
    flex-direction: column;
    gap: 0.15rem;
    padding: 0.55rem 0.7rem;
    border: 1px solid var(--line);
    border-radius: 5px;
    background: var(--panel);
  }

  .tile .key {
    font-size: 0.68rem;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--muted);
  }

  .tile strong {
    font-size: 1.15rem;
    font-variant-numeric: tabular-nums;
  }

  .tile .sub {
    font-size: 0.7rem;
    color: var(--muted);
  }

  h4 {
    margin: 0.3rem 0 0;
    font-size: 0.72rem;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--muted);
    font-weight: 600;
  }

  .grid {
    width: 100%;
    border-collapse: collapse;
    font-size: 0.78rem;
    font-variant-numeric: tabular-nums;
  }

  .grid th {
    text-align: right;
    font-weight: 600;
    color: var(--muted);
    padding: 0.3rem 0.5rem;
    border-bottom: 1px solid var(--line);
    white-space: nowrap;
  }

  .grid th:first-child,
  .grid td:first-child {
    text-align: left;
  }

  .grid td {
    text-align: right;
    padding: 0.3rem 0.5rem;
    border-bottom: 1px solid var(--line);
    white-space: nowrap;
  }

  .grid tr.self td {
    font-weight: 600;
  }

  .trades {
    display: block;
    max-height: 26rem;
    overflow: auto;
  }

  .pairs {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(15rem, 1fr));
    gap: 0.15rem 1.2rem;
    margin: 0;
    font-size: 0.8rem;
  }

  .pairs > div {
    display: flex;
    justify-content: space-between;
    gap: 1rem;
    padding: 0.2rem 0;
    border-bottom: 1px solid var(--line);
  }

  .pairs dt {
    color: var(--muted);
  }

  .pairs dd {
    margin: 0;
    font-variant-numeric: tabular-nums;
    text-align: right;
  }

  .caveats {
    display: flex;
    flex-direction: column;
    gap: 0.35rem;
  }

  .warn {
    margin: 0;
    padding: 0.45rem 0.6rem;
    font-size: 0.76rem;
    line-height: 1.4;
    border-left: 2px solid var(--accent);
    background: var(--panel);
    color: var(--muted);
  }

  .failed {
    margin: 0;
    padding: 0.5rem 0.6rem;
    font-size: 0.8rem;
    border-left: 2px solid var(--bad);
    background: var(--panel);
    color: var(--bad);
  }

  .waiting,
  .missing {
    color: var(--muted);
    font-size: 0.8rem;
  }

  .missing {
    text-align: left;
  }

  .up {
    color: var(--good);
  }

  .down {
    color: var(--bad);
  }
</style>
