<script lang="ts">
  type Health = { status: string; db: string };

  let health = $state<Health | null>(null);
  let error = $state<string | null>(null);

  async function check() {
    error = null;
    try {
      const res = await fetch('/api/v1/healthz');
      health = (await res.json()) as Health;
      if (!res.ok) {
        error = `server returned ${res.status}`;
      }
    } catch (e) {
      health = null;
      error = e instanceof Error ? e.message : String(e);
    }
  }

  $effect(() => {
    void check();
  });
</script>

<main>
  <h1>ALVO Backtester</h1>
  <p class="sub">Phase 0 — foundations. No market data yet.</p>

  <section class="card">
    <h2>Health</h2>
    {#if error}
      <p class="bad">{error}</p>
    {:else if health}
      <dl>
        <dt>status</dt>
        <dd class={health.status === 'ok' ? 'good' : 'bad'}>{health.status}</dd>
        <dt>database</dt>
        <dd class={health.db === 'up' ? 'good' : 'bad'}>{health.db}</dd>
      </dl>
    {:else}
      <p>checking…</p>
    {/if}
    <button onclick={check}>Re-check</button>
  </section>
</main>

<style>
  main {
    max-width: 34rem;
    margin: 4rem auto;
    padding: 0 1.5rem;
  }

  h1 {
    margin: 0;
    font-size: 1.75rem;
    letter-spacing: -0.02em;
  }

  .sub {
    margin: 0.25rem 0 2rem;
    color: var(--muted);
  }

  .card {
    border: 1px solid var(--line);
    border-radius: 0.5rem;
    padding: 1.25rem 1.5rem;
  }

  h2 {
    margin: 0 0 1rem;
    font-size: 0.8rem;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    color: var(--muted);
  }

  dl {
    display: grid;
    grid-template-columns: 6rem 1fr;
    gap: 0.4rem 1rem;
    margin: 0 0 1.25rem;
  }

  dt {
    color: var(--muted);
  }

  dd {
    margin: 0;
    font-variant-numeric: tabular-nums;
  }

  .good {
    color: var(--good);
  }

  .bad {
    color: var(--bad);
  }

  button {
    font: inherit;
    color: inherit;
    background: none;
    border: 1px solid var(--line);
    border-radius: 0.375rem;
    padding: 0.4rem 0.9rem;
    cursor: pointer;
  }

  button:hover {
    border-color: var(--fg);
  }
</style>
