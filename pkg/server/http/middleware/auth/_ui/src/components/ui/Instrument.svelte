<script lang="ts">
  import type { Snippet } from "svelte";

  /**
   * Every page in this console is an instrument: a masthead naming what it is,
   * a custody line saying where its truth lives, a closing double rule, then
   * the body. One template for all twenty-five pages — the reason a page here
   * cannot drift into its own private layout.
   */
  let {
    title,
    note = "",
    actions,
    custody,
    children,
    wide = false,
  }: {
    title: string;
    note?: string;
    actions?: Snippet;
    custody?: Snippet;
    children: Snippet;
    wide?: boolean;
  } = $props();
</script>

<article class="px-5 pb-24 pt-8 sm:px-8 lg:px-10">
  <header class={wide ? "" : "max-w-[104ch]"}>
    <div class="flex flex-wrap items-start justify-between gap-x-8 gap-y-4">
      <div class="min-w-0 flex-1 basis-80">
        <h1 class="text-[1.6rem] font-bold leading-[1.15] tracking-[-0.02em] text-ink sm:text-[1.85rem]">
          {title}
        </h1>
        {#if note}
          <p class="mt-2 max-w-[70ch] text-[13.5px] leading-[1.6] text-muted">{note}</p>
        {/if}
      </div>

      {#if actions}
        <div class="flex shrink-0 flex-wrap items-center gap-2">{@render actions()}</div>
      {/if}
    </div>

    {#if custody}
      <div class="mt-5 flex flex-wrap items-center gap-x-6 gap-y-2">{@render custody()}</div>
    {/if}

    <div class="rule-double mt-5"></div>
  </header>

  <div class={wide ? "mt-8" : "mt-8 max-w-[104ch]"}>
    {@render children()}
  </div>
</article>
