<script lang="ts">
  import { onDestroy, onMount, tick } from "svelte";
  import { storeInfo } from "@/store/store";
  import OverlayScroll from "./OverlayScroll.svelte";
  import {
    buildNavigation,
    recentNavigation,
    type NavigationItem,
    type NavigationType,
  } from "@/navigation";

  export let onclose: () => void;

  let dialog: HTMLDivElement;
  let input: HTMLInputElement;
  let query = "";
  let debouncedQuery = "";
  let selectedIndex = 0;
  let previousFocus: HTMLElement | null = null;
  let recentExpanded = false;
  let debounceTimer: ReturnType<typeof setTimeout>;

  const RECENT_LIMIT = 20;
  const RECENT_PREVIEW = 10;

  const typeColors: Record<NavigationType, string> = {
    swagger: "bg-green-500",
    grpc: "bg-blue-500",
    page: "bg-indigo-500",
    iframe: "bg-teal-500",
  };

  $: items = buildNavigation($storeInfo);
  $: {
    const nextQuery = query;
    clearTimeout(debounceTimer);
    debounceTimer = setTimeout(() => (debouncedQuery = nextQuery), 150);
  }
  $: normalizedQuery = debouncedQuery.toLocaleLowerCase().trim();
  $: waiting = query !== debouncedQuery;
  $: terms = normalizedQuery.split(/\s+/).filter(Boolean);
  // Recomputed only when the navigation tree changes, so the list cannot
  // reshuffle under the cursor while the palette is open.
  $: recent = recentNavigation(items, RECENT_LIMIT);
  $: if (normalizedQuery) recentExpanded = false;
  $: canExpandRecent = !normalizedQuery && !waiting && recent.length > RECENT_PREVIEW;
  $: results = waiting
    ? []
    : normalizedQuery
      ? items
          .filter((item) => {
            const content = `${item.name} ${item.type} ${item.breadcrumb} ${item.path}`.toLocaleLowerCase();
            return terms.every((term) => content.includes(term));
          })
          .sort((a, b) => {
            const aStarts = a.name.toLocaleLowerCase().startsWith(normalizedQuery);
            const bStarts = b.name.toLocaleLowerCase().startsWith(normalizedQuery);
            return Number(bStarts) - Number(aStarts) || a.name.localeCompare(b.name);
          })
      : recentExpanded
        ? recent
        : recent.slice(0, RECENT_PREVIEW);
  $: if (selectedIndex >= results.length) selectedIndex = Math.max(0, results.length - 1);

  // Only keyboard navigation scrolls: hovering a partially visible option must
  // never move the list under the pointer.
  const revealSelected = async () => {
    await tick();

    document
      .getElementById(`view-search-result-${selectedIndex}`)
      ?.scrollIntoView({ block: "nearest" });
  };

  const select = (item: NavigationItem) => {
    window.location.hash = item.href.slice(1);
    onclose();
  };

  const onKeydown = (event: KeyboardEvent) => {
    if (event.key === "Escape") {
      event.preventDefault();
      onclose();
      return;
    }

    if (event.key === "Tab") {
      const focusable = [input, ...dialog.querySelectorAll<HTMLButtonElement>("button")];
      const first = focusable[0];
      const last = focusable.at(-1);

      if (focusable.length === 1 || (event.shiftKey && document.activeElement === first)) {
        event.preventDefault();
        last?.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
      return;
    }

    if (event.target !== input) return;

    if (event.key === "ArrowDown") {
      event.preventDefault();
      selectedIndex = results.length ? (selectedIndex + 1) % results.length : 0;
      void revealSelected();
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      selectedIndex = results.length
        ? (selectedIndex - 1 + results.length) % results.length
        : 0;
      void revealSelected();
    } else if (event.key === "Enter" && results[selectedIndex]) {
      event.preventDefault();
      select(results[selectedIndex]);
    }
  };

  onMount(async () => {
    previousFocus = document.activeElement as HTMLElement | null;
    await tick();
    input.focus();
  });

  onDestroy(() => {
    clearTimeout(debounceTimer);
    previousFocus?.focus();
  });
</script>

<div class="fixed inset-0 z-50 flex items-start justify-center px-3 pt-[10vh] sm:px-6 sm:pt-[14vh]">
  <button
    type="button"
    class="absolute inset-0 cursor-default bg-black/45"
    aria-label="Close search"
    onclick={onclose}
  ></button>

  <div
    bind:this={dialog}
    class="relative flex max-h-[72vh] w-full max-w-2xl flex-col overflow-hidden rounded-md border border-black bg-white shadow-2xl"
    role="dialog"
    tabindex="-1"
    aria-modal="true"
    aria-label="Search View"
    onkeydown={onKeydown}
  >
    <div class="flex items-center border-b border-black bg-yellow-100">
      <i class="lni lni-search-1 ml-3 text-[19px]!" aria-hidden="true"></i>
      <input
        bind:this={input}
        bind:value={query}
        oninput={() => (selectedIndex = 0)}
        class="min-w-0 flex-1 bg-transparent px-3 py-3 pr-4 text-base font-medium outline-none placeholder:text-gray-600"
        type="search"
        placeholder="Search pages and APIs..."
        aria-label="Search pages and APIs"
        aria-controls="view-search-results"
        aria-activedescendant={results[selectedIndex]
          ? `view-search-result-${selectedIndex}`
          : undefined}
      />
    </div>

    {#if results.length > 0 || (normalizedQuery && !waiting)}
      <OverlayScroll class="min-h-0">
        <div id="view-search-results" role="listbox">
          {#if results.length > 0}
            {#if !normalizedQuery}
              <div class="border-b border-black bg-gray-50 px-3 py-1.5 text-xs font-semibold uppercase tracking-wide text-gray-600">
                Recently visited
              </div>
            {/if}
            {#each results as item, index}
              <button
                id={`view-search-result-${index}`}
                type="button"
                role="option"
                aria-selected={index === selectedIndex}
                class={`flex w-full items-center gap-3 border-b border-black px-3 py-2 text-left last:border-b-0 ${index === selectedIndex ? "bg-black text-white" : "bg-white text-black hover:bg-gray-100"}`}
                onmouseenter={() => (selectedIndex = index)}
                onclick={() => select(item)}
              >
                <span
                  class={`flex h-7 w-7 shrink-0 items-center justify-center font-mono text-sm font-bold text-white ${typeColors[item.type]}`}
                  aria-hidden="true"
                >
                  {item.type[0].toUpperCase()}
                </span>
                <span class="min-w-0 flex-1">
                  <span class="block truncate font-medium">{item.name}</span>
                  <span class={`block truncate text-xs ${index === selectedIndex ? "text-gray-300" : "text-gray-600"}`}>
                    {item.breadcrumb}
                  </span>
                </span>
                <span class={`shrink-0 text-xs uppercase ${index === selectedIndex ? "text-gray-300" : "text-gray-500"}`}>
                  {item.type}
                </span>
              </button>
            {/each}
          {:else}
            <div class="px-4 py-10 text-center">
              <p class="font-medium">No matches found</p>
              <p class="mt-1 text-sm text-gray-600">Try a page, service, group, or API name.</p>
            </div>
          {/if}
        </div>
      </OverlayScroll>
    {/if}

    {#if results.length > 0}
      <div class="flex items-center gap-3 border-t border-black bg-gray-50 px-3 py-1.5 text-xs text-gray-600">
        <span class="flex items-center gap-0.5">
          <i class="lni lni-arrow-upward text-[13px]" aria-hidden="true"></i>
          <i class="lni lni-arrow-downward text-[13px]" aria-hidden="true"></i>
          navigate
        </span>
        <span><kbd class="font-sans">Enter</kbd> open</span>
        {#if canExpandRecent}
          <button
            type="button"
            class="ml-auto text-gray-600 underline-offset-2 hover:text-black focus-visible:text-black focus-visible:underline focus-visible:outline-none"
            onclick={() => (recentExpanded = !recentExpanded)}
          >
            {recentExpanded ? "Show less" : `Show more (${recent.length - RECENT_PREVIEW})`}
          </button>
        {/if}
      </div>
    {/if}
  </div>
</div>

