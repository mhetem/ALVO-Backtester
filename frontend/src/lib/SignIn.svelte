<script lang="ts">
  import { describe } from './api';
  import { login, register, type User } from './session';

  type Props = {
    onDone: (user: User) => void;
    onClose: () => void;
  };

  let { onDone, onClose }: Props = $props();

  let creating = $state(false);
  let email = $state('');
  let password = $state('');
  let busy = $state(false);
  let error = $state<string | null>(null);
  let field = $state<HTMLInputElement | null>(null);

  async function submit(event: SubmitEvent) {
    event.preventDefault();
    if (busy) {
      return;
    }

    busy = true;
    error = null;

    try {
      if (creating) {
        await register(email, password);
      }
      onDone(await login(email, password));
    } catch (cause) {
      error = describe(cause);
    } finally {
      busy = false;
    }
  }

  function onKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      event.stopPropagation();
      onClose();
    }
  }

  $effect(() => {
    field?.focus();
  });
</script>

<svelte:window onkeydown={onKeydown} />

<div class="backdrop">
  <button type="button" class="scrim" aria-label="Close" onclick={onClose}></button>

  <form class="dialog" onsubmit={submit} aria-label={creating ? 'Create an account' : 'Sign in'}>
    <header>
      <span class="heading">{creating ? 'Create an account' : 'Sign in'}</span>
      <button type="button" class="close" onclick={onClose} aria-label="Close">×</button>
    </header>

    <p class="lead">
      An account keeps your indicator set per symbol. Market data stays public either way.
    </p>

    <label>
      <span>Email</span>
      <input
        bind:this={field}
        bind:value={email}
        type="email"
        autocomplete="username"
        spellcheck="false"
        required
      />
    </label>

    <label>
      <span>Password</span>
      <input
        bind:value={password}
        type="password"
        autocomplete={creating ? 'new-password' : 'current-password'}
        minlength="8"
        required
      />
    </label>

    {#if error}
      <p class="bad">{error}</p>
    {/if}

    <div class="actions">
      <button type="button" class="link" onclick={() => (creating = !creating)}>
        {creating ? 'I already have an account' : 'Create an account'}
      </button>
      <button type="submit" class="go" disabled={busy}>
        {busy ? 'Working…' : creating ? 'Create' : 'Sign in'}
      </button>
    </div>
  </form>
</div>

<style>
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 60;
    display: flex;
    align-items: flex-start;
    justify-content: center;
    padding: 6rem 1rem 1rem;
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
    width: min(22rem, 100%);
    padding: 0.9rem;
    background: var(--bg);
    border: 1px solid var(--line);
    border-radius: 0.5rem;
    box-shadow: 0 1.5rem 3rem rgb(0 0 0 / 0.3);
  }

  header {
    display: flex;
    align-items: center;
  }

  .heading {
    flex: 1;
    font-size: 0.95rem;
    font-weight: 600;
  }

  .close {
    font: inherit;
    font-size: 1.2rem;
    line-height: 1;
    color: var(--muted);
    background: none;
    border: 0;
    padding: 0.1rem 0.3rem;
    cursor: pointer;
  }

  .lead {
    margin: 0;
    color: var(--muted);
    font-size: 0.78rem;
  }

  label {
    display: flex;
    flex-direction: column;
    gap: 0.2rem;
    font-size: 0.72rem;
  }

  label span {
    color: var(--muted);
    letter-spacing: 0.06em;
    text-transform: uppercase;
  }

  input {
    font: inherit;
    font-size: 0.875rem;
    color: inherit;
    background: var(--panel);
    border: 1px solid var(--line);
    border-radius: 0.3rem;
    padding: 0.35rem 0.55rem;
  }

  input:focus {
    outline: none;
    border-color: var(--accent);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--accent) 18%, transparent);
  }

  .bad {
    margin: 0;
    color: var(--bad);
    font-size: 0.78rem;
  }

  .actions {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.6rem;
    margin-top: 0.2rem;
  }

  .link {
    font: inherit;
    font-size: 0.72rem;
    color: var(--muted);
    background: none;
    border: 0;
    padding: 0;
    cursor: pointer;
    text-decoration: underline;
  }

  .link:hover {
    color: var(--fg);
  }

  .go {
    font: inherit;
    font-size: 0.8125rem;
    font-weight: 600;
    color: var(--accent-fg);
    background: var(--accent);
    border: 0;
    border-radius: 999px;
    padding: 0.3rem 1rem;
    cursor: pointer;
  }

  .go:disabled {
    opacity: 0.6;
    cursor: progress;
  }
</style>
