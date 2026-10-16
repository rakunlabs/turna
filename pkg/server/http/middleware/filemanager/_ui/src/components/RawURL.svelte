<script lang="ts">
  import { Copy, Check, Terminal } from "@lucide/svelte";
  import { app } from "../lib/state.svelte";

  let { path, type }: { path: string; type?: string } = $props();

  const url = $derived(app.rawURL(path));
  const relative = $derived(`${app.info.prefix}${app.info.raw_path}${encodeURI(path)}`);
  // break only after slashes so names stay whole
  const segments = $derived(relative.split(/(?<=\/)/));

  let copied = $state<"url" | "curl" | null>(null);
  let timer: ReturnType<typeof setTimeout>;

  async function copy(kind: "url" | "curl") {
    const text = kind === "url" ? url : `curl -fsSL '${url}'`;
    try {
      await navigator.clipboard.writeText(text);
    } catch {
      const ta = document.createElement("textarea");
      ta.value = text;
      document.body.append(ta);
      ta.select();
      document.execCommand("copy");
      ta.remove();
    }
    copied = kind;
    clearTimeout(timer);
    timer = setTimeout(() => (copied = null), 1600);
  }
</script>

<div class="flex flex-col gap-2">
  <div class="flex items-baseline justify-between">
    <h3 class="text-[12px] font-medium text-muted">Raw URL</h3>
    <a href={url} target="_blank" rel="noreferrer" class="text-[12px] text-accent-text underline-offset-2 hover:underline">Open</a>
  </div>

  <button
    class="group relative w-full rounded-lg border border-line bg-sunk p-3 text-left transition-colors duration-150 hover:border-line-strong"
    onclick={() => copy("url")}
    aria-label="Copy raw URL"
  >
    <div class="flex items-center gap-2">
      <span class="rounded bg-accent px-1.5 py-px font-mono text-[11px] font-semibold tracking-wide text-accent-ink">GET</span>
      <span class="ml-auto text-faint transition-colors group-hover:text-ink">
        {#if copied === "url"}
          <Check class="size-3.5 text-ok" />
        {:else}
          <Copy class="size-3.5" />
        {/if}
      </span>
    </div>
    <p class="mt-2 font-mono text-[13px] leading-snug [overflow-wrap:anywhere] text-ink">
      {#each segments as seg}{seg}<wbr />{/each}
    </p>
    {#if type}
      <p class="mt-1.5 font-mono text-[11px] text-faint">Content-Type: {type.split(";")[0]}</p>
    {/if}
  </button>

  <button class="btn btn-ghost h-7 justify-start px-1.5 text-[12px]" onclick={() => copy("curl")}>
    {#if copied === "curl"}
      <Check class="size-3.5 text-ok" /> Copied curl command
    {:else}
      <Terminal class="size-3.5" /> Copy as curl
    {/if}
  </button>
  <span class="sr-only" aria-live="polite">{copied ? "Copied" : ""}</span>
</div>
