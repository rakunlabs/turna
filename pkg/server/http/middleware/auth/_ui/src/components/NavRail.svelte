<script lang="ts">
  import type { InfoPayload } from "../lib/api";
  import type { NavGroup, NavItem, Tab } from "../lib/navigation";

  export let activeTab: Tab;
  export let navGroups: NavGroup[] = [];
  export let nav: NavItem[] = [];
  export let info: InfoPayload | null = null;
  export let busy = false;
  export let onSelect: (tab: Tab) => void = () => {};
  export let onRefresh: () => void = () => {};
</script>

<aside class="min-h-0 flex flex-row overflow-x-auto border-b border-line bg-crt lg:h-full lg:flex-col lg:overflow-x-hidden lg:overflow-y-auto lg:border-b-0 lg:border-r">
  <nav class="flex flex-1 flex-row lg:flex-col">
    {#each navGroups as group}
      <div class="flex shrink-0 flex-row lg:flex-col">
        <div class="hidden border-b border-line bg-panel px-4 py-2 lg:block">
          <span class="t-label text-fg">{group.label}</span>
        </div>
        {#each group.items as item}
          <button
            class={`flex shrink-0 items-center gap-3 border-r border-line px-4 py-3 text-left text-[11px] font-bold uppercase tracking-[0.15em] lg:border-b lg:border-r-0 ${
              activeTab === item.id ? "bg-alert text-white" : "text-dim hover:bg-panel hover:text-fg"
            }`}
            on:click={() => onSelect(item.id)}
          >
            <span class={activeTab === item.id ? "text-white" : "text-dim"}>{String(nav.findIndex((navItem) => navItem.id === item.id)).padStart(2, "0")}</span>
            {item.label}
          </button>
        {/each}
      </div>
    {/each}
  </nav>

  <div class="hidden border-t border-line bg-crt p-4 lg:block">
    <p class="t-label">SRC &gt;&gt; POSTGRESQL</p>
    <p class="t-label mt-1">PFX &gt;&gt; {info?.prefix_path ?? "/auth"}</p>
    <p class="t-label mt-1">DOC &gt;&gt; TA-{String(info?.version ?? 0).padStart(4, "0")}</p>
    <button class="btn-t mt-4 w-full lg:hidden" disabled={busy} on:click={onRefresh}>[ REFRESH ]</button>
  </div>
</aside>
