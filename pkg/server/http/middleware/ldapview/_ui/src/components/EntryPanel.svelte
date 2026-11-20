<script lang="ts">
  import { untrack } from "svelte";
  import { LoaderCircle, Eye, EyeOff, X, Users, User, Copy } from "@lucide/svelte";
  import { api, type Entry } from "../lib/api";
  import { app } from "../lib/state.svelte";
  import { escapeFilter, rdn } from "../lib/format";
  import EntryCard from "./EntryCard.svelte";

  let { dn, onopen, onclose }: { dn: string; onopen: (dn: string) => void; onclose: () => void } = $props();

  type Tab = "attributes" | "memberOf" | "members";

  let entry = $state<Entry | null>(null);
  let loading = $state(false);
  let error = $state<string | null>(null);
  let operational = $state(false);
  let tab = $state<Tab>("attributes");
  let filter = $state("");

  // memberships and members are loaded on demand
  let groups = $state<string[] | null>(null);
  let groupsLoading = $state(false);
  let nested = $state(false);
  let nestedMembers = $state<string[] | null>(null);

  const get = (name: string) => entry?.attributes.find((a) => a.name.toLowerCase() === name.toLowerCase());
  const classes = $derived((get("objectClass")?.values ?? []).map((c) => c.toLowerCase()));
  const isGroup = $derived(classes.some((c) => c.includes("group")) || !!get("member") || !!get("uniqueMember") || !!get("memberUid"));
  const members = $derived([...new Set([...(get("member")?.values ?? []), ...(get("uniqueMember")?.values ?? []).map((v) => v.replace(/#'.*'B$/, ""))])]);
  const memberUids = $derived([...new Set(get("memberUid")?.values ?? [])]);

  // a newer selection wins over a slower earlier response
  let request = 0;

  async function load(target: string, withOperational: boolean) {
    const id = ++request;
    loading = true;
    error = null;
    try {
      const res = target === "" ? await api.rootDSE() : await api.entry(target, withOperational);
      if (id !== request) return;
      entry = res.entries[0] ?? null;
      if (res.truncated) error = res.warning ?? "Entry was truncated";
    } catch (e) {
      if (id !== request) return;
      entry = null;
      error = (e as Error).message;
    } finally {
      if (id === request) loading = false;
    }
  }

  $effect(() => {
    const target = dn;
    const op = operational;
    untrack(() => {
      groups = null;
      nestedMembers = null;
      load(target, op);
    });
  });

  $effect(() => {
    if (tab === "members" && !isGroup) tab = "attributes";
  });

  async function loadGroups() {
    if (!entry) return;
    const target = entry.dn;
    groupsLoading = true;
    try {
      const found = new Set<string>(get("memberOf")?.values ?? []);
      const d = escapeFilter(entry.dn);
      let f: string;
      if (nested && app.flavor === "ad") f = `(member:1.2.840.113556.1.4.1941:=${d})`;
      else {
        const uid = get("uid")?.values[0];
        f = `(|(member=${d})(uniqueMember=${d})${uid ? `(memberUid=${escapeFilter(uid)})` : ""})`;
      }
      const res = await api.search({
        base_dn: app.baseDN,
        scope: "sub",
        filter: f,
        attributes: ["1.1"],
        size_limit: 0,
        time_limit: 0,
        deref: "never",
        types_only: false,
        page_size: app.flavor === "ad" ? 500 : 0,
      });
      if (entry?.dn !== target) return;
      for (const e of res.entries) found.add(e.dn);
      groups = [...found].sort((a, b) => a.localeCompare(b));
      if (res.truncated) app.toast(res.warning ?? "Group list is incomplete", "error");
    } catch (e) {
      groups = [];
      app.toast((e as Error).message, "error");
    } finally {
      groupsLoading = false;
    }
  }

  async function loadNestedMembers() {
    if (!entry) return;
    const target = entry.dn;
    groupsLoading = true;
    try {
      const res = await api.search({
        base_dn: app.baseDN,
        scope: "sub",
        filter: `(memberOf:1.2.840.113556.1.4.1941:=${escapeFilter(entry.dn)})`,
        attributes: ["1.1"],
        size_limit: 0,
        time_limit: 0,
        deref: "never",
        types_only: false,
        page_size: 500,
      });
      if (entry?.dn !== target) return;
      nestedMembers = res.entries.map((e) => e.dn);
    } catch (e) {
      nestedMembers = [];
      app.toast((e as Error).message, "error");
    } finally {
      groupsLoading = false;
    }
  }

  $effect(() => {
    if (tab === "memberOf" && groups === null && entry && !groupsLoading) loadGroups();
  });

  $effect(() => {
    if (tab === "members" && nested && nestedMembers === null && entry && !groupsLoading) loadNestedMembers();
  });

  function setNested(v: boolean) {
    nested = v;
    groups = null;
    nestedMembers = null;
  }

  const shownMembers = $derived(nested && nestedMembers ? nestedMembers : members);
  const list = $derived.by(() => {
    const items = tab === "memberOf" ? (groups ?? []) : shownMembers;
    const f = filter.trim().toLowerCase();
    return f ? items.filter((v) => v.toLowerCase().includes(f)) : items;
  });

  async function copyList() {
    await navigator.clipboard.writeText(list.join("\n"));
    app.toast(`${list.length} DNs copied`);
  }
</script>

<div class="flex items-center gap-1 border-b border-line px-2 py-1.5">
  <div class="flex rounded-md bg-sunk p-0.5" role="tablist">
    {#each [["attributes", "Attributes"], ["memberOf", "Member of"], ...(isGroup ? [["members", "Members"]] : [])] as [v, label] (v)}
      <button role="tab" aria-selected={tab === v} class="h-6 rounded px-2 text-[12px] font-medium {tab === v ? 'bg-surface text-ink shadow-sm' : 'text-muted hover:text-ink'}" onclick={() => (tab = v as Tab)}>
        {label}
        {#if v === "members"}<span class="text-faint tnum">{members.length + memberUids.length}</span>{/if}
        {#if v === "memberOf" && groups}<span class="text-faint tnum">{groups.length}</span>{/if}
      </button>
    {/each}
  </div>
  {#if loading || groupsLoading}<LoaderCircle class="size-3.5 animate-spin text-faint" />{/if}
  <div class="ml-auto flex items-center">
    {#if tab === "attributes"}
      <button class="btn btn-ghost h-7 px-2" aria-pressed={operational} title="Include operational attributes (+)" onclick={() => (operational = !operational)}>
        {#if operational}<Eye class="size-3.5" />{:else}<EyeOff class="size-3.5" />{/if} Operational
      </button>
    {:else if app.flavor === "ad"}
      <label class="flex items-center gap-1.5 px-2 text-[12px] text-muted" title="Follow nested groups with LDAP_MATCHING_RULE_IN_CHAIN">
        <input type="checkbox" checked={nested} onchange={(e) => setNested(e.currentTarget.checked)} /> Nested
      </label>
    {/if}
    <button class="icon-btn" aria-label="Close" onclick={onclose}><X class="size-4" /></button>
  </div>
</div>

<div class="min-h-0 flex-1 overflow-auto p-3">
  {#if error}
    <div class="mb-2 rounded-md bg-danger-soft px-3 py-2 break-words text-danger" role="alert">{error}</div>
  {/if}

  {#if tab === "attributes"}
    {#if entry}
      <input class="input mb-2 h-7 text-[12px]" bind:value={filter} placeholder="Filter attributes" aria-label="Filter attributes" />
      <EntryCard {entry} {filter} {onopen} compact />
    {:else if !loading && !error}
      <p class="text-faint">Entry not readable with this bind.</p>
    {/if}
  {:else}
    <div class="mb-2 flex items-center gap-2">
      <input class="input h-7 flex-1 text-[12px]" bind:value={filter} placeholder="Filter" aria-label="Filter list" />
      <button class="icon-btn" title="Copy DNs" aria-label="Copy DNs" onclick={copyList} disabled={!list.length}><Copy class="size-3.5" /></button>
    </div>

    {#if tab === "memberOf" && groups === null}
      <p class="text-faint">Looking up groups…</p>
    {:else if list.length === 0 && memberUids.length === 0}
      <p class="text-faint">{tab === "memberOf" ? "Not a member of any group this bind can see." : "No members."}</p>
    {:else}
      <ul class="space-y-px">
        {#each list.slice(0, 1000) as d (d)}
          <li>
            <button class="flex w-full items-center gap-2 rounded-md px-2 py-1 text-left hover:bg-hover" onclick={() => onopen(d)} title={d}>
              {#if tab === "memberOf"}<Users class="size-3.5 shrink-0 text-muted" />{:else}<User class="size-3.5 shrink-0 text-muted" />{/if}
              <span class="min-w-0 flex-1">
                <span class="block truncate">{rdn(d)}</span>
                <span class="block truncate font-mono text-[11px] text-faint">{d}</span>
              </span>
            </button>
          </li>
        {/each}
      </ul>
      {#if list.length > 1000}<p class="mt-2 text-[12px] text-faint">Showing 1000 of {list.length}. Use the filter to narrow.</p>{/if}
      {#if tab === "members" && memberUids.length && !nested}
        <p class="mt-3 mb-1 text-[11.5px] font-medium text-faint uppercase">memberUid</p>
        <div class="flex flex-wrap gap-1">
          {#each memberUids as u (u)}
            <button class="rounded-full border border-line px-2 py-px font-mono text-[12px] hover:border-line-strong" onclick={() => app.run({ base_dn: app.baseDN, scope: "sub", filter: `(uid=${escapeFilter(u)})` })}>{u}</button>
          {/each}
        </div>
      {/if}
    {/if}
  {/if}
</div>
