<script lang="ts">
  import { untrack } from 'svelte';

  import { HELP_LABELS, HELP_TOPICS, type HelpTopic } from './help';

  type Props = {
    topic?: HelpTopic;
    onClose: () => void;
  };

  let { topic = 'strategies', onClose }: Props = $props();

  // The tab is seeded from whichever panel opened help and then follows the reader's clicks.
  // The effect keeps it honest if the topic is ever changed without the panel remounting,
  // which is not reachable today only because help layers above everything that could.
  let active = $state<HelpTopic>(untrack(() => topic));

  $effect(() => {
    active = topic;
  });

  function onKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      event.stopPropagation();
      onClose();
    }
  }
</script>

<svelte:window onkeydown={onKeydown} />

<div class="backdrop">
  <button type="button" class="scrim" aria-label="Close" onclick={onClose}></button>

  <div class="dialog" role="dialog" aria-modal="true" aria-label="Help">
    <header>
      <h2>How this works</h2>
      <button type="button" class="close" onclick={onClose} aria-label="Close">×</button>
    </header>

    <div class="tabs" role="group" aria-label="Help topics">
      {#each HELP_TOPICS as id (id)}
        <button
          type="button"
          aria-pressed={active === id}
          class:on={active === id}
          onclick={() => (active = id)}
        >
          {HELP_LABELS[id]}
        </button>
      {/each}
    </div>

    <div class="body">
      {#if active === 'strategies'}
        <p class="lede">
          A strategy is a set of rules over indicators. It never sees the future: every rule is
          evaluated on a bar that has already closed.
        </p>

        <h3>Inputs</h3>
        <p>
          Give each indicator a name, and the rules refer to it by that name. An input has a
          <em>source</em> — the price field it reads (<code>close</code>, <code>hlc3</code>,
          <code>volume</code>…) — and an <em>output</em>, which matters for indicators that emit
          more than one line, like the two bands of a Bollinger or the signal line of a MACD.
        </p>

        <h3>Rules</h3>
        <p>
          Each side (long and short) has an <strong>entry</strong> and an <strong>exit</strong>.
          Rules nest: <code>all</code> requires every child, <code>any</code> requires one, and
          <code>not</code> inverts. Leave a side empty and it simply never trades.
        </p>
        <ul>
          <li>
            <code>crosses above</code> / <code>crosses below</code> need two bars to be true — the
            previous bar on one side, this one on the other. On the first bar after warmup they
            cannot fire, because there is nothing to compare against.
          </li>
          <li>
            <code>has risen for</code> / <code>has fallen for</code> count consecutive bars.
          </li>
          <li>
            An operand can look back: <em>back</em> 1 reads the previous bar's value of that input.
          </li>
        </ul>

        <h3>Stops and targets</h3>
        <p>
          A stop or target is an exit rule, quoted either as a percentage or as a multiple of ATR.
          They are checked against the bar's range, so both can be reachable inside one bar — when
          that happens the pessimistic one wins, and the run counts it. OHLC cannot tell you which
          came first, so the engine refuses to guess in your favour.
        </p>

        <h3>Sizing</h3>
        <p>
          Four shapes: a fixed number of shares, a fraction of equity, a fixed cash amount, or
          <strong>risk %</strong>. The last one sizes off the distance to the stop, so
          <em>every side that can open a position needs a stop of its own</em> — the editor rejects
          the spec otherwise, because a side without one would size at zero and silently skip every
          entry.
        </p>

        <h3>Costs</h3>
        <p>
          Brokerage in cents per trade, plus fees and slippage in basis points. They are applied on
          both sides of every round trip. Leaving them at zero does not make a strategy better; it
          makes the report wrong in the optimistic direction.
        </p>
      {:else if active === 'backtests'}
        <p class="lede">
          A run takes one strategy, one timeframe and a date range, and replays it bar by bar over
          candles already in the database. Nothing is fetched while it runs.
        </p>

        <h3>The rule that shapes everything</h3>
        <p>
          <strong>A signal on the close of a bar fills at the open of the next one.</strong> This is
          structural, not a setting. It is what keeps a backtest from buying at a price that was
          only knowable after the decision, which is the single easiest way to produce a spectacular
          and completely fictional equity curve.
        </p>
        <p class="aside">
          Futures are the exception, because they have no open: a signal on one session's settlement
          fills at the next session's settlement. Same one-bar delay, different price.
        </p>

        <h3>Warmup</h3>
        <p>
          Indicators need history before they mean anything — a 200-period average has no value on
          bar 3. The engine loads bars from <em>before</em> your start date to seed them, and no
          trade is taken until every input is ready. If a strategy seems to start late, that is why.
        </p>

        <h3>Baskets</h3>
        <p>
          Type several tickers separated by commas and the run becomes a portfolio on shared
          capital, up to 20 names. <strong>Held at once</strong> caps concurrent positions: leave it
          at the basket size for a portfolio, or lower it to rank and rotate. When more entries fire
          than slots remain, the run takes them in basket order.
        </p>

        <h3>Reading the report</h3>
        <ul>
          <li><strong>Return</strong> is what the equity curve did. <strong>CAGR</strong> annualises it, which is misleading over a window much shorter than a year.</li>
          <li><strong>Sharpe</strong> and <strong>Sortino</strong> divide return by volatility; Sortino only counts downside. <strong>Calmar</strong> divides by the worst drawdown.</li>
          <li><strong>Profit factor</strong> is gross wins over gross losses. It shows as blank when there were no losing trades — that is not an infinitely good strategy, it is too few trades.</li>
          <li><strong>Expectancy</strong> is the average trade. It is the number that survives contact with costs.</li>
        </ul>
        <p>
          A run that never opened a position is <em>not scored</em>. That is different from scoring
          zero, and it matters most in sweeps.
        </p>
      {:else if active === 'sweeps'}
        <p class="lede">
          A sweep runs the same strategy many times with different numbers. It is the fastest way to
          find a strategy that fits noise, so the design pushes you toward evidence that survives.
        </p>

        <h3>Axes</h3>
        <p>
          An axis points at one number in the spec and varies it from/to by a step. Up to 3 axes,
          and at most 200 points in the grid. Sweepable paths are an indicator parameter
          (<code>/inputs/fast/params/period</code>), the sizing value
          (<code>/sizing/value</code>), or a cost (<code>/costs/fee_bps</code>). Every point is
          built and validated before anything is queued, so a range that runs past a parameter's
          ceiling is one error now rather than half a grid failing an hour from now.
        </p>

        <h3>Grid</h3>
        <p>
          Every combination is run and scored by your objective. The heatmap shows the shape of the
          result, and <em>the shape matters more than the peak</em>: a broad plateau is a parameter
          region that works, while a lone bright cell surrounded by bad ones is almost always noise
          that will not repeat.
        </p>

        <h3>Walk-forward</h3>
        <p>
          The honest version. The window is cut into folds; in each one, the grid is optimised on
          the <strong>in-sample</strong> stretch, the single best point is taken, and that point
          alone is run on the <strong>out-of-sample</strong> stretch that follows — data it was
          never tuned on.
        </p>
        <p>
          <strong>The out-of-sample column is the answer.</strong> The in-sample score is the number
          to distrust: it is the best of many tries on data the parameters already saw, and it will
          almost always look good. A strategy whose out-of-sample results collapse was fitted, not
          discovered — that is the sweep working, not failing.
        </p>
        <p class="aside">
          A fold where no point ever traded stays unresolved and says so, rather than promoting a
          spec that does nothing into the next window.
        </p>

        <h3>Objectives</h3>
        <p>
          What "best" means when picking a fold's winner. Return picks the boldest; Sharpe, Sortino
          and Calmar penalise the path it took to get there. Expectancy favours strategies whose
          edge is per-trade rather than a run of luck.
        </p>
      {:else}
        <p class="lede">
          What is in the database, and the ways it can mislead you. None of this is fixable in a
          strategy, so it is worth knowing before you trust a number.
        </p>

        <h3>Timeframes</h3>
        <p>
          <code>5m</code> and <code>1d</code> are stored; <code>15m</code>, <code>30m</code> and
          <code>1h</code> are folded from 5m when you ask for them, anchored to the session open
          rather than the clock. A real B3 session delivers 79–83 five-minute bars, not a fixed 84 —
          bars with no trades do not exist.
        </p>

        <h3>The daily bar is not the sum of the intraday bars</h3>
        <p>
          B3's official daily close comes from the closing auction, which intraday bars never
          capture. Both are stored separately and the daily one is authoritative. A 1d backtest and
          a resampled-from-5m backtest of the same window will not agree, and the daily one is right.
        </p>

        <h3>Survivorship</h3>
        <p>
          The universe is today's index members, backfilled backwards. Companies that fell out of
          the index before the backfill are missing, and they are disproportionately the ones that
          did badly. Results over the pre-launch window are flattered by an amount nobody can
          measure. Symbols admitted from here on are kept forever, so the bias decays going forward.
        </p>

        <h3>Corporate actions</h3>
        <p>
          Splits adjust the share count; dividends are credited as cash. The two are told apart by
          the size of the adjustment, with the seam at 33% — which means a split smaller than 3:2 is
          not detectable from this data and never will be.
        </p>

        <h3>Shorts</h3>
        <p>
          Borrow cost accrues, but from a single flat default rate, and the hard-to-borrow list is
          empty. A short on a name that was genuinely expensive to borrow — or simply unavailable —
          will look better here than it would have been.
        </p>

        <h3>Futures</h3>
        <p>
          Daily settlement only, roughly 14 months deep, drawn as a line because a settlement series
          has no open, high or low to draw. Contracts are stitched into one continuous series and
          back-adjusted across each roll, which means <strong>older prices are not prices anyone
          traded</strong> — the further back you read, the further they sit from what printed.
        </p>
      {/if}
    </div>
  </div>
</div>

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 70;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 2rem 1rem;
    background: color-mix(in srgb, var(--alvo-ink) 55%, transparent);
  }

  .scrim {
    position: absolute;
    inset: 0;
    background: none;
    border: 0;
    padding: 0;
    cursor: default;
  }

  .dialog {
    position: relative;
    display: flex;
    flex-direction: column;
    gap: 0.7rem;
    width: min(48rem, 100%);
    max-height: 100%;
    padding: 0.9rem 1.1rem 1.1rem;
    background: var(--bg);
    border: 1px solid var(--line);
    border-radius: 0.5rem;
    box-shadow: 0 1.5rem 3rem rgb(0 0 0 / 0.3);
    overflow: hidden;
  }

  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  h2 {
    margin: 0;
    font-size: 0.95rem;
    letter-spacing: 0.06em;
    text-transform: uppercase;
  }

  .close {
    font: inherit;
    font-size: 1.3rem;
    line-height: 1;
    padding: 0 0.3rem;
    border: 0;
    background: none;
    color: var(--muted);
    cursor: pointer;
  }

  .tabs {
    display: flex;
    flex-wrap: wrap;
    gap: 0.3rem;
    border-bottom: 1px solid var(--line);
    padding-bottom: 0.55rem;
  }

  .tabs button {
    font: inherit;
    font-size: 0.8rem;
    padding: 0.25rem 0.6rem;
    border: 1px solid transparent;
    border-radius: 0.25rem;
    background: none;
    color: var(--muted);
    cursor: pointer;
  }

  .tabs button.on {
    border-color: var(--line);
    color: var(--fg);
  }

  .body {
    overflow-y: auto;
    padding-right: 0.4rem;
    font-size: 0.85rem;
    line-height: 1.55;
  }

  .body h3 {
    margin: 1.1rem 0 0.3rem;
    font-size: 0.7rem;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--muted);
  }

  .body p {
    margin: 0 0 0.55rem;
  }

  .body ul {
    margin: 0 0 0.55rem;
    padding-left: 1.1rem;
  }

  .body li {
    margin-bottom: 0.3rem;
  }

  .lede {
    color: var(--fg);
    border-left: 2px solid var(--line);
    padding-left: 0.7rem;
  }

  .aside {
    color: var(--muted);
    font-size: 0.8rem;
  }

  code {
    font-size: 0.9em;
    padding: 0.05rem 0.25rem;
    border-radius: 0.2rem;
    background: color-mix(in srgb, var(--muted) 15%, transparent);
  }
</style>
