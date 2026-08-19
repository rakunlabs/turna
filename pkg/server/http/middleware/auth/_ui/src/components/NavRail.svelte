<script lang="ts">
  import type { InfoPayload } from "../lib/api";
  import type { NavGroup, NavItem, Tab } from "../lib/navigation";

  export let activeTab: Tab;
  export let navGroups: NavGroup[] = [];
  export let info: InfoPayload | null = null;
  export let busy = false;
  export let onSelect: (tab: Tab) => void = () => {};
  export let onRefresh: () => void = () => {};
</script>

<aside class="min-h-0 flex flex-row overflow-x-auto border-b border-line bg-surface lg:h-full lg:flex-col lg:overflow-x-hidden lg:overflow-y-auto lg:border-b-0 lg:border-r">
  <nav class="flex flex-1 flex-row gap-1 p-2 lg:flex-col">
    {#each navGroups as group}
      <div class="flex shrink-0 flex-row gap-1 lg:flex-col">
        <div class="hidden px-3 pb-1 pt-3 lg:block">
          <span class="t-label">{group.label}</span>
        </div>
        {#each group.items as item}
          <button
            class={`flex shrink-0 items-center gap-3 rounded-lg px-3 py-2 text-left text-[13px] font-medium ${
              activeTab === item.id ? "bg-primary/15 text-primary" : "text-dim hover:bg-panel-hover hover:text-fg"
            }`}
            on:click={() => onSelect(item.id)}
          >
            {item.label}
          </button>
        {/each}
      </div>
    {/each}
  </nav>

  <div class="hidden border-t border-line p-4 lg:block">
    <p class="t-label">PostgreSQL / {info?.prefix_path ?? "/auth"}</p>
    <button class="btn-t mt-4 w-full lg:hidden" disabled={busy} on:click={onRefresh}>Refresh</button>
  </div>
</aside>
