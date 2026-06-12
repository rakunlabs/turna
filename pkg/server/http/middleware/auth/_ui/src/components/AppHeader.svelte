<script lang="ts">
  import type { InfoPayload } from "../lib/api";

  type ThemeMode = "system" | "dark" | "light";

  export let info: InfoPayload | null = null;
  export let busy = false;
  export let themeMode: ThemeMode = "system";
  export let onRefresh: () => void = () => {};
  export let onThemeMode: (mode: ThemeMode) => void = () => {};

  $: versionLabel = info?.version !== undefined ? `SYS.VER ${String(info.version).padStart(4, "0")}` : "NO LINK";

  const themeModes: { id: ThemeMode; label: string }[] = [
    { id: "system", label: "SYS" },
    { id: "dark", label: "DARK" },
    { id: "light", label: "LIGHT" },
  ];
</script>

<header class="z-40 flex min-h-[49px] items-stretch border-b border-line bg-crt">
  <div class="flex shrink-0 items-center gap-1 border-r border-line px-4 py-3">
    <span class="font-display text-lg uppercase leading-none tracking-tight">TURNA</span>
    <span class="font-display text-lg leading-none text-alert">//</span>
    <span class="font-display text-lg uppercase leading-none tracking-tight">AUTH</span>
    <span class="ml-1 self-start text-[9px] text-dim">&reg;</span>
  </div>

  <div class="hidden min-w-0 flex-1 items-center px-4 md:flex">
    <span class="t-label">IDENTITY / ACCESS / OAUTH2 - CONTROL PLANE</span>
  </div>

  <div class="ml-auto flex shrink-0 items-center gap-2 border-l border-line px-3 sm:px-4" title="database link status">
    <span class={`h-2 w-2 ${info ? "bg-phosphor" : "bg-alert"}`}></span>
    <span class="t-label text-fg">{versionLabel}</span>
  </div>

  <div class="flex shrink-0 items-center gap-2 border-l border-line px-2 sm:px-3" title="theme mode">
    <span class="t-label hidden xl:inline">THEME</span>
    <div class="grid grid-cols-3 border border-line bg-crt">
      {#each themeModes as mode}
        <button
          class={`px-2 py-1 text-[9px] font-bold uppercase tracking-[0.12em] sm:px-2.5 ${
            themeMode === mode.id ? "bg-alert text-white" : "text-dim hover:bg-panel hover:text-fg"
          }`}
          aria-pressed={themeMode === mode.id}
          on:click={() => onThemeMode(mode.id)}
        >
          {mode.label}
        </button>
      {/each}
    </div>
  </div>

  <button class="hidden border-l border-line px-4 text-[10px] font-bold uppercase tracking-[0.2em] hover:bg-fg hover:text-crt disabled:opacity-40 md:block" disabled={busy} on:click={onRefresh}>
    {busy ? "[ POLLING... ]" : "[ REFRESH ]"}
  </button>
</header>
