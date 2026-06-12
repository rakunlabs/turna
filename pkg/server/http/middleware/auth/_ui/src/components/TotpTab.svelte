<script lang="ts">
  import type { SettingNamespace } from "../lib/api";

  export let busy = false;
  export let settingsRevision = 0;
  export let getSettingBool: (namespace: SettingNamespace, path: string[], fallback?: boolean) => boolean = () => false;
  export let setSettingBool: (namespace: SettingNamespace, path: string[], value: boolean) => void = () => {};
  export let getSettingString: (namespace: SettingNamespace, path: string[]) => string = () => "";
  export let setSettingString: (namespace: SettingNamespace, path: string[], value: string) => void = () => {};
  export let getSettingNumber: (namespace: SettingNamespace, path: string[], fallback?: number) => number = () => 0;
  export let setSettingNumber: (namespace: SettingNamespace, path: string[], value: string) => void = () => {};
  export let saveSetting: (namespace: SettingNamespace) => void | Promise<void> = () => {};

  const ns: SettingNamespace = "totp";

  function inputValue(event: Event) {
    return (event.currentTarget as HTMLInputElement).value;
  }

  function checkedValue(event: Event) {
    return (event.currentTarget as HTMLInputElement).checked;
  }

  function sBool(_rev: number, path: string[], fallback = false) {
    return getSettingBool(ns, path, fallback);
  }

  function sString(_rev: number, path: string[]) {
    return getSettingString(ns, path);
  }

  function sNumber(_rev: number, path: string[], fallback = 0) {
    return getSettingNumber(ns, path, fallback);
  }

  $: disabled = sBool(settingsRevision, ["disabled"]);
  $: issuer = sString(settingsRevision, ["issuer"]);
  $: skew = sNumber(settingsRevision, ["skew"], 1);
</script>

<div class="grid gap-px bg-line p-px">
  <div class="grid gap-3 bg-panel p-4">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div>
        <span class="t-label text-fg">[ TOTP ]</span>
        <h3 class="mt-2 font-display text-3xl uppercase leading-none tracking-tight md:text-4xl">Authenticator 2FA</h3>
      </div>
      <button class="btn-t-solid" disabled={busy} on:click={() => saveSetting(ns)}>[ SAVE TOTP ]</button>
    </div>
    <p class="max-w-3xl text-xs leading-5 text-dim">
      Time-based one-time passwords (RFC 6238) for password logins. The issuer labels the entry in authenticator apps; skew tolerates clock drift in 30-second periods.
    </p>
  </div>

  <div class="grid gap-px bg-line md:grid-cols-2">
    <label class="flex items-center gap-3 bg-panel p-3 text-xs font-bold uppercase tracking-[0.15em] md:col-span-2">
      <input type="checkbox" checked={disabled} class="h-3.5 w-3.5 appearance-none border border-line bg-crt checked:bg-alert" on:change={(event) => setSettingBool(ns, ["disabled"], checkedValue(event))} />
      <span class={disabled ? "text-alert" : "text-dim"}>DISABLE TOTP</span>
    </label>
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">ISSUER</span>
      <input class="field-t" value={issuer} placeholder="Turna Auth" on:input={(event) => setSettingString(ns, ["issuer"], inputValue(event))} />
    </label>
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">SKEW PERIODS</span>
      <input class="field-t" type="number" min="0" value={skew} on:input={(event) => setSettingNumber(ns, ["skew"], inputValue(event))} />
    </label>
  </div>
</div>
