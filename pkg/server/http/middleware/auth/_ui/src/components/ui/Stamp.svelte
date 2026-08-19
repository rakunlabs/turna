<script lang="ts">
  import type { Snippet } from "svelte";

  type Tone = "muted" | "ink" | "seal" | "carbon" | "endorsed" | "caution";

  let {
    children,
    tone = "muted",
    class: extra = "",
  }: { children: Snippet; tone?: Tone; class?: string } = $props();

  const tones: Record<Tone, string> = {
    muted: "text-muted",
    ink: "text-ink",
    seal: "text-seal",
    carbon: "text-carbon",
    endorsed: "text-endorsed",
    caution: "text-caution",
  };
</script>

<span class="stamp {tones[tone]} {extra}">{@render children()}</span>
