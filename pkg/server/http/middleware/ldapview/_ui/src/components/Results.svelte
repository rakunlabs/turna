<script lang="ts">
  import { Copy, Download, TriangleAlert, Rows3, Table2, FileCode2, Braces, SearchX, ListFilter } from "@lucide/svelte";
  import type { SearchResult } from "../lib/api";
  import { app } from "../lib/state.svelte";
  import { toLDIF, toJSON, toCSV, download } from "../lib/format";
  import EntryCard from "./EntryCard.svelte";

  let { result, onopen }: { result: SearchResult; onopen: (dn: string) => void } = $props();

  type View = "entries" | "table" | "ldif" | "json";
  let view = $state<View>((localStorage.getItem("turna-lv-view") as View) || "entries");
  let filter = $state("");

  $effect(() => {
    localStorage.setItem("turna-lv-view", view);
  });

  const LIMIT = 200;
  let shown = $state(LIMIT);
  $effect(() => {
    void result;
    shown = LIMIT;
  });

  const entries = $derived.by(() => {
    const f = filter.trim().toLowerCase();
    if (!f) return result.entries;
    return result.entries.filter(
      (e) => e.dn.toLowerCase().includes(f) || e.attributes.some((a) => a.name.toLowerCase().includes(f) || a.values.some((v) => v.toLowerCase().includes(f))),
    );
  });

  const columns = $derived.by(() => {
    const seen = new Map<string, string>();
    for (const e of entries) for (const a of e.attributes) if (!seen.has(a.name.toLowerCase())) seen.set(a.name.toLowerCase(), a.name);
    return [...seen.values()].filter((c) => c.toLowerCase() !== "objectclass").slice(0, 12);
  });

  const text = $derived(view === "ldif" ? toLDIF(entries) : view === "json" ? toJSON(entries) : "");

  const views: { v: View; label: string; icon: typeof Rows3 }[] = [
    { v: "entries", label: "Entries", icon: Rows3 },
    { v: "table", label: "Table", icon: Table2 },
    { v: "ldif", label: "LDIF", icon: FileCode2 },
    { v: "json", label: "JSON", icon: Braces },
  ];

  async function copyAll() {
    await navigator.clipboard.writeText(view === "json" ? toJSON(entries) : view === "table" ? toCSV(entries) : toLDIF(entries));
    app.toast(`${entries.length} entries copied as ${view === "json" ? "JSON" : view === "table" ? "CSV" : "LDIF"}`);
  }

  function save() {
    if (view === "json") download("ldap-search.json", toJSON(entries), "application/json");
    else if (view === "table") download("ldap-search.csv", toCSV(entries), "text/csv");
    else download("ldap-search.ldif", toLDIF(entries), "text/plain");
  }

  const cell = (vals: string[] | undefined) => (vals ? vals.join("; ") : "");
</script>

<div class="flex min-h-0 flex-1 flex-col">
  <div class="flex flex-wrap items-center gap-2 border-b border-line bg-surface px-3 py-2">
    <div class="text-[12.5px]">
      <span class="font-semibold tnum">{result.entries.length}</span>
      <span class="text-muted">{result.entries.length === 1 ? "entry" : "entries"}</span>
      {#if filter && entries.length !== result.entries.length}<span class="text-muted">· {entries.length} shown</span>{/if}
      <span class="text-faint tnum">· {result.duration_ms.toFixed(1)} ms</span>
    </div>

    <div class="relative ml-auto w-44 min-w-0 shrink">
      <ListFilter class="pointer-events-none absolute top-2 left-2 size-4 text-faint" />
      <input class="input h-7 pl-7 text-[12px]" bind:value={filter} placeholder="Filter results" aria-label="Filter results" />
    </div>

    <div class="flex items-center gap-1">
    <div class="flex rounded-md bg-sunk p-0.5" role="tablist">
      {#each views as v (v.v)}
        {@const I = v.icon}
        <button
          role="tab"
          aria-selected={view === v.v}
          class="flex h-6 items-center gap-1 rounded px-2 text-[12px] font-medium {view === v.v ? 'bg-surface text-ink shadow-sm' : 'text-muted hover:text-ink'}"
          onclick={() => (view = v.v)}
        >
          <I class="size-3.5" /><span class="hidden lg:inline">{v.label}</span>
        </button>
      {/each}
    </div>
    <button class="icon-btn" title="Copy" aria-label="Copy results" onclick={copyAll} disabled={!entries.length}><Copy class="size-3.5" /></button>
    <button class="icon-btn" title="Download" aria-label="Download results" onclick={save} disabled={!entries.length}><Download class="size-3.5" /></button>
    </div>
  </div>

  {#if result.truncated || result.referrals?.length}
    <div class="flex items-start gap-2 border-b border-line bg-danger-soft/60 px-3 py-1.5 text-[12.5px]">
      <TriangleAlert class="mt-0.5 size-3.5 shrink-0 text-danger" />
      <div class="min-w-0">
        {#if result.truncated}<div>Results are incomplete: {result.warning}. Narrow the filter or raise the limit.</div>{/if}
        {#if result.referrals?.length}<div class="break-all">Referrals: {result.referrals.join(", ")}</div>{/if}
      </div>
    </div>
  {/if}

  <div class="min-h-0 flex-1 overflow-auto">
    {#if result.entries.length === 0}
      <div class="flex h-full flex-col items-center justify-center gap-2 p-8 text-center text-muted">
        <SearchX class="size-6 text-faint" />
        <p>No entries matched.</p>
        <p class="text-[12px] text-faint">Check the base DN and scope, or try <code class="kbd">(objectClass=*)</code>. Anonymous binds often see less.</p>
      </div>
    {:else if view === "entries"}
      <div class="space-y-2 p-3">
        {#each entries.slice(0, shown) as e, i (i + e.dn)}
          <EntryCard entry={e} {onopen} />
        {/each}
        {#if entries.length > shown}
          <button class="btn w-full justify-center" onclick={() => (shown += LIMIT)}>Show {Math.min(LIMIT, entries.length - shown)} more</button>
        {/if}
      </div>
    {:else if view === "table"}
      <table class="min-w-full border-collapse text-[12.5px]">
        <thead class="sticky top-0 z-10 bg-sunk">
          <tr>
            <th class="border-b border-line px-3 py-1.5 text-left font-medium whitespace-nowrap text-muted">dn</th>
            {#each columns as c (c)}<th class="border-b border-line px-3 py-1.5 text-left font-mono font-normal whitespace-nowrap text-muted">{c}</th>{/each}
          </tr>
        </thead>
        <tbody>
          {#each entries.slice(0, shown) as e, i (i + e.dn)}
            {@const by = new Map(e.attributes.map((a) => [a.name.toLowerCase(), a.binary ? ["(binary)"] : a.values]))}
            <tr class="border-b border-line hover:bg-hover">
              <td class="max-w-[360px] truncate px-3 py-1 font-mono"><button class="hover:text-accent-text hover:underline" title={e.dn} onclick={() => onopen(e.dn)}>{e.dn}</button></td>
              {#each columns as c (c)}
                <td class="max-w-[280px] truncate px-3 py-1 font-mono" title={cell(by.get(c.toLowerCase()))}>{cell(by.get(c.toLowerCase()))}</td>
              {/each}
            </tr>
          {/each}
        </tbody>
      </table>
      {#if entries.length > shown}
        <div class="p-3"><button class="btn w-full justify-center" onclick={() => (shown += LIMIT)}>Show more</button></div>
      {/if}
    {:else}
      <pre class="m-0 min-h-full p-3 font-mono text-[12px] leading-5 break-all whitespace-pre-wrap">{text}</pre>
    {/if}
  </div>
</div>
