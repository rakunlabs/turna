<script lang="ts">
  import type { SettingNamespace } from "../lib/api";

  export let busy = false;
  export let settingsRevision = 0;
  export let getSettingBool: (namespace: SettingNamespace, path: string[], fallback?: boolean) => boolean = () => false;
  export let setSettingBool: (namespace: SettingNamespace, path: string[], value: boolean) => void = () => {};
  export let getSettingString: (namespace: SettingNamespace, path: string[]) => string = () => "";
  export let setSettingString: (namespace: SettingNamespace, path: string[], value: string) => void = () => {};
  export let saveSetting: (namespace: SettingNamespace) => void | Promise<void> = () => {};

  const ns: SettingNamespace = "admin";

  function inputValue(event: Event) {
    return (event.currentTarget as HTMLInputElement).value;
  }

  function checkedValue(event: Event) {
    return (event.currentTarget as HTMLInputElement).checked;
  }

  function checkboxClass(danger = false) {
    const base = "h-3.5 w-3.5 appearance-none border border-line bg-crt";
    return `${base} ${danger ? "checked:bg-alert" : "checked:bg-fg"}`;
  }

  function sString(_rev: number, path: string[]) {
    return getSettingString(ns, path);
  }

  function sBool(_rev: number, path: string[], fallback = false) {
    return getSettingBool(ns, path, fallback);
  }

  $: permission = sString(settingsRevision, ["permission"]);
  $: allowMissing = sBool(settingsRevision, ["allow_missing_x_user"], true);
</script>

<div class="grid gap-px bg-line p-px">
  <div class="grid gap-3 bg-panel p-4">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div>
        <span class="t-label text-fg">[ ADMIN ]</span>
        <h3 class="mt-2 font-display text-3xl uppercase leading-none tracking-tight md:text-4xl">Admin Access</h3>
      </div>
      <button class="btn-t-solid" disabled={busy} on:click={() => saveSetting(ns)}>[ SAVE ADMIN ]</button>
    </div>
    <p class="max-w-3xl text-xs leading-5 text-dim">
      Controls who may administer this auth instance. The permission is matched against the permission ID or name carried by <span class="text-fg">X-User</span>.
    </p>
  </div>

  <div class="grid gap-px bg-line md:grid-cols-2">
    <label class="grid gap-1 bg-panel p-3 md:col-span-2">
      <span class="t-label">ADMIN PERMISSION</span>
      <input class="field-t" value={permission} placeholder="turna.auth.admin; empty = bootstrap open" on:input={(event) => setSettingString(ns, ["permission"], inputValue(event))} />
      <span class="text-[10px] leading-4 text-dim">Matched against permission ID or name on X-User. Empty keeps bootstrap compatibility.</span>
    </label>
    <label class="flex items-center gap-3 bg-panel p-3 text-xs font-bold uppercase tracking-[0.15em]">
      <input type="checkbox" checked={allowMissing} class={checkboxClass(true)} on:change={(event) => setSettingBool(ns, ["allow_missing_x_user"], checkedValue(event))} />
      <span class={allowMissing ? "text-alert" : "text-dim"}>ALLOW MISSING X-USER BREAK-GLASS ADMIN</span>
    </label>
    <p class="bg-panel p-3 text-[11px] leading-4 text-dim md:col-span-2">
      Use break-glass only when the auth route is not publicly exposed. If enabled, removing the session chain lets direct requests administer auth.
    </p>
  </div>
</div>
