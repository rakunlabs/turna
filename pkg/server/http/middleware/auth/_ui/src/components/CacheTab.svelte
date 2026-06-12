<script lang="ts">
  import type { SettingNamespace } from "../lib/api";

  export let busy = false;
  export let settingsRevision = 0;
  export let getSettingBool: (namespace: SettingNamespace, path: string[], fallback?: boolean) => boolean = () => false;
  export let setSettingBool: (namespace: SettingNamespace, path: string[], value: boolean) => void = () => {};
  export let getSettingString: (namespace: SettingNamespace, path: string[]) => string = () => "";
  export let setSettingString: (namespace: SettingNamespace, path: string[], value: string) => void = () => {};
  export let getSettingList: (namespace: SettingNamespace, path: string[]) => string = () => "";
  export let setSettingList: (namespace: SettingNamespace, path: string[], value: string) => void = () => {};
  export let saveSetting: (namespace: SettingNamespace) => void | Promise<void> = () => {};

  const ns: SettingNamespace = "cache";

  function inputValue(event: Event) {
    return (event.currentTarget as HTMLInputElement | HTMLSelectElement).value;
  }

  function checkedValue(event: Event) {
    return (event.currentTarget as HTMLInputElement).checked;
  }

  function setCodeStoreActive(event: Event) {
    setSettingString(ns, ["code_store", "active"], inputValue(event));
  }

  function setRedisTLS(event: Event) {
    setSettingBool(ns, ["code_store", "redis", "tls", "enabled"], checkedValue(event));
  }

  function sString(_rev: number, path: string[]) {
    return getSettingString(ns, path);
  }

  function sBool(_rev: number, path: string[], fallback = false) {
    return getSettingBool(ns, path, fallback);
  }

  $: pollInterval = sString(settingsRevision, ["poll_interval"]);
  $: codeStoreActive = sString(settingsRevision, ["code_store", "active"]) || "memory";
  $: redisTLS = sBool(settingsRevision, ["code_store", "redis", "tls", "enabled"]);
</script>

<div class="grid gap-px bg-line p-px">
  <div class="grid gap-3 bg-panel p-4">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div>
        <span class="t-label text-fg">[ CACHE ]</span>
        <h3 class="mt-2 font-display text-3xl uppercase leading-none tracking-tight md:text-4xl">Shared State</h3>
      </div>
      <button class="btn-t-solid" disabled={busy} on:click={() => saveSetting(ns)}>[ SAVE CACHE ]</button>
    </div>
    <p class="max-w-3xl text-xs leading-5 text-dim">
      Database version polling and the temporary OAuth code/state store. Use Redis for multi-instance authorization-code, provider-state, device and email flows.
    </p>
  </div>

  <div class="grid gap-px bg-line md:grid-cols-2 xl:grid-cols-3">
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">POLL INTERVAL</span>
      <input class="field-t" value={pollInterval} placeholder="5s" on:input={(event) => setSettingString(ns, ["poll_interval"], inputValue(event))} />
      <span class="text-[10px] leading-4 text-dim">How often instances poll PostgreSQL for config version changes.</span>
    </label>
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">OAUTH CODE STORE</span>
      <select class="field-t" on:change={setCodeStoreActive}>
        <option value="memory" selected={codeStoreActive === "memory"}>memory</option>
        <option value="redis" selected={codeStoreActive === "redis"}>redis</option>
      </select>
    </label>
    <p class="bg-panel p-3 text-[11px] leading-4 text-dim">
      Memory works for a single instance; Redis is required when running more than one auth replica.
    </p>

    {#if codeStoreActive === "redis"}
      <label class="grid gap-1 bg-panel p-3 md:col-span-2 xl:col-span-3">
        <span class="t-label">REDIS ADDRESSES</span>
        <input class="field-t" value={getSettingList(ns, ["code_store", "redis", "address"])} placeholder="127.0.0.1:6379" on:input={(event) => setSettingList(ns, ["code_store", "redis", "address"], inputValue(event))} />
      </label>
      <label class="grid gap-1 bg-panel p-3">
        <span class="t-label">REDIS USERNAME</span>
        <input class="field-t" value={getSettingString(ns, ["code_store", "redis", "username"])} placeholder="optional" on:input={(event) => setSettingString(ns, ["code_store", "redis", "username"], inputValue(event))} />
      </label>
      <label class="grid gap-1 bg-panel p-3">
        <span class="t-label">REDIS PASSWORD</span>
        <input class="field-t" value={getSettingString(ns, ["code_store", "redis", "password"])} placeholder="optional" on:input={(event) => setSettingString(ns, ["code_store", "redis", "password"], inputValue(event))} />
      </label>
      <label class="grid gap-1 bg-panel p-3">
        <span class="t-label">CLIENT NAME</span>
        <input class="field-t" value={getSettingString(ns, ["code_store", "redis", "client_name"])} placeholder="turna-auth" on:input={(event) => setSettingString(ns, ["code_store", "redis", "client_name"], inputValue(event))} />
      </label>
      <label class="flex items-center gap-3 bg-panel p-3 text-xs font-bold uppercase tracking-[0.15em]">
        <input type="checkbox" checked={redisTLS} class="h-3.5 w-3.5 appearance-none border border-line bg-crt checked:bg-fg" on:change={setRedisTLS} />
        <span class={redisTLS ? "text-fg" : "text-dim"}>REDIS TLS</span>
      </label>
      {#if redisTLS}
        <label class="grid gap-1 bg-panel p-3">
          <span class="t-label">TLS CA FILE</span>
          <input class="field-t" value={getSettingString(ns, ["code_store", "redis", "tls", "ca_file"])} placeholder="optional" on:input={(event) => setSettingString(ns, ["code_store", "redis", "tls", "ca_file"], inputValue(event))} />
        </label>
        <label class="grid gap-1 bg-panel p-3">
          <span class="t-label">TLS CERT FILE</span>
          <input class="field-t" value={getSettingString(ns, ["code_store", "redis", "tls", "cert_file"])} placeholder="optional" on:input={(event) => setSettingString(ns, ["code_store", "redis", "tls", "cert_file"], inputValue(event))} />
        </label>
        <label class="grid gap-1 bg-panel p-3">
          <span class="t-label">TLS KEY FILE</span>
          <input class="field-t" value={getSettingString(ns, ["code_store", "redis", "tls", "key_file"])} placeholder="optional" on:input={(event) => setSettingString(ns, ["code_store", "redis", "tls", "key_file"], inputValue(event))} />
        </label>
      {/if}
    {/if}
  </div>
</div>
