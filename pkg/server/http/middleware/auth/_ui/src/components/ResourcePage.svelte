<script lang="ts">
  import { kindSpecs } from "../lib/api";
  import type { ResourceKind, Row } from "../lib/api";

  export let kind: ResourceKind;
  export let rows: Row[] = [];
  export let onCreate: (kind: ResourceKind) => void;
  export let onEdit: (kind: ResourceKind, id: string) => void;
  export let onDelete: (kind: ResourceKind, id: string) => void;

  $: page = kindSpecs[kind];

  function formatDate(value: string) {
    if (!value) return "—";
    const date = new Date(value);
    if (Number.isNaN(date.getTime())) return value;

    return date
      .toISOString()
      .replace("T", " ")
      .replace(/\.\d+Z$/, "Z");
  }
</script>

<div class="bg-panel">
  <div class="flex flex-col gap-3 border-b border-line p-4 md:flex-row md:items-end md:justify-between">
    <div>
      <p class="t-label">[ POSTGRES RESOURCE ] / REC.COUNT {String(rows.length).padStart(3, "0")}</p>
      <h3 class="mt-2 font-display text-3xl uppercase leading-none tracking-tight md:text-4xl">
        {page.title}
      </h3>
      <p class="mt-3 max-w-3xl text-xs leading-5 text-dim">{page.description}</p>
    </div>

    <button class="btn-t-solid shrink-0" on:click={() => onCreate(kind)}>
      [+] {page.cta}
    </button>
  </div>

  {#if rows.length === 0}
    <div class="grid min-h-48 place-items-center p-8 text-center">
      <div>
        <p class="text-sm font-bold uppercase tracking-[0.2em]">/// NO RECORDS ///</p>
        {#if page.canCreate}
          <p class="t-label mt-3">PRESS [+] {page.cta} TO CREATE A RECORD — IT WILL BE PERSISTED TO POSTGRESQL</p>
        {:else}
          <p class="t-label mt-3">PRESS [+] {page.cta} TO CONFIGURE A RESERVED NAMESPACE</p>
        {/if}
      </div>
    </div>
  {:else}
    <div
      class="hidden grid-cols-[1fr,110px,210px,130px] gap-4 border-b border-line px-4 py-2 md:grid"
    >
      <span class="t-label text-fg">{page.primaryLabel} / {page.secondaryLabel}</span>
      <span class="t-label text-fg">STATUS</span>
      <span class="t-label text-fg">UPDATED [UTC]</span>
      <span class="t-label text-right text-fg">ACTIONS</span>
    </div>

    <div class="divide-y divide-line">
      {#each rows as row, i}
        <div class="grid gap-2 px-4 py-3 md:grid-cols-[1fr,110px,210px,130px] md:items-center md:gap-4">
          <div class="min-w-0">
            <p class="truncate text-sm font-bold text-fg">
              <span class="mr-2 text-[10px] font-medium text-dim">{String(i + 1).padStart(2, "0")}</span
              >{row.id}
            </p>
            {#if row.sub}
              <p class="mt-0.5 truncate pl-6 text-[11px] text-dim">{row.sub}</p>
            {/if}
          </div>
          <div>
            {#if row.enabled}
              <span class="text-[11px] font-bold uppercase tracking-[0.1em] text-fg">[ ACTIVE ]</span>
            {:else}
              <span class="text-[11px] font-bold uppercase tracking-[0.1em] text-alert">[ HALTED ]</span>
            {/if}
          </div>
          <p class="text-[11px] uppercase text-dim">{formatDate(row.updated)}</p>
          <div class="flex gap-px md:justify-end">
            <button
              class="border border-line px-3 py-1 text-[11px] font-bold uppercase tracking-[0.1em] text-fg hover:bg-fg hover:text-crt"
              on:click={() => onEdit(kind, row.id)}
            >
              EDIT
            </button>
            <button
              class="border border-line px-3 py-1 text-[11px] font-bold uppercase tracking-[0.1em] text-alert hover:bg-alert hover:text-white"
              on:click={() => onDelete(kind, row.id)}
            >
              DEL
            </button>
          </div>
        </div>
      {/each}
    </div>
  {/if}
</div>
