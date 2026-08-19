<script lang="ts">
  /**
   * The seal states an instrument's standing at a glance: intact and endorsed,
   * broken, held, or never issued.
   *
   * A struck square, not a dot — the console's corner language is square
   * throughout, and a round status light is the one shape that would read as
   * borrowed from a generic dashboard rather than from this world.
   */
  type State = "endorsed" | "broken" | "held" | "void";

  let {
    state = "endorsed",
    label = "",
    size = 11,
  }: { state?: State; label?: string; size?: number } = $props();

  const marks: Record<State, { ring: string; fill: string; text: string }> = {
    endorsed: { ring: "border-endorsed", fill: "bg-endorsed", text: "text-endorsed" },
    broken: { ring: "border-seal", fill: "bg-seal", text: "text-seal" },
    held: { ring: "border-caution", fill: "bg-caution", text: "text-caution" },
    void: { ring: "border-faint", fill: "bg-transparent", text: "text-muted" },
  };

  const mark = $derived(marks[state]);
</script>

<span class="inline-flex items-center gap-2">
  <span
    class="inline-grid shrink-0 place-items-center border {mark.ring}"
    style="width:{size}px;height:{size}px"
    aria-hidden="true"
  >
    <span class={mark.fill} style="width:{size - 6}px;height:{size - 6}px"></span>
  </span>
  {#if label}
    <span class="stamp {mark.text}">{label}</span>
  {/if}
</span>
