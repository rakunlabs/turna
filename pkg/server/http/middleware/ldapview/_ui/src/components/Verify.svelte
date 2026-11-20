<script lang="ts">
  import { KeyRound, LoaderCircle, CircleCheck, CircleX, X } from "@lucide/svelte";
  import { api, type VerifyResponse } from "../lib/api";
  import { app } from "../lib/state.svelte";
  import EntryCard from "./EntryCard.svelte";

  let { onclose, onopen }: { onclose: () => void; onopen: (dn: string) => void } = $props();

  const saved = (() => {
    try {
      return JSON.parse(localStorage.getItem("turna-lv-verify") ?? "{}");
    } catch {
      return {};
    }
  })();

  const defaultFilter = app.flavor === "ad" ? "(sAMAccountName=%s)" : "(uid=%s)";

  let how = $state<"direct" | "search">(saved.how ?? "search");
  let username = $state<string>(saved.username ?? "");
  let password = $state("");
  let filter = $state<string>(saved.filter ?? defaultFilter);
  let baseDN = $state<string>(saved.base_dn ?? app.baseDN);
  let busy = $state(false);
  let result = $state<VerifyResponse | null>(null);
  let error = $state<string | null>(null);

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    busy = true;
    error = null;
    result = null;
    try {
      result = await api.verify({
        username,
        password,
        filter: how === "search" ? filter : undefined,
        base_dn: how === "search" ? baseDN : undefined,
      });
      localStorage.setItem("turna-lv-verify", JSON.stringify({ how, username, filter, base_dn: baseDN }));
    } catch (err) {
      error = (err as Error).message;
    } finally {
      busy = false;
    }
  }

  function key(e: KeyboardEvent) {
    if (e.key === "Escape") onclose();
  }
</script>

<svelte:window onkeydown={key} />

<div class="fixed inset-0 z-40 flex items-start justify-center overflow-auto bg-black/30 p-4 pt-[8vh]" role="presentation" onclick={(e) => e.target === e.currentTarget && onclose()}>
  <div class="w-full max-w-[560px] rounded-xl border border-line bg-surface shadow-[var(--shadow)]" role="dialog" aria-modal="true" aria-labelledby="verify-title">
    <header class="flex items-center gap-2 border-b border-line px-4 py-3">
      <KeyRound class="size-4 text-accent-text" />
      <h2 id="verify-title" class="font-semibold">Test a login</h2>
      <button class="icon-btn ml-auto" aria-label="Close" onclick={onclose}><X class="size-4" /></button>
    </header>

    <form class="space-y-3 p-4" onsubmit={submit}>
      <p class="text-[12.5px] text-muted">
        Checks a username and password on <span class="font-mono">{app.session?.server}</span> with a separate connection, the way an application logging users in would. Your session is not changed and the password is not stored.
      </p>

      <div class="grid grid-cols-2 gap-1 rounded-lg bg-sunk p-1">
        {#each [["search", "Search, then bind"], ["direct", "Bind directly"]] as [v, label] (v)}
          <label class="flex h-8 items-center justify-center rounded-md text-[12.5px] font-medium {how === v ? 'bg-surface text-ink shadow-sm' : 'text-muted hover:text-ink'}">
            <input type="radio" class="sr-only" bind:group={how} value={v} />{label}
          </label>
        {/each}
      </div>
      <p class="-mt-1.5 text-[12px] text-faint">
        {#if how === "search"}
          Finds the user's DN with the filter using your session, then binds as that DN. Most applications (and turna's <code>auth</code> middleware) work this way.
        {:else}
          Binds with the username as given: a full DN, <code>user@domain</code>, <code>DOMAIN\user</code>, or a plain name through the server's DN template.
        {/if}
      </p>

      {#if how === "search"}
        <div class="grid grid-cols-1 gap-2 sm:grid-cols-2">
          <label class="block">
            <span class="mb-1 block text-[11.5px] font-medium text-muted">User filter <span class="font-normal text-faint">(%s = username)</span></span>
            <input class="input font-mono" bind:value={filter} spellcheck="false" required />
          </label>
          <label class="block">
            <span class="mb-1 block text-[11.5px] font-medium text-muted">Search base</span>
            <input class="input font-mono" bind:value={baseDN} spellcheck="false" placeholder="session base DN" />
          </label>
        </div>
      {/if}

      <div class="grid grid-cols-1 gap-2 sm:grid-cols-2">
        <label class="block">
          <span class="mb-1 block text-[11.5px] font-medium text-muted">Username</span>
          <input class="input font-mono" bind:value={username} spellcheck="false" autocomplete="off" required />
        </label>
        <label class="block">
          <span class="mb-1 block text-[11.5px] font-medium text-muted">Password</span>
          <input class="input" type="password" bind:value={password} autocomplete="new-password" required />
        </label>
      </div>

      <div class="flex items-center gap-2">
        <button class="btn btn-primary" disabled={busy}>
          {#if busy}<LoaderCircle class="size-3.5 animate-spin" />{/if} Test
        </button>
        {#if result}
          <span class="flex items-center gap-1.5 font-medium {result.ok ? 'text-ok' : 'text-danger'}">
            {#if result.ok}<CircleCheck class="size-4" /> Login works{:else}<CircleX class="size-4" /> Login fails{/if}
          </span>
        {/if}
      </div>

      {#if error}
        <div class="rounded-md bg-danger-soft px-3 py-2 break-words text-danger" role="alert">{error}</div>
      {/if}

      {#if result}
        <ol class="space-y-1 rounded-lg border border-line p-2">
          {#each result.steps as s, i (i)}
            <li class="flex items-start gap-2 text-[12.5px]">
              {#if s.ok}<CircleCheck class="mt-0.5 size-3.5 shrink-0 text-ok" />{:else}<CircleX class="mt-0.5 size-3.5 shrink-0 text-danger" />{/if}
              <span class="w-28 shrink-0 font-medium">{s.name}</span>
              <span class="min-w-0 flex-1 font-mono break-all {s.ok ? 'text-muted' : 'text-danger'}">{s.detail}{#if s.code} <span class="text-faint">(code {s.code})</span>{/if}</span>
              <span class="shrink-0 text-faint tnum">{s.duration_ms.toFixed(1)} ms</span>
            </li>
          {/each}
        </ol>
        {#if result.steps.some((s) => s.code === 49)}
          <p class="text-[12px] text-faint">Code 49 is "invalid credentials": wrong password, wrong DN, or a locked or expired account. Servers do not say which.</p>
        {/if}
        {#if result.entry}
          <div>
            <p class="mb-1 text-[11.5px] font-medium text-faint uppercase">Entry as seen by this user</p>
            <EntryCard entry={result.entry} onopen={(dn) => { onclose(); onopen(dn); }} compact />
          </div>
        {/if}
      {/if}
    </form>
  </div>
</div>
