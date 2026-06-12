<script lang="ts">
  export let apiBase: string;

  let alias = "";
  let path = "/";
  let method = "GET";
  let host = "";
  let busy = false;
  let result: boolean | null = null;
  let error = "";
  const methodOptions = ["GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"];

  async function check() {
    if (!alias.trim()) {
      error = "ALIAS IS REQUIRED";
      return;
    }

    busy = true;
    error = "";
    result = null;

    try {
      const res = await fetch(`${apiBase}/check`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ alias: alias.trim(), path, method: method.trim(), host }),
      });

      if (!res.ok) {
        const body = await res.json().catch(() => ({}));
        throw new Error(body.message || res.statusText);
      }

      const body = await res.json();
      result = Boolean(body.allowed);
    } catch (err) {
      error = err instanceof Error ? err.message : "UNKNOWN ERROR";
    } finally {
      busy = false;
    }
  }
</script>

<div class="bg-panel">
  <div class="flex items-center justify-between border-b border-line px-4 py-2">
    <span class="t-label text-fg">[ LIVE ACCESS CHECK ]</span>
    <span class="t-label">PROC / V1-CHECK</span>
  </div>

  <div class="grid gap-px bg-line p-px sm:grid-cols-2">
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">SUBJECT ALIAS *</span>
      <input bind:value={alias} class="field-t" placeholder="user@example.com" />
    </label>
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">HOST</span>
      <input bind:value={host} class="field-t" placeholder="example.com" />
    </label>
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">PATH</span>
      <input bind:value={path} class="field-t" placeholder="/path/to/check" />
    </label>
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">METHOD</span>
      <input bind:value={method} list="live-check-methods" class="field-t" placeholder="GET or custom method" />
      <datalist id="live-check-methods">
        {#each methodOptions as option}
          <option value={option}></option>
        {/each}
      </datalist>
    </label>
  </div>

  <div class="flex flex-wrap items-center gap-4 border-t border-line px-4 py-3">
    <button class="btn-t" disabled={busy} on:click={check}>RUN CHECK &gt;&gt;&gt;</button>

    {#if result === true}
      <span class="bg-fg px-3 py-1 text-xs font-bold uppercase tracking-[0.15em] text-crt">
        [ ALLOWED ]
      </span>
    {:else if result === false}
      <span class="bg-alert px-3 py-1 text-xs font-bold uppercase tracking-[0.15em] text-white">
        [ DENIED ]
      </span>
    {/if}

    {#if error}
      <span class="text-xs uppercase tracking-[0.1em] text-alert">ERR // {error}</span>
    {/if}
  </div>
</div>
