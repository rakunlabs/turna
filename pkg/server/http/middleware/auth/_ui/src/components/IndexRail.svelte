<script lang="ts">
  import type { NavGroup, Tab } from "../lib/navigation";

  /**
   * The index of the dossier. The seven groups the operator already knows are
   * kept exactly as they are; what changes is that the index is searchable, its
   * groups fold, and the active entry is marked with the same seal used
   * everywhere else rather than a filled pill.
   */
  let {
    groups,
    active,
    onselect,
    search = $bindable(""),
  }: {
    groups: NavGroup[];
    active: Tab;
    onselect: (tab: Tab) => void;
    search?: string;
  } = $props();

  let collapsed = $state<Record<string, boolean>>({});
  let searchEl = $state<HTMLInputElement | null>(null);


  const query = $derived(search.trim().toLowerCase());

  const shown = $derived(
    groups
      .map((group) => ({
        ...group,
        items: query
          ? group.items.filter(
              (item) =>
                item.label.toLowerCase().includes(query) || item.id.toLowerCase().includes(query),
            )
          : group.items,
      }))
      .filter((group) => group.items.length > 0),
  );

  export function focusSearch() {
    searchEl?.focus();
    searchEl?.select();
  }

  function toggle(label: string) {
    collapsed = { ...collapsed, [label]: !collapsed[label] };
  }

  // A search always shows its results, whatever the operator folded away.
  function isOpen(label: string) {
    return Boolean(query) || !collapsed[label];
  }
</script>

<nav class="flex h-full min-h-0 flex-col bg-sheet lg:border-r lg:border-rule" aria-label="Console index">
  <div class="shrink-0 border-b border-rule px-3 py-3">
    <label class="stamp block" for="index-search">Find</label>
    <input
      id="index-search"
      bind:this={searchEl}
      bind:value={search}
      class="entry mt-1"
      type="search"
      placeholder="Press / to search"
      autocomplete="off"
    />
  </div>

  <div class="min-h-0 flex-1 overflow-y-auto overscroll-contain px-2 pb-6 pt-2">
    {#each shown as group (group.label)}
      <div class="mb-1">
        <button
          type="button"
          class="flex w-full items-center gap-2 px-2 pb-1 pt-3 text-left"
          aria-expanded={isOpen(group.label)}
          onclick={() => toggle(group.label)}
        >
          <span class="stamp">{group.label}</span>
          <span class="h-px flex-1 bg-rule"></span>
          <svg
            width="9"
            height="9"
            viewBox="0 0 9 9"
            aria-hidden="true"
            fill="none"
            class="shrink-0 text-muted transition-transform duration-150 {isOpen(group.label)
              ? ''
              : '-rotate-90'}"
          >
            <path d="M1 3L4.5 6.5L8 3" stroke="currentColor" stroke-width="1.3" />
          </svg>
        </button>

        {#if isOpen(group.label)}
          <ul>
            {#each group.items as item (item.id)}
              {@const current = active === item.id}
              <li>
                <button
                  type="button"
                  class="flex w-full items-center gap-2.5 px-2 py-[7px] text-left text-[13px] leading-[1.35] transition-colors
                    {current
                      ? 'bg-raised font-semibold text-ink'
                      : 'text-muted hover:bg-raised hover:text-ink'}"
                  aria-current={current ? "page" : undefined}
                  onclick={() => onselect(item.id)}
                >
                  <span
                    class="inline-block h-[6px] w-[6px] shrink-0 {current
                      ? 'bg-seal'
                      : 'bg-transparent'}"
                    aria-hidden="true"
                  ></span>
                  <span class="min-w-0 truncate">{item.label}</span>
                </button>
              </li>
            {/each}
          </ul>
        {/if}
      </div>
    {/each}

    {#if shown.length === 0}
      <p class="px-2 py-6 text-[13px] leading-[1.5] text-muted">
        Nothing in the index matches “{search}”.
      </p>
    {/if}
  </div>

</nav>
