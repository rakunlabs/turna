<script lang="ts">
  import { onDestroy } from "svelte";

  /**
   * The console's signature control, for the acts that cannot be taken back:
   * rotating the signing key, revoking a credential, deleting a principal.
   *
   * The consequence is printed on a tamper-evident band, and the action stays
   * inert until the operator breaks the seal. Once broken it arms for a fixed
   * window and re-seals itself, so an armed control never sits forgotten in a
   * background tab. No modal: nothing here needs to interrupt or trap focus,
   * and a dialog that says "Are you sure?" carries none of the consequence.
   */
  let {
    consequence,
    action,
    onconfirm,
    disabled = false,
    window: armWindow = 10,
  }: {
    consequence: string;
    action: string;
    onconfirm: () => void;
    disabled?: boolean;
    window?: number;
  } = $props();

  let remaining = $state(0);
  let timer: ReturnType<typeof setInterval> | undefined;

  const armed = $derived(remaining > 0);

  function clear() {
    if (timer) clearInterval(timer);
    timer = undefined;
  }

  function arm() {
    remaining = armWindow;
    clear();
    timer = setInterval(() => {
      remaining -= 1;
      if (remaining <= 0) clear();
    }, 1000);
  }

  function reseal() {
    remaining = 0;
    clear();
  }

  function confirm() {
    reseal();
    onconfirm();
  }

  onDestroy(clear);
</script>

<div class="border border-seal/45">
  <p class="hatch border-b border-seal/30 px-4 py-3 text-[13px] leading-[1.55] text-ink">
    {consequence}
  </p>

  <div class="flex flex-wrap items-center gap-3 px-4 py-3">
    {#if !armed}
      <button type="button" class="act act-seal" {disabled} onclick={arm}>Break seal</button>
      <span class="stamp">Sealed — this action is inert</span>
    {:else}
      <button type="button" class="act act-seal" {disabled} onclick={confirm}>{action}</button>
      <button type="button" class="act act-quiet" onclick={reseal}>Re-seal</button>
      <span class="stamp text-seal" role="status" aria-live="polite">
        Seal broken — re-seals in {remaining}s
      </span>
    {/if}
  </div>

  {#if armed}
    <!-- Driven off the countdown state, not a CSS animation: a blanket
         reduced-motion override would otherwise collapse the drain instantly
         and take the timing affordance from the people most entitled to it. -->
    <div class="h-[2px] w-full bg-seal/25" aria-hidden="true">
      <div
        class="h-full bg-seal transition-[width] duration-1000 ease-linear motion-reduce:transition-none"
        style="width: {(remaining / armWindow) * 100}%"
      ></div>
    </div>
  {/if}
</div>
