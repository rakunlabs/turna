<script lang="ts">
  import { Copy, Search, ListTree, Binary, Link } from "@lucide/svelte";
  import type { Entry } from "../lib/api";
  import { app } from "../lib/state.svelte";
  import { isDN } from "../lib/format";
  import { readable, disabled } from "../lib/values";

  let {
    entry,
    filter = "",
    onopen,
    compact = false,
  }: { entry: Entry; filter?: string; onopen: (dn: string) => void; compact?: boolean } = $props();

  let shownBinary = $state<Record<string, boolean>>({});
  let revealed = $state<Record<string, boolean>>({});

  // servers may return password hashes or even clear text to users who may read them
  const SECRET = /^(userpassword|unicodepwd|sambantpassword|sambalmpassword|ms-mcs-admpwd|mslaps-password|authpassword)$/i;

  const attrs = $derived.by(() => {
    const f = filter.trim().toLowerCase();
    const list = f
      ? entry.attributes.filter((a) => a.name.toLowerCase().includes(f) || a.values.some((v) => v.toLowerCase().includes(f)))
      : entry.attributes;
    return [...list].sort((a, b) => {
      const ao = a.name.toLowerCase() === "objectclass" ? 0 : 1;
      const bo = b.name.toLowerCase() === "objectclass" ? 0 : 1;
      return ao - bo || a.name.localeCompare(b.name, undefined, { sensitivity: "base" });
    });
  });

  async function copy(text: string, what = "Copied") {
    await navigator.clipboard.writeText(text);
    app.toast(what);
  }

  const isDisabled = $derived(disabled(entry.attributes));

  const bytes = (b64: string) => Math.floor((b64.length * 3) / 4) - (b64.endsWith("==") ? 2 : b64.endsWith("=") ? 1 : 0);
</script>

<article class="overflow-hidden rounded-lg border border-line bg-surface">
  <header class="flex items-center gap-2 border-b border-line bg-sunk px-3 py-2">
    <button class="min-w-0 flex-1 truncate text-left font-mono text-[12.5px] font-medium hover:text-accent-text" title="Open {entry.dn}" onclick={() => onopen(entry.dn)}>
      {entry.dn || "(root DSE)"}
    </button>
    {#if isDisabled}<span class="shrink-0 rounded bg-danger-soft px-1.5 text-[11px] font-medium text-danger">disabled</span>{/if}
    <button class="icon-btn" title="Copy DN" aria-label="Copy DN" onclick={() => copy(entry.dn, "DN copied")}><Copy class="size-3.5" /></button>
    {#if entry.dn}
      <button class="icon-btn" title="Search under this entry" aria-label="Search under this entry" onclick={() => app.run({ base_dn: entry.dn, scope: "sub" })}><Search class="size-3.5" /></button>
      <button class="icon-btn" title="List direct children" aria-label="List direct children" onclick={() => app.run({ base_dn: entry.dn, scope: "one", filter: "(objectClass=*)" })}><ListTree class="size-3.5" /></button>
    {/if}
  </header>

  {#if attrs.length === 0}
    <p class="px-3 py-2 text-faint">{entry.attributes.length ? "No attribute matches the filter." : "No attributes returned."}</p>
  {:else}
    <table class="w-full table-fixed border-collapse text-[12.5px]">
      <colgroup><col class={compact ? "w-[min(40%,180px)]" : "w-[min(32%,240px)]"} /><col /></colgroup>
      <tbody>
        {#each attrs as a (a.name)}
          <tr class="border-b border-line align-top last:border-0 hover:bg-hover/50">
            <th class="truncate px-3 py-1.5 text-left font-mono font-normal text-muted" scope="row" title={a.name}>
              {a.name}
              {#if (a.total ?? a.values.length) > 1}<span class="ml-1 text-faint tnum">×{a.total ?? a.values.length}</span>{/if}
            </th>
            <td class="min-w-0 px-3 py-1.5">
              {#if SECRET.test(a.name) && !revealed[a.name]}
                <button class="font-mono text-muted hover:text-ink" title="Show value" onclick={() => (revealed[a.name] = true)}>{"•".repeat(8)} <span class="font-sans text-[12px] text-accent-text">show</span></button>
              {:else if a.binary}
                {@const key = a.name}
                <div class="flex items-center gap-1.5 text-muted">
                  <Binary class="size-3.5" />
                  {#if app.formatted && readable(a.name, a.values[0], true)}
                    <span class="font-mono text-ink">{a.values.map((v) => readable(a.name, v, true) ?? v).join(", ")}</span>
                  {:else}
                    <span>binary · {a.values.map(bytes).join(", ")} bytes</span>
                  {/if}
                  <button class="text-accent-text hover:underline" onclick={() => (shownBinary[key] = !shownBinary[key])}>{shownBinary[key] ? "hide" : "base64"}</button>
                </div>
                {#if shownBinary[key]}
                  {#each a.values as v, i (i)}
                    <div class="mt-1 font-mono text-[11.5px] break-all text-muted">{v}</div>
                  {/each}
                {/if}
              {:else}
                {#each a.values as v, i (i)}
                  <div class="group/v flex items-start gap-1">
                    {#if isDN(v)}
                      <button class="min-w-0 text-left font-mono break-all text-accent-text hover:underline" onclick={() => onopen(v)} title="Open entry">
                        <Link class="mr-1 inline size-3 align-[-1px]" />{v}
                      </button>
                    {:else}
                      {@const nice = app.formatted ? readable(a.name, v) : null}
                      {#if nice}
                        <span class="min-w-0 break-all" title={v}><span class="font-mono">{nice}</span> <span class="font-mono text-[11px] text-faint">{v}</span></span>
                      {:else}
                        <span class="min-w-0 font-mono break-all whitespace-pre-wrap">{v}</span>
                      {/if}
                    {/if}
                    <button class="ml-auto shrink-0 text-faint opacity-0 group-hover/v:opacity-100 hover:text-ink focus:opacity-100" aria-label="Copy value" title="Copy value" onclick={() => copy(v)}>
                      <Copy class="size-3" />
                    </button>
                  </div>
                {/each}
                {#if a.values.length === 0}<span class="text-faint">(no values)</span>{/if}
                {#if a.total}
                  <button class="mt-0.5 text-[12px] text-accent-text hover:underline" onclick={() => onopen(entry.dn)}>+{a.total - a.values.length} more values, open entry</button>
                {/if}
              {/if}
            </td>
          </tr>
        {/each}
      </tbody>
    </table>
  {/if}
</article>
