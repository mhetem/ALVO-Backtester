<script lang="ts">
  import { onMount } from 'svelte';

  import { describe } from './api';
  import { formatDay } from './format';
  import { fetchSharedStrategy, type LegSummary, type SharedStrategy } from './strategy';

  type Props = {
    token: string;
  };

  let { token }: Props = $props();

  let strategy = $state<SharedStrategy | null>(null);
  let error = $state<string | null>(null);
  let loading = $state(true);
  let copied = $state(false);

  const body = $derived(strategy ? JSON.stringify(strategy.spec, null, 2) : '');

  function describeLeg(leg: LegSummary): string {
    if (!leg.trades) {
      return 'does not trade';
    }

    const parts = ['entry'];
    if (leg.rule_exit) {
      parts.push('rule exit');
    }
    if (leg.stop_loss) {
      parts.push('stop');
    }
    if (leg.take_profit) {
      parts.push('target');
    }

    return parts.join(', ');
  }

  async function copy() {
    try {
      await navigator.clipboard.writeText(body);
      copied = true;
      setTimeout(() => (copied = false), 1500);
    } catch (cause) {
      error = describe(cause);
    }
  }

  onMount(async () => {
    try {
      strategy = await fetchSharedStrategy(token);
    } catch (cause) {
      error = describe(cause);
    } finally {
      loading = false;
    }
  });
</script>

<main>
  <header>
    <a class="brand" href="/">
      <span class="wordmark">ALVO</span>
      <span class="target" aria-hidden="true"></span>
      <span class="product">Backtester</span>
    </a>
    <span class="badge">Shared strategy</span>
  </header>

  {#if loading}
    <p class="hint">Loading…</p>
  {:else if error}
    <p class="error">{error}</p>
    <p class="hint">
      A share link can be revoked by whoever created it, and a revoked link looks exactly like one
      that never existed.
    </p>
  {:else if strategy}
    <section class="identity">
      <h1>{strategy.name}</h1>
      {#if strategy.description}
        <p class="description">{strategy.description}</p>
      {/if}
      <p class="meta">
        Version {strategy.version} · last changed {formatDay(strategy.updated_at)}{#if strategy.shared_at}
          · shared {formatDay(strategy.shared_at)}{/if}
      </p>
    </section>

    {#if strategy.plan}
      <dl class="plan">
        <div>
          <dt>Indicators</dt>
          <dd>{strategy.plan.indicators.join(', ') || '—'}</dd>
        </div>
        <div>
          <dt>Inputs</dt>
          <dd>{strategy.plan.inputs} across {strategy.plan.slots} lines</dd>
        </div>
        <div>
          <dt>Warmup</dt>
          <dd>{strategy.plan.warmup} bars</dd>
        </div>
        <div>
          <dt>Long</dt>
          <dd>{describeLeg(strategy.plan.long)}</dd>
        </div>
        <div>
          <dt>Short</dt>
          <dd>{describeLeg(strategy.plan.short)}</dd>
        </div>
      </dl>
    {/if}

    <div class="spec">
      <div class="bar">
        <span>Spec</span>
        <button type="button" onclick={() => void copy()}>{copied ? 'Copied' : 'Copy'}</button>
      </div>
      <pre>{body}</pre>
    </div>

    <p class="hint">
      This is the spec as it was saved, and nothing else — no runs, no results, and nothing about
      who wrote it. Paste it into your own editor to run it.
    </p>
  {/if}
</main>

<style>
  main {
    max-width: 52rem;
    margin: 0 auto;
    padding: 2rem 1.25rem 4rem;
    display: flex;
    flex-direction: column;
    gap: 1.1rem;
  }

  header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 1rem;
    padding-bottom: 0.8rem;
    border-bottom: 1px solid var(--line);
  }

  .brand {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    text-decoration: none;
    color: inherit;
  }

  .wordmark {
    font-weight: 700;
    letter-spacing: 0.14em;
  }

  .target {
    width: 0.55rem;
    height: 0.55rem;
    border-radius: 50%;
    border: 2px solid var(--accent);
  }

  .product {
    font-size: 0.8rem;
    color: var(--muted);
    letter-spacing: 0.06em;
    text-transform: uppercase;
  }

  .badge {
    font-size: 0.68rem;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--muted);
    border: 1px solid var(--line);
    border-radius: 999px;
    padding: 0.15rem 0.6rem;
  }

  h1 {
    margin: 0;
    font-size: 1.35rem;
  }

  .identity {
    display: flex;
    flex-direction: column;
    gap: 0.35rem;
  }

  .description {
    margin: 0;
    font-size: 0.9rem;
    line-height: 1.5;
  }

  .meta {
    margin: 0;
    font-size: 0.74rem;
    color: var(--muted);
  }

  .plan {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(11rem, 1fr));
    gap: 0.6rem;
    margin: 0;
  }

  .plan div {
    border: 1px solid var(--line);
    border-radius: 5px;
    padding: 0.5rem 0.6rem;
    background: var(--panel);
  }

  dt {
    font-size: 0.66rem;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--muted);
  }

  dd {
    margin: 0.2rem 0 0;
    font-size: 0.84rem;
  }

  .spec {
    border: 1px solid var(--line);
    border-radius: 5px;
    overflow: hidden;
  }

  .bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0.4rem 0.6rem;
    background: var(--panel);
    border-bottom: 1px solid var(--line);
    font-size: 0.68rem;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--muted);
  }

  .bar button {
    font: inherit;
    font-size: 0.68rem;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    padding: 0.2rem 0.6rem;
    border: 1px solid var(--line);
    border-radius: 4px;
    background: var(--bg);
    color: var(--fg);
    cursor: pointer;
  }

  pre {
    margin: 0;
    padding: 0.8rem;
    overflow-x: auto;
    font-size: 0.78rem;
    line-height: 1.5;
  }

  .hint {
    margin: 0;
    font-size: 0.8rem;
    color: var(--muted);
    line-height: 1.5;
  }

  .error {
    margin: 0;
    padding: 0.5rem 0.7rem;
    font-size: 0.85rem;
    border-left: 2px solid var(--bad);
    background: var(--panel);
    color: var(--bad);
  }
</style>
