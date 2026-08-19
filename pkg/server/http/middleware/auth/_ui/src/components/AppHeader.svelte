<script lang="ts">
  import type { InfoPayload } from "../lib/api";

  type ThemeMode = "system" | "dark" | "light";

  export let info: InfoPayload | null = null;
  export let busy = false;
  export let themeMode: ThemeMode = "system";
  export let onRefresh: () => void = () => {};
  export let onThemeMode: (mode: ThemeMode) => void = () => {};

  $: versionLabel = info?.version !== undefined ? `v${info.version}` : "offline";

  const themeModes: { id: ThemeMode; label: string }[] = [
    { id: "system", label: "Auto" },
    { id: "dark", label: "Dark" },
    { id: "light", label: "Light" },
  ];
</script>

<header class="z-40 flex min-h-[52px] items-center gap-3 border-b border-line bg-surface px-4">
  <div class="flex shrink-0 items-center gap-2 py-3">
    <span class="font-display text-lg leading-none tracking-tight">Turna Auth</span>
  </div>

  <div class="hidden min-w-0 flex-1 items-center md:flex">
    <span class="t-label">Identity &amp; access control plane</span>
  </div>

  <div class="ml-auto flex shrink-0 items-center gap-2" title="database link status">
    <span class={`h-2 w-2 rounded-full ${info ?"bg-phosphor" :"bg-alert"}`}></span>
    <span class="t-label">{versionLabel}</span>
  </div>

  <div class="flex shrink-0 items-center gap-2" title="theme mode">
    <div class="flex rounded-lg border border-line bg-crt p-0.5">
      {#each themeModes as mode}
        <button
          class={`rounded-md px-2 py-1 text-xs font-medium sm:px-2.5 ${
            themeMode === mode.id ? "bg-surface text-fg shadow-sm" : "text-dim hover:text-fg"
          }`}
          aria-pressed={themeMode === mode.id}
          on:click={() => onThemeMode(mode.id)}
        >
          {mode.label}
        </button>
      {/each}
    </div>
  </div>

  <button class="btn-t hidden md:inline-flex" disabled={busy} on:click={onRefresh}>
    {busy ? "Refreshing..." : "Refresh"}
  </button>
</header>
