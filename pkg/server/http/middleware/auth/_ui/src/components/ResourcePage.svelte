<script lang="ts">
  import type { Snippet } from "svelte";

  import Instrument from "./ui/Instrument.svelte";
  import Seal from "./ui/Seal.svelte";
  import BreakSeal from "./ui/BreakSeal.svelte";
  import { kindSpecs, type ResourceKind } from "../lib/api";
  import { formatStamp } from "../lib/records";
  import { registry } from "../lib/state/registry.svelte";
  import { editor } from "../lib/state/editor.svelte";
  import { session } from "../lib/state/session.svelte";

  let {
    kind,
    oncommitted,
    extra,
  }: { kind: ResourceKind; oncommitted: () => Promise<void>; extra?: Snippet } = $props();

  let filter = $state("");
  let pendingRevoke = $state("");

  const spec = $derived(kindSpecs[kind]);
  const rows = $derived(registry.rows(kind));

  const shown = $derived(
    filter.trim()
      ? rows.filter((row) => `${row.id} ${row.sub}`.toLowerCase().includes(filter.trim().toLowerCase()))
      : rows,
  );

  // Identity lists reach the paging cap; anything else is short enough to read.
  const filterable = $derived(rows.length > 8);

  $effect(() => {
    // Changing page must not carry a half-armed revoke with it.
    void kind;
    pendingRevoke = "";
    filter = "";
  });

  function revoke(id: string) {
    void editor.remove(kind, id, oncommitted);
    pendingRevoke = "";
  }
</script>

<Instrument title={spec.title} note={spec.description} wide>
  {#snippet actions()}
    <button type="button" class="act act-primary" onclick={() => editor.startCreate(kind)}>
      {spec.cta}
    </button>
  {/snippet}

  {#snippet custody()}
    <span class="stamp">
      {rows.length}
      {rows.length === 1 ? "record" : "records"}{filter.trim() && shown.length !== rows.length
        ? ` · ${shown.length} shown`
        : ""}
    </span>
    <span class="serial stamp-raw">{session.apiBase}/{spec.listPath}</span>
  {/snippet}

  {#if filterable}
    <div class="mb-6 max-w-md">
      <label class="stamp block" for="register-filter">Filter</label>
      <input
        id="register-filter"
        class="entry mt-1"
        type="search"
        placeholder="{spec.primaryLabel} or {spec.secondaryLabel.toLowerCase()}"
        autocomplete="off"
        bind:value={filter}
      />
    </div>
  {/if}

  {#if rows.length === 0}
    <div class="border border-dashed border-rule px-6 py-14 text-center">
      <p class="text-[15px] font-semibold text-ink">Nothing issued yet</p>
      <p class="mx-auto mt-2 max-w-[52ch] text-[13px] leading-[1.6] text-muted">
        {spec.canCreate
          ? spec.description
          : "Only the reserved namespaces can be written here — open one to set its values."}
      </p>
      <button type="button" class="act act-primary mt-6" onclick={() => editor.startCreate(kind)}>
        {spec.cta}
      </button>
    </div>
  {:else if shown.length === 0}
    <p class="border border-dashed border-rule px-6 py-12 text-center text-[13px] text-muted">
      No record matches “{filter}”.
    </p>
  {:else}
    <div class="border border-rule bg-sheet">
      <!-- Column headings sit on a rule; the register is a ledger, not cards. -->
      <div
        class="hidden items-baseline gap-4 border-b border-rule px-4 py-2 md:grid md:grid-cols-[minmax(0,1fr)_7rem_9rem_9.5rem]"
      >
        <span class="stamp">{spec.primaryLabel} · {spec.secondaryLabel}</span>
        <span class="stamp">Standing</span>
        <span class="stamp">Amended</span>
        <span class="stamp text-right">Instrument</span>
      </div>

      <ul>
        {#each shown as row (row.id)}
          <li class="border-b border-rule last:border-b-0">
            <div
              class="grid gap-x-4 gap-y-1.5 px-4 py-3 transition-colors hover:bg-raised md:grid-cols-[minmax(0,1fr)_7rem_9rem_9.5rem] md:items-center"
            >
              <div class="min-w-0">
                <p class="serial truncate text-[13.5px] font-medium text-ink">{row.id}</p>
                {#if row.sub}
                  <p class="mt-0.5 truncate text-[12px] text-muted">{row.sub}</p>
                {/if}
              </div>

              <div class="min-w-0">
                <Seal state={row.enabled ? "endorsed" : "void"} label={row.enabled ? "Active" : "Held"} />
              </div>

              <p class="serial min-w-0 truncate text-[12px] text-muted">
                {formatStamp(row.updated) || "—"}
              </p>

              <div class="flex gap-2 md:justify-end">
                <button type="button" class="act" onclick={() => void editor.load(kind, row.id)}>
                  Amend
                </button>
                <button
                  type="button"
                  class="act act-quiet text-seal hover:bg-seal/10 hover:text-seal"
                  aria-expanded={pendingRevoke === row.id}
                  onclick={() => (pendingRevoke = pendingRevoke === row.id ? "" : row.id)}
                >
                  Revoke
                </button>
              </div>
            </div>

            {#if pendingRevoke === row.id}
              <div class="px-4 pb-4">
                <BreakSeal
                  consequence={`Revoking ${row.id} deletes the record from PostgreSQL. Anything authenticating or authorising through it stops working on the next request, and there is no undo.`}
                  action="Revoke {row.id}"
                  disabled={session.busy}
                  onconfirm={() => revoke(row.id)}
                />
              </div>
            {/if}
          </li>
        {/each}
      </ul>
    </div>
  {/if}

  {#if extra}
    <div class="mt-12">{@render extra()}</div>
  {/if}
</Instrument>
