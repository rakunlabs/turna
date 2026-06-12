<script lang="ts">
  import type { SettingNamespace } from "../lib/api";

  export let busy = false;
  export let settingsRevision = 0;
  export let getSettingBool: (namespace: SettingNamespace, path: string[], fallback?: boolean) => boolean = () => false;
  export let setSettingBool: (namespace: SettingNamespace, path: string[], value: boolean) => void = () => {};
  export let saveSetting: (namespace: SettingNamespace) => void | Promise<void> = () => {};

  const ns: SettingNamespace = "token_exchange";

  function checkedValue(event: Event) {
    return (event.currentTarget as HTMLInputElement).checked;
  }

  function sBool(_rev: number, path: string[], fallback = false) {
    return getSettingBool(ns, path, fallback);
  }

  $: disabled = sBool(settingsRevision, ["disabled"]);
</script>

<div class="grid gap-px bg-line p-px">
  <div class="grid gap-3 bg-panel p-4">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div>
        <span class="t-label text-fg">[ TOKEN EXCHANGE ]</span>
        <h3 class="mt-2 font-display text-3xl uppercase leading-none tracking-tight md:text-4xl">RFC 8693</h3>
      </div>
      <button class="btn-t-solid" disabled={busy} on:click={() => saveSetting(ns)}>[ SAVE TOKEN EXCHANGE ]</button>
    </div>
    <p class="max-w-3xl text-xs leading-5 text-dim">
      The RFC 8693 token exchange grant (<span class="text-fg">grant_type=urn:ietf:params:oauth:grant-type:token-exchange</span>) lets a client swap one token for another at <span class="text-fg">/oauth2/token</span>.
    </p>
  </div>

  <div class="grid gap-px bg-line">
    <label class="flex items-center gap-3 bg-panel p-3 text-xs font-bold uppercase tracking-[0.15em]">
      <input type="checkbox" checked={disabled} class="h-3.5 w-3.5 appearance-none border border-line bg-crt checked:bg-alert" on:change={(event) => setSettingBool(ns, ["disabled"], checkedValue(event))} />
      <span class={disabled ? "text-alert" : "text-dim"}>DISABLE TOKEN EXCHANGE</span>
    </label>
  </div>
</div>
