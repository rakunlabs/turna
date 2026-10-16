<script lang="ts">
  import { LoaderCircle } from "@lucide/svelte";

  let { url, name }: { url: string; name: string } = $props();

  let family = $state<string | null>(null);
  let failed = $state(false);
  let sample = $state("The quick brown fox jumps over the lazy dog");

  $effect(() => {
    const id = `fm-font-${Math.random().toString(36).slice(2)}`;
    const face = new FontFace(id, `url("${url}")`);
    let added = false;
    failed = false;
    family = null;
    face
      .load()
      .then((f) => {
        document.fonts.add(f);
        added = true;
        family = id;
      })
      .catch(() => (failed = true));
    return () => {
      if (added) document.fonts.delete(face);
    };
  });

  const sizes = [64, 40, 24, 16, 12];
</script>

<div class="absolute inset-0 overflow-y-auto p-8">
  {#if failed}
    <p class="font-medium">This font could not be loaded</p>
    <p class="mt-0.5 text-muted">The browser does not support this font format. Download it to install it locally.</p>
  {:else if !family}
    <div class="flex items-center gap-2 text-muted"><LoaderCircle class="size-4 animate-spin" /> Loading font</div>
  {:else}
    <div class="flex max-w-[960px] flex-col gap-8">
      <label class="flex flex-col gap-1.5">
        <span class="text-[12px] font-medium text-muted">Sample text</span>
        <input class="input max-w-[520px]" bind:value={sample} />
      </label>
      <p class="text-[13px] text-muted">{name}</p>
      {#each sizes as size}
        <div class="flex items-baseline gap-4 border-b border-line pb-4">
          <span class="tnum w-10 shrink-0 text-[11px] text-faint">{size}px</span>
          <p class="min-w-0 break-words" style:font-family={family} style:font-size="{size}px" style:line-height="1.2">{sample}</p>
        </div>
      {/each}
      <p class="break-all" style:font-family={family} style:font-size="28px" style:line-height="1.5">
        ABCDEFGHIJKLMNOPQRSTUVWXYZ<br />abcdefghijklmnopqrstuvwxyz<br />0123456789 &amp; ! ? @ # % ( ) [ ] {"{ }"} çğıöşü ÇĞİÖŞÜ
      </p>
    </div>
  {/if}
</div>
