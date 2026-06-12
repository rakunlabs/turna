<script lang="ts">
  import CheckWidget from "./CheckWidget.svelte";
  import type { SettingNamespace } from "../lib/api";

  export let apiBase = "/auth/v1";
  export let busy = false;
  export let settingsRevision = 0;
  export let getSettingBool: (namespace: SettingNamespace, path: string[], fallback?: boolean) => boolean = () => false;
  export let setSettingBool: (namespace: SettingNamespace, path: string[], value: boolean) => void = () => {};
  export let getSettingList: (namespace: SettingNamespace, path: string[]) => string = () => "";
  export let setSettingList: (namespace: SettingNamespace, path: string[], value: string) => void = () => {};
  export let saveSetting: (namespace: SettingNamespace) => void | Promise<void> = () => {};

  function inputValue(event: Event) {
    return (event.currentTarget as HTMLTextAreaElement).value;
  }

  function checkedValue(event: Event) {
    return (event.currentTarget as HTMLInputElement).checked;
  }

  function checkboxClass(checked: boolean) {
    const base = "h-3.5 w-3.5 appearance-none border bg-crt";
    return `${base} border-line checked:bg-alert`;
  }

  function settingBool(_revision: number, namespace: SettingNamespace, path: string[], fallback = false) {
    return getSettingBool(namespace, path, fallback);
  }

  function settingList(_revision: number, namespace: SettingNamespace, path: string[]) {
    return getSettingList(namespace, path);
  }

  $: noHostCheck = settingBool(settingsRevision, "check", ["no_host_check"]);
  $: defaultHosts = settingList(settingsRevision, "check", ["default_hosts"]);
</script>

<div class="grid gap-px bg-line p-px">
  <div class="bg-panel p-4">
    <p class="t-label text-fg">[ ACCESS CHECK ]</p>
    <h3 class="mt-2 font-display text-3xl uppercase leading-none tracking-tight md:text-4xl">Live Check</h3>
    <p class="mt-3 max-w-3xl text-xs leading-5 text-dim">
      Test an alias, host, path, and method against the IAM permission graph.
    </p>
  </div>
  <div class="bg-crt p-4">
    <CheckWidget {apiBase} />
  </div>
  <div class="grid gap-px bg-line p-px">
    <div class="flex items-center justify-between bg-panel px-3 py-2">
      <span class="t-label text-fg">[ CHECK SETTINGS ]</span>
      <button class="btn-t-solid" disabled={busy} on:click={() => saveSetting("check")}>SAVE CHECK</button>
    </div>
    <div class="grid gap-px bg-line md:grid-cols-2">
      <label class="grid gap-1 bg-panel p-3">
        <span class="t-label">DEFAULT HOSTS</span>
        <textarea class="field-t min-h-24" value={defaultHosts} placeholder="api.example.com" on:input={(event) => setSettingList("check", ["default_hosts"], inputValue(event))}></textarea>
      </label>
      <label class="flex items-center gap-3 bg-panel p-3 text-xs font-bold uppercase tracking-[0.15em]">
        <input type="checkbox" checked={noHostCheck} class={checkboxClass(noHostCheck)} on:change={(event) => setSettingBool("check", ["no_host_check"], checkedValue(event))} />
        <span class={noHostCheck ? "text-alert" : "text-dim"}>DISABLE HOST CHECK</span>
      </label>
    </div>
  </div>
</div>
