<script lang="ts">
  import { session } from "../lib/state/session.svelte";
  import { theme, type ThemeMode } from "../lib/state/theme.svelte";

  let {
    onrefresh,
    onmenu,
    menuOpen = false,
  }: { onrefresh: () => void; onmenu: () => void; menuOpen?: boolean } = $props();

  const modes: { id: ThemeMode; label: string }[] = [
    { id: "system", label: "Auto" },
    { id: "light", label: "Light" },
    { id: "dark", label: "Dark" },
  ];
</script>

<header class="z-30 flex min-h-[56px] items-stretch gap-2 border-b border-rule bg-sheet px-2.5 sm:gap-4 sm:px-5">
  <button
    type="button"
    class="act act-quiet -ml-1 shrink-0 self-center lg:hidden"
    aria-expanded={menuOpen}
    aria-label={menuOpen ? "Close index" : "Open index"}
    onclick={onmenu}
  >
    <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true" fill="none">
      {#if menuOpen}
        <path d="M2 2L14 14M14 2L2 14" stroke="currentColor" stroke-width="1.5" />
      {:else}
        <path d="M1.5 3.5h13M1.5 8h13M1.5 12.5h13" stroke="currentColor" stroke-width="1.5" />
      {/if}
    </svg>
  </button>

  <div class="flex shrink-0 items-center self-center">
    <span class="text-[15px] font-extrabold tracking-[-0.02em] text-ink">Turna Auth</span>
  </div>

  <!-- Where this instance lives, stated continuously rather than on request. -->
  <div class="hidden min-w-0 items-center gap-5 self-center border-l border-rule pl-5 md:flex">
    <span class="stamp-raw truncate serial">{session.info?.prefix_path ?? "/auth"}</span>
    <span class="stamp-raw truncate">{session.info?.storage ?? "postgres"}</span>
  </div>

  <div class="ml-auto flex shrink-0 items-center gap-2 self-center sm:gap-5">
    <!-- Both themes ship, so the control is reachable at every width. -->
    <div class="flex items-stretch border border-rule" role="group" aria-label="Theme">
      {#each modes as mode, index (mode.id)}
        <button
          type="button"
          class="stamp px-1 py-1.5 transition-colors sm:px-2.5
            {index > 0 ? 'border-l border-rule' : ''}
            {theme.mode === mode.id ? 'bg-ink text-sheet' : 'text-muted hover:text-ink'}"
          aria-pressed={theme.mode === mode.id}
          onclick={() => theme.set(mode.id)}
        >
          {mode.label}
        </button>
      {/each}
    </div>

    <button
      type="button"
      class="act shrink-0"
      disabled={session.busy}
      onclick={onrefresh}
      aria-label="Re-read from the database"
    >
      <svg width="14" height="14" viewBox="0 0 14 14" aria-hidden="true" fill="none" class="sm:hidden">
        <path
          d="M12 7a5 5 0 1 1-1.6-3.7M12 1.5V4h-2.5"
          stroke="currentColor"
          stroke-width="1.4"
          stroke-linecap="round"
          stroke-linejoin="round"
        />
      </svg>
      <span class="hidden sm:inline">{session.busy ? "Reading…" : "Re-read"}</span>
    </button>
  </div>
</header>
