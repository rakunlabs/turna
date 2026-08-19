<script lang="ts">
  import type { Snippet } from "svelte";

  /**
   * A section heading sits on its own rule rather than inside a card. Sections
   * are separated by space and a line; nothing here is boxed for the sake of
   * looking contained.
   */
  let {
    title,
    note = "",
    aside,
    children,
    first = false,
  }: {
    title: string;
    note?: string;
    aside?: Snippet;
    children: Snippet;
    first?: boolean;
  } = $props();
</script>

<section class={first ? "" : "mt-12"}>
  <div class="flex items-baseline gap-4 border-b border-rule pb-1.5">
    <h2 class="stamp stamp-ink shrink-0">{title}</h2>
    <span class="min-w-0 flex-1"></span>
    {#if aside}
      <div class="flex shrink-0 items-center gap-3">{@render aside()}</div>
    {/if}
  </div>

  {#if note}
    <p class="mt-3 max-w-[70ch] text-[13px] leading-[1.6] text-muted">{note}</p>
  {/if}

  <div class="mt-5">{@render children()}</div>
</section>
