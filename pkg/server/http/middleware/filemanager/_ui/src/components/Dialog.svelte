<script lang="ts">
  import { dialog } from "../lib/dialog.svelte";

  let el = $state<HTMLDialogElement>();
  let input = $state<HTMLInputElement>();
  let value = $state("");
  let error = $state<string | null>(null);

  const d = $derived(dialog.current);

  $effect(() => {
    if (!el) return;
    if (d) {
      value = d.value ?? "";
      error = null;
      if (!el.open) el.showModal();
      queueMicrotask(() => {
        if (!input) return;
        input.focus();
        const [s, e] = d.select ?? [0, value.length];
        input.setSelectionRange(s, e);
      });
    } else if (el.open) {
      el.close();
    }
  });

  $effect(() => {
    if (d?.input && d.validate) error = d.validate(value);
  });

  function submit(e: SubmitEvent) {
    e.preventDefault();
    if (!d) return;
    if (d.input) {
      const v = value.trim();
      const err = d.validate?.(v) ?? (v ? null : "Required");
      if (err) {
        error = err;
        return;
      }
      dialog.close(v);
    } else {
      dialog.close("");
    }
  }
</script>

<dialog
  bind:this={el}
  onclose={() => dialog.current && dialog.close(null)}
  onclick={(e) => e.target === el && dialog.close(null)}
  class="m-auto w-[min(440px,calc(100vw-32px))] rounded-xl border border-line bg-surface p-0 text-ink shadow-[var(--shadow)] backdrop:bg-black/30 backdrop:backdrop-blur-[1px]"
  aria-labelledby="dlg-title"
>
  {#if d}
    <form onsubmit={submit} class="flex flex-col gap-4 p-5">
      <div class="flex flex-col gap-1">
        <h2 id="dlg-title" class="text-[15px] font-semibold tracking-[-0.01em]">{d.title}</h2>
        {#if d.description}
          <p class="text-muted">{d.description}</p>
        {/if}
      </div>

      {#if d.input}
        <label class="flex flex-col gap-1.5">
          {#if d.label}<span class="text-[12px] font-medium text-muted">{d.label}</span>{/if}
          <input
            bind:this={input}
            bind:value
            class="input font-mono"
            placeholder={d.placeholder}
            spellcheck="false"
            autocomplete="off"
            aria-invalid={!!error}
          />
          <span class="min-h-[18px] text-[12px] text-danger" aria-live="polite">{error ?? ""}</span>
        </label>
      {/if}

      <div class="flex justify-end gap-2">
        <button type="button" class="btn" onclick={() => dialog.close(null)}>Cancel</button>
        <button type="submit" class={["btn", d.danger ? "btn-danger" : "btn-primary"]} disabled={d.input && !!error}>
          {d.confirm}
        </button>
      </div>
    </form>
  {/if}
</dialog>
