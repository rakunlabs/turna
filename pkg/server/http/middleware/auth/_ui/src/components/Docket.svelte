<script lang="ts">
  import { docket } from "../lib/state/session.svelte";

  /**
   * The docket records what the console just did. A commit clears itself; a
   * rejection stays until dismissed, because losing an error message loses the
   * only statement of why the write did not happen.
   */
</script>

<div
  class="pointer-events-none fixed bottom-0 right-0 z-50 flex w-[min(30rem,100vw)] flex-col items-stretch gap-px p-4"
>
  {#each docket.entries as entry (entry.id)}
    <div
      class="stamp-in pointer-events-auto flex items-start gap-3 border bg-sheet px-3.5 py-2.5
        {entry.kind === 'rejected' ? 'border-seal' : 'border-endorsed'}"
      role={entry.kind === "rejected" ? "alert" : "status"}
    >
      <span class="stamp mt-[3px] shrink-0 {entry.kind === 'rejected' ? 'text-seal' : 'text-endorsed'}">
        {entry.kind === "rejected" ? "Rejected" : "Committed"}
      </span>
      <span class="min-w-0 flex-1 break-words text-[13px] leading-[1.5] text-ink">{entry.text}</span>
      <button
        type="button"
        class="act-quiet -mr-1 -mt-1 shrink-0 border-0 bg-transparent p-1 text-muted hover:text-ink"
        aria-label="Dismiss"
        onclick={() => docket.dismiss(entry.id)}
      >
        <svg width="12" height="12" viewBox="0 0 12 12" aria-hidden="true" fill="none">
          <path d="M1 1L11 11M11 1L1 11" stroke="currentColor" stroke-width="1.5" />
        </svg>
      </button>
    </div>
  {/each}
</div>
