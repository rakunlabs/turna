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
  <div class="flex flex-col gap-3 border-b border-line p-4 md:flex-row md:items-end md:justify-between md:p-6">
    <div>
      <h3 class="font-display text-2xl leading-tight tracking-tight md:text-3xl">
        {page.title}
      </h3>
      <p class="mt-2 max-w-3xl text-xs leading-5 text-dim">{page.description}</p>
      <p class="t-label mt-2">{rows.length} record{rows.length === 1 ? "" : "s"}</p>
    </div>

    <button class="btn-t-solid shrink-0" on:click={() => onCreate(kind)}>
      + {page.cta}
    </button>
  </div>

  {#if rows.length === 0}
    <div class="grid min-h-48 place-items-center p-8 text-center">
      <div>
        <p class="text-sm font-semibold">No records</p>
        {#if page.canCreate}
          <p class="t-label mt-3">Press "+ {page.cta}" to create the first record.</p>
        {:else}
          <p class="t-label mt-3">Press "+ {page.cta}" to configure a reserved namespace.</p>
        {/if}
      </div>
    </div>
  {:else}
    <div
      class="hidden grid-cols-[1fr,110px,210px,130px] gap-4 border-b border-line px-4 py-2 md:grid md:px-6"
    >
      <span class="t-label">{page.primaryLabel} / {page.secondaryLabel}</span>
      <span class="t-label">Status</span>
      <span class="t-label">Updated (UTC)</span>
      <span class="t-label text-right">Actions</span>
    </div>

    <div class="divide-y divide-line">
      {#each rows as row}
        <div class="grid gap-2 px-4 py-3 hover:bg-panel-hover md:grid-cols-[1fr,110px,210px,130px] md:items-center md:gap-4 md:px-6">
          <div class="min-w-0">
            <p class="truncate text-sm font-medium text-fg">{row.id}</p>
            {#if row.sub}
              <p class="mt-0.5 truncate text-xs text-dim">{row.sub}</p>
            {/if}
          </div>
          <div>
            {#if row.enabled}
              <span class="inline-flex items-center gap-1.5 text-xs text-dim"><span class="h-1.5 w-1.5 rounded-full bg-phosphor"></span>Active</span>
            {:else}
              <span class="inline-flex items-center gap-1.5 text-xs text-alert"><span class="h-1.5 w-1.5 rounded-full bg-alert"></span>Disabled</span>
            {/if}
          </div>
          <p class="text-xs text-dim">{formatDate(row.updated)}</p>
          <div class="flex gap-2 md:justify-end">
            <button
              class="rounded-md border border-line px-3 py-1 text-xs font-medium text-fg hover:bg-panel-hover"
              on:click={() => onEdit(kind, row.id)}
            >
              Edit
            </button>
            <button
              class="rounded-md border border-alert/40 px-3 py-1 text-xs font-medium text-alert hover:bg-alert hover:text-white"
              on:click={() => onDelete(kind, row.id)}
            >
              Delete
            </button>
          </div>
        </div>
      {/each}
    </div>
  {/if}
</div>
