<script lang="ts">
  import type { Snippet } from "svelte";
  import Seal from "./Seal.svelte";

  /**
   * A value this instance takes from its static config file instead of the
   * database. It is shown, never edited: the only way to change it is the
   * config file and a restart, and the page must say so plainly.
   */
  type Row = { label: string; value: string; muted?: boolean };

  let {
    configKey,
    rows = [],
    children,
  }: {
    /** Dotted path in the middleware config, e.g. "cache.code_store". */
    configKey: string;
    rows?: Row[];
    children?: Snippet;
  } = $props();
</script>

<div class="border-l-2 border-caution py-1 pl-4">
  <div class="flex flex-wrap items-center gap-x-4 gap-y-1.5">
    <Seal state="held" label="Set from config" />
    <span class="serial stamp-raw text-[12px] text-muted">{configKey}</span>
  </div>

  {#if rows.length > 0}
    <dl class="mt-4 grid gap-x-8 gap-y-3 sm:grid-cols-[10rem_minmax(0,1fr)]">
      {#each rows as row (row.label)}
        <dt class="stamp">{row.label}</dt>
        <dd class="serial min-w-0 break-all text-[12.5px] {row.muted ? 'text-muted' : 'text-ink'}">
          {row.value}
        </dd>
      {/each}
    </dl>
  {/if}

  {#if children}
    <p class="mt-4 max-w-[70ch] text-[12px] leading-[1.6] text-muted">{@render children()}</p>
  {/if}
</div>
