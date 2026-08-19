<script lang="ts">
  /**
   * A boolean in this world is a square rail with a square knob — the same
   * corner language as everything else on the sheet. The whole row is the
   * control, so a monthly visitor never has to hit a 34px target, and the
   * visible text is the accessible name rather than a detached label.
   */
  let {
    label,
    checked = $bindable(false),
    hint = "",
    disabled = false,
    /** Set when turning this on widens what the system accepts. */
    consequential = false,
    id = crypto.randomUUID(),
  }: {
    label: string;
    checked?: boolean;
    hint?: string;
    disabled?: boolean;
    consequential?: boolean;
    id?: string;
  } = $props();

  const hintID = $derived(hint ? `${id}-hint` : undefined);
  const railOn = $derived(consequential ? "bg-seal border-seal" : "bg-carbon border-carbon");
</script>

<div class="min-w-0">
  <button
    {id}
    type="button"
    role="switch"
    aria-checked={checked}
    aria-describedby={hintID}
    {disabled}
    class="flex w-full items-center gap-3 py-1 text-left transition-opacity disabled:cursor-not-allowed disabled:opacity-55"
    onclick={() => (checked = !checked)}
  >
    <span
      class="relative inline-flex h-[18px] w-[34px] shrink-0 items-center border transition-colors duration-150
        {checked ? railOn : 'border-faint bg-transparent'}"
      aria-hidden="true"
    >
      <span
        class="absolute top-[2px] h-[12px] w-[12px] transition-[left] duration-150 ease-[var(--ease-settle)]
          {checked ? 'left-[19px] bg-white' : 'left-[2px] bg-faint'}"
      ></span>
    </span>
    <span class="min-w-0 text-[13.5px] leading-[1.45] text-ink">{label}</span>
  </button>

  {#if hint}
    <p id={hintID} class="ml-[46px] max-w-[62ch] text-[12px] leading-[1.5] text-muted">{hint}</p>
  {/if}
</div>
