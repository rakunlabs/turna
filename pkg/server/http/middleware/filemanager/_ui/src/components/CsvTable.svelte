<script lang="ts">
  import { parseDelimited, delimiterFor } from "../lib/csv";

  let { text, path }: { text: string; path: string } = $props();

  const MAX_ROWS = 2000;

  const parsed = $derived(parseDelimited(text, delimiterFor(path, text.slice(0, 4096)), MAX_ROWS + 1));
  const header = $derived(parsed.rows[0] ?? []);
  const body = $derived(parsed.rows.slice(1, MAX_ROWS + 1));
  const truncated = $derived(parsed.truncated || parsed.rows.length > MAX_ROWS + 1);
  const width = $derived(Math.max(header.length, ...body.slice(0, 200).map((r) => r.length)));
  const numeric = $derived(
    Array.from({ length: width }, (_, c) => {
      const sample = body.slice(0, 50).map((r) => r[c]).filter((v) => v !== undefined && v !== "");
      return sample.length > 0 && sample.every((v) => /^-?[\d.,]+%?$/.test(v.trim()));
    }),
  );
</script>

<div class="absolute inset-0 flex flex-col">
  <div class="min-h-0 flex-1 overflow-auto">
    {#if parsed.rows.length === 0}
      <p class="p-8 text-muted">This file has no rows.</p>
    {:else}
      <table class="tnum min-w-full border-separate border-spacing-0 text-left text-[13px]">
        <thead class="sticky top-0 z-[1]">
          <tr>
            <th class="sticky left-0 z-[2] w-12 border-r border-b border-line bg-sunk px-3 py-2 text-right text-[11px] font-medium text-faint">#</th>
            {#each Array.from({ length: width }, (_, i) => header[i] ?? "") as h, i}
              <th
                class={["border-b border-line bg-sunk px-3 py-2 font-semibold whitespace-nowrap", i > 0 && "border-l", numeric[i] && "text-right"]}
                scope="col"
              >
                {h}
              </th>
            {/each}
          </tr>
        </thead>
        <tbody>
          {#each body as row, r}
            <tr class="hover:bg-hover">
              <td class="sticky left-0 border-r border-b border-line/70 bg-surface px-3 py-1.5 text-right text-[11px] text-faint">{r + 1}</td>
              {#each Array.from({ length: width }, (_, i) => row[i] ?? "") as cell, i}
                <td
                  class={[
                    "max-w-[420px] truncate border-b border-line/70 px-3 py-1.5",
                    i > 0 && "border-l",
                    numeric[i] && "text-right",
                  ]}
                  title={cell.length > 40 ? cell : undefined}
                >
                  {cell}
                </td>
              {/each}
            </tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>
  <div class="tnum flex h-7 shrink-0 items-center gap-4 border-t border-line px-4 text-[12px] text-muted">
    <span>{body.length.toLocaleString()} rows · {width} columns</span>
    {#if truncated}<span class="text-accent-text">Showing the first {MAX_ROWS.toLocaleString()} rows. Switch to Text or download for the rest.</span>{/if}
  </div>
</div>
