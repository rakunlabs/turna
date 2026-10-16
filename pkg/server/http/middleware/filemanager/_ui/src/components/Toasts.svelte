<script lang="ts">
  import { CircleCheck, CircleAlert, X } from "@lucide/svelte";
  import { app } from "../lib/state.svelte";
</script>

<div class="pointer-events-none fixed right-4 bottom-4 z-50 flex flex-col items-end gap-2" aria-live="polite">
  {#each app.toasts as t (t.id)}
    <div
      class="toast pointer-events-auto flex max-w-[420px] items-start gap-2.5 rounded-lg border border-line bg-surface py-2.5 pr-2 pl-3 shadow-[var(--shadow)]"
      role={t.kind === "error" ? "alert" : "status"}
    >
      {#if t.kind === "error"}
        <CircleAlert class="mt-px size-4 shrink-0 text-danger" />
      {:else}
        <CircleCheck class="mt-px size-4 shrink-0 text-ok" />
      {/if}
      <span class="min-w-0 break-words">{t.text}</span>
      <button class="icon-btn -my-1 size-6 shrink-0" onclick={() => app.dismiss(t.id)} aria-label="Dismiss">
        <X class="size-3.5" />
      </button>
    </div>
  {/each}
</div>

<style>
  .toast {
    animation: rise 220ms var(--ease-out);
  }
  @keyframes rise {
    from {
      opacity: 0;
      transform: translateY(6px);
    }
  }
</style>
