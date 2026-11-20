<script lang="ts">
  import { untrack } from "svelte";
  import { Play, LoaderCircle, History, X, ChevronDown, Settings2 } from "@lucide/svelte";
  import type { Deref, Scope } from "../lib/api";
  import { app, defaultQuery } from "../lib/state.svelte";
  import { timeAgo, prettyFilter, compactFilter } from "../lib/format";
  import { presets, expand, type Preset } from "../lib/presets";

  let { busy, onrun }: { busy: boolean; onrun: () => void } = $props();

  let showHistory = $state(false);
  let showMore = $state(false);

  // the attributes field is a comma or space separated list
  let attrText = $state(app.query.attributes.join(", "));
  $effect(() => {
    const next = app.query.attributes.join(", ");
    untrack(() => {
      if (split(attrText).join(", ") !== next) attrText = next;
    });
  });

  function split(v: string) {
    return v.split(/[\s,]+/).filter(Boolean);
  }

  const list = $derived(presets[app.flavor]);

  function usePreset(p: Preset) {
    app.query.filter = expand(p.filter);
    if (p.attributes) app.query.attributes = [...p.attributes];
  }

  function togglePretty() {
    const f = app.query.filter;
    app.query.filter = f.includes("\n") ? compactFilter(f) : prettyFilter(f);
  }

  const scopes: { v: Scope; label: string }[] = [
    { v: "base", label: "Base" },
    { v: "one", label: "One level" },
    { v: "sub", label: "Subtree" },
    { v: "children", label: "Children" },
  ];

  // a quick balance check catches the most common typo before a round trip
  const filterProblem = $derived.by(() => {
    const f = app.query.filter.trim();
    if (!f) return null;
    let depth = 0;
    for (let i = 0; i < f.length; i++) {
      if (f[i] === "\\") i++;
      else if (f[i] === "(") depth++;
      else if (f[i] === ")" && --depth < 0) return "Unbalanced parentheses";
    }
    return depth !== 0 ? "Unbalanced parentheses" : null;
  });

  function submit(e: SubmitEvent) {
    e.preventDefault();
    app.query.attributes = split(attrText);
    onrun();
  }

  function key(e: KeyboardEvent) {
    if ((e.metaKey || e.ctrlKey) && e.shiftKey && e.key.toLowerCase() === "f") {
      e.preventDefault();
      togglePretty();
      return;
    }
    if ((e.metaKey || e.ctrlKey) && e.key === "Enter") {
      e.preventDefault();
      app.query.attributes = split(attrText);
      onrun();
    }
  }
</script>

<svelte:window onkeydown={(e) => e.key === "Escape" && showHistory && (showHistory = false)} />

<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<form class="border-b border-line bg-surface p-3" onsubmit={submit} onkeydown={key}>
  <div class="grid grid-cols-1 gap-2 md:grid-cols-[minmax(0,1fr)_150px]">
    <label class="block min-w-0">
      <span class="mb-1 block text-[11.5px] font-medium text-muted">Base DN</span>
      <input class="input font-mono" bind:value={app.query.base_dn} placeholder="dc=example,dc=org (empty for root)" spellcheck="false" />
    </label>
    <label class="block">
      <span class="mb-1 block text-[11.5px] font-medium text-muted">Scope</span>
      <div class="relative">
        <select class="input appearance-none pr-7" bind:value={app.query.scope}>
          {#each scopes as s (s.v)}<option value={s.v}>{s.label}</option>{/each}
        </select>
        <ChevronDown class="pointer-events-none absolute top-2 right-2 size-4 text-faint" />
      </div>
    </label>
  </div>

  <label class="mt-2 block">
    <span class="mb-1 flex items-center gap-2 text-[11.5px] font-medium text-muted">
      Filter
      {#if filterProblem}<span class="font-normal text-danger">{filterProblem}</span>{/if}
      <button type="button" class="ml-auto font-normal text-accent-text hover:underline" onclick={togglePretty} title="Format or compact the filter (Ctrl+Shift+F)">{app.query.filter.includes("\n") ? "Compact" : "Format"}</button>
    </span>
    <textarea
      class="input h-auto min-h-8 resize-y py-1.5 font-mono leading-5 {filterProblem ? 'border-danger' : ''}"
      rows={Math.min(8, app.query.filter.split("\n").length)}
      bind:value={app.query.filter}
      placeholder="(objectClass=*)"
      spellcheck="false"
    ></textarea>
  </label>
  <div class="mt-1.5 flex flex-wrap gap-1">
    {#each list as p (p.label)}
      <button type="button" class="rounded-full border border-line px-2 py-px text-[11.5px] text-muted hover:border-line-strong hover:text-ink" title={p.hint ?? p.filter} onclick={() => usePreset(p)}>
        {p.label}
      </button>
    {/each}
  </div>

  <label class="mt-2 block">
    <span class="mb-1 block text-[11.5px] font-medium text-muted">Attributes <span class="font-normal text-faint">— empty for all user attributes, <code class="kbd">+</code> for operational</span></span>
    <input class="input font-mono" bind:value={attrText} placeholder="cn, mail, memberOf" spellcheck="false" />
  </label>

  {#if showMore}
    <div class="mt-2 grid grid-cols-2 gap-2 md:grid-cols-5">
      <label class="block">
        <span class="mb-1 block text-[11.5px] font-medium text-muted">Size limit</span>
        <input class="input tnum" type="number" min="0" max={app.info?.max_size_limit} bind:value={app.query.size_limit} />
      </label>
      <label class="block">
        <span class="mb-1 block text-[11.5px] font-medium text-muted">Time limit (s)</span>
        <input class="input tnum" type="number" min="0" max={app.info?.max_time_limit} bind:value={app.query.time_limit} placeholder="max" />
      </label>
      <label class="block">
        <span class="mb-1 block text-[11.5px] font-medium text-muted">Page size</span>
        <input class="input tnum" type="number" min="0" bind:value={app.query.page_size} placeholder="0 = off" />
      </label>
      <label class="block">
        <span class="mb-1 block text-[11.5px] font-medium text-muted">Aliases</span>
        <select class="input" bind:value={app.query.deref}>
          {#each ["never", "search", "find", "always"] as d (d)}<option value={d as Deref}>{d}</option>{/each}
        </select>
      </label>
      <label class="flex items-end gap-2 pb-1.5">
        <input type="checkbox" bind:checked={app.query.types_only} /> Names only
      </label>
    </div>
    <p class="mt-1 text-[11.5px] text-faint">
      Server caps: {app.info?.max_size_limit} entries, {app.info?.max_time_limit}s. Use a page size for servers such as Active Directory that cap a single response.
    </p>
  {/if}

  <div class="mt-3 flex items-center gap-2">
    <button class="btn btn-primary" disabled={busy || !!filterProblem}>
      {#if busy}<LoaderCircle class="size-3.5 animate-spin" />{:else}<Play class="size-3.5" />{/if}
      Search
    </button>
    <span class="hidden text-[11.5px] text-faint sm:inline"><span class="kbd">⌘/Ctrl</span> <span class="kbd">Enter</span></span>
    <div class="ml-auto flex items-center gap-1">
      <button type="button" class="btn btn-ghost {showMore ? 'text-ink' : ''}" onclick={() => (showMore = !showMore)} aria-pressed={showMore}>
        <Settings2 class="size-3.5" /> Limits
      </button>
      <div class="relative">
        <button type="button" class="btn btn-ghost" onclick={() => (showHistory = !showHistory)} aria-expanded={showHistory} disabled={app.history.length === 0}>
          <History class="size-3.5" /> History
        </button>
        {#if showHistory}
          <div class="absolute right-0 z-30 mt-1 w-[min(520px,90vw)] rounded-lg border border-line bg-surface p-1 shadow-[var(--shadow)]">
            <div class="flex items-center justify-between px-2 py-1 text-[11.5px] text-faint">
              Recent searches
              <button type="button" class="hover:text-danger" onclick={() => { app.clearHistory(); showHistory = false; }}>Clear</button>
            </div>
            <div class="max-h-80 overflow-auto">
              {#each app.history as h (h.at)}
                <button
                  type="button"
                  class="block w-full rounded-md px-2 py-1.5 text-left hover:bg-hover"
                  onclick={() => {
                    const { at: _, ...q } = h;
                    app.query = { ...defaultQuery(), ...q };
                    showHistory = false;
                  }}
                >
                  <div class="truncate font-mono text-[12px]">{h.filter}</div>
                  <div class="flex gap-2 truncate text-[11.5px] text-faint">
                    <span class="truncate">{h.base_dn || "(root)"}</span>
                    <span>· {h.scope}</span>
                    <span class="ml-auto shrink-0">{timeAgo(h.at)}</span>
                  </div>
                </button>
              {/each}
            </div>
          </div>
          <button type="button" class="fixed inset-0 z-20 cursor-default" aria-label="Close history" onclick={() => (showHistory = false)}></button>
        {/if}
      </div>
      <button type="button" class="icon-btn" title="Reset form" aria-label="Reset form" onclick={() => (app.query = defaultQuery(app.query.base_dn))}>
        <X class="size-3.5" />
      </button>
    </div>
  </div>
</form>
