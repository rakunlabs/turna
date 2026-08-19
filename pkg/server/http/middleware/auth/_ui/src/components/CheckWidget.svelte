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
      error = "Alias is required";
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
      error = err instanceof Error ? err.message : "Unknown error";
    } finally {
      busy = false;
    }
  }
</script>

<div class="bg-panel">
  <div class="flex items-center justify-between border-b border-line px-4 py-2">
    <span class="t-label text-fg">Live access check</span>
    <span class="t-label">Proc / v1-check</span>
  </div>

  <div class="grid gap-px bg-line p-px sm:grid-cols-2">
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">Subject alias *</span>
      <input bind:value={alias} class="field-t" placeholder="user@example.com" />
    </label>
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">Host</span>
      <input bind:value={host} class="field-t" placeholder="example.com" />
    </label>
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">Path</span>
      <input bind:value={path} class="field-t" placeholder="/path/to/check" />
    </label>
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">Method</span>
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
      <span class="bg-fg px-3 py-1 text-xs font-bold text-crt">
        Allowed
      </span>
    {:else if result === false}
      <span class="bg-alert px-3 py-1 text-xs font-bold text-white">
        Denied
      </span>
    {/if}

    {#if error}
      <span class="text-xs text-alert">ERR // {error}</span>
    {/if}
  </div>
</div>
