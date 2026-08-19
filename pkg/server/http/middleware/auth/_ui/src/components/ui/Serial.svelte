<script lang="ts">
  /**
   * A serial is a value that came from the server and can change under the
   * operator without them acting: the auth version, a record count, a key id.
   * When it advances it re-stamps rather than silently swapping, so a change
   * that happened on another instance is something you see arrive.
   */
  let {
    value,
    size = "lg",
    tone = "ink",
  }: {
    value: string | number | null | undefined;
    size?: "sm" | "md" | "lg" | "xl";
    tone?: "ink" | "muted" | "seal" | "carbon";
  } = $props();

  const sizes = {
    sm: "text-[13px] font-medium",
    md: "text-xl font-semibold",
    lg: "text-[2.75rem] leading-[0.95] font-bold tracking-[-0.03em]",
    xl: "text-[4.5rem] leading-[0.9] font-bold tracking-[-0.035em]",
  };

  const tones = {
    ink: "text-ink",
    muted: "text-muted",
    seal: "text-seal",
    carbon: "text-carbon",
  };

  const shown = $derived(value === null || value === undefined || value === "" ? "—" : String(value));
</script>

{#key shown}
  <span class="serial stamp-in inline-block {sizes[size]} {tones[tone]}">{shown}</span>
{/key}
