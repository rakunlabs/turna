<script lang="ts">
  import type { Snippet } from "svelte";

  import Instrument from "./ui/Instrument.svelte";
  import Seal from "./ui/Seal.svelte";
  import BreakSeal from "./ui/BreakSeal.svelte";
  import { kindSpecs, type ResourceKind } from "../lib/api";
  import { formatStamp } from "../lib/records";
  import { PAGE_SIZE, registry } from "../lib/state/registry.svelte";
  import { editor } from "../lib/state/editor.svelte";
  import { session } from "../lib/state/session.svelte";

  let {
    kind,
    oncommitted,
    extra,
  }: { kind: ResourceKind; oncommitted: () => Promise<void>; extra?: Snippet } = $props();

  let filter = $state("");
  let appliedFilter = $state("");
  let extraFilters = $state<Record<string, string>>({});
  let pendingRevoke = $state("");

  const spec = $derived(kindSpecs[kind]);
  const rows = $derived(registry.rows(kind));

  /**
   * The IAM registers are paged and searched on the server: the filter box is
   * a search term the API resolves against the whole register, not just the
   * page on screen. Short registers apply the same explicit search action
   * against their in-memory rows.
   */
  const paginated = $derived(spec.paginated === true);
  const standing = $derived(registry.standing(kind));

  const shown = $derived(
    !paginated && appliedFilter
      ? rows.filter((row) => `${row.id} ${row.sub}`.toLowerCase().includes(appliedFilter.toLowerCase()))
      : rows,
  );

  const filtersActive = $derived(Object.values(standing.filters ?? {}).some((value) => value !== ""));
  const filterable = $derived(
    paginated ? standing.total > 8 || standing.search !== "" || filtersActive : rows.length > 8,
  );
  const pageable = $derived(paginated && standing.total > PAGE_SIZE);
  const rangeEnd = $derived(standing.offset + rows.length);

  $effect(() => {
    // Changing page must not carry a half-armed revoke or draft values from another register.
    void kind;
    pendingRevoke = "";
    // A paginated register keeps its search term and filters across visits; show them.
    const search = kindSpecs[kind].paginated ? registry.standing(kind).search : "";
    filter = search;
    appliedFilter = search;
    extraFilters = kindSpecs[kind].paginated ? { ...registry.standing(kind).filters } : {};
  });

  async function search() {
    const term = filter.trim();
    const next: Record<string, string> = {};
    for (const field of spec.extraFilters ?? []) {
      const value = (extraFilters[field.param] ?? "").trim();
      if (value) next[field.param] = value;
    }

    if (paginated) {
      await registry.applyQuery(kind, term, next);
      return;
    }

    appliedFilter = term;
  }

  function openRecord(event: MouseEvent, id: string) {
    if (event.button !== 0 || event.ctrlKey || event.metaKey || event.shiftKey || event.altKey) return;

    event.preventDefault();
    void editor.load(kind, id);
  }

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
    {#if paginated}
      <span class="stamp">
        {standing.total}
        {standing.total === 1 ? "record" : "records"}{standing.search
          ? ` matching “${standing.search}”`
          : ""}{filtersActive ? " · filtered" : ""}
      </span>
    {:else}
      <span class="stamp">
        {rows.length}
        {rows.length === 1 ? "record" : "records"}{appliedFilter && shown.length !== rows.length
          ? ` · ${shown.length} shown`
          : ""}
      </span>
    {/if}
    <span class="serial stamp-raw">{session.apiBase}/{spec.listPath}</span>
  {/snippet}

  {#if filterable}
    <form
      class="mb-6"
      onsubmit={(event) => {
        event.preventDefault();
        void search();
      }}
    >
      <div class="max-w-md">
        <label class="stamp block" for="register-filter">{paginated ? "Search" : "Filter"}</label>
        <input
          id="register-filter"
          class="entry mt-1"
          type="search"
          placeholder="{spec.primaryLabel} or {spec.secondaryLabel.toLowerCase()}"
          autocomplete="off"
          bind:value={filter}
        />
      </div>

      {#if paginated && spec.extraFilters?.length}
        <div class="mt-4 grid max-w-2xl gap-x-4 gap-y-4 sm:grid-cols-3">
          {#each spec.extraFilters as field (field.param)}
            <div class="min-w-0">
              <label class="stamp block" for={`register-filter-${field.param}`}>{field.label}</label>
              <input
                id={`register-filter-${field.param}`}
                class="entry mt-1 {field.mono ? 'serial' : ''}"
                type="search"
                list={field.options ? `register-filter-${field.param}-options` : undefined}
                placeholder={field.placeholder}
                autocomplete="off"
                aria-describedby={`register-filter-${field.param}-hint`}
                bind:value={extraFilters[field.param]}
              />
              {#if field.options}
                <datalist id={`register-filter-${field.param}-options`}>
                  {#each field.options as option (option)}
                    <option value={option}></option>
                  {/each}
                </datalist>
              {/if}
              <p
                id={`register-filter-${field.param}-hint`}
                class="mt-1.5 text-[12px] leading-[1.5] text-muted"
              >
                {field.hint}
              </p>
            </div>
          {/each}
        </div>
      {/if}

      {#if paginated}
        <p class="mt-1.5 text-[12px] leading-[1.5] text-muted">
          Search runs on the server across every record, not just this page.
        </p>
      {/if}

      <button type="submit" class="act act-primary mt-4" disabled={session.busy}>
        {session.busy ? "Searching…" : "Search"}
      </button>
    </form>
  {/if}

  {#if rows.length === 0 && !(paginated && (standing.search || filtersActive))}
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
      {appliedFilter || standing.search
        ? `No record matches “${appliedFilter || standing.search}”.`
        : "No record matches the current filters."}
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
                {#if kind === "roles" || kind === "permissions"}
                  <a
                    href={`./#${kind}/${encodeURIComponent(row.id)}`}
                    class="serial block truncate text-[13.5px] font-medium text-ink underline decoration-rule underline-offset-2 hover:decoration-ink"
                    onclick={(event) => openRecord(event, row.id)}
                  >
                    {row.id}
                  </a>
                {:else}
                  <p class="serial truncate text-[13.5px] font-medium text-ink">{row.id}</p>
                {/if}
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

    {#if pageable}
      <div class="mt-5 flex flex-wrap items-center gap-3">
        <button
          type="button"
          class="act"
          disabled={session.busy || standing.offset === 0}
          onclick={() => void registry.turnPage(kind, standing.offset - PAGE_SIZE)}
        >
          ← Previous
        </button>

        <span class="stamp" role="status" aria-live="polite">
          {standing.offset + 1}–{rangeEnd} of {standing.total}
        </span>

        <button
          type="button"
          class="act"
          disabled={session.busy || rangeEnd >= standing.total}
          onclick={() => void registry.turnPage(kind, standing.offset + PAGE_SIZE)}
        >
          Next →
        </button>
      </div>
    {/if}
  {/if}

  {#if extra}
    <div class="mt-12">{@render extra()}</div>
  {/if}
</Instrument>
