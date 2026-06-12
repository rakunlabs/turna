<script lang="ts">
  import { onMount } from "svelte";

  export let apiBase = "/auth/v1";

  type LdapGroup = {
    name: string;
    members: string[];
    description: string;
  };

  type LMap = {
    name: string;
    role_ids: string[];
  };

  let groups: LdapGroup[] = [];
  let lmapByName: Record<string, LMap> = {};
  let roleNameByID: Record<string, string> = {};
  let panelError = "";
  let panelNotice = "";
  let ldapConfigured = true;
  let loading = true;
  let syncing = false;

  async function fetchJSON(path: string) {
    const res = await fetch(`${apiBase}/${path}`);
    const body = await res.json().catch(() => ({}));
    if (!res.ok) {
      const error = new Error(body?.message ?? `${path} failed: ${res.status}`);
      (error as Error & { status?: number }).status = res.status;
      throw error;
    }

    return body;
  }

  async function load() {
    loading = true;
    panelError = "";
    try {
      const [lmapsRes, rolesRes] = await Promise.all([
        fetchJSON("lmaps?_limit=1000"),
        fetchJSON("roles?_limit=1000"),
      ]);

      lmapByName = {};
      for (const lmap of lmapsRes.payload ?? []) {
        lmapByName[lmap.name] = lmap;
      }

      roleNameByID = {};
      for (const role of rolesRes.payload ?? []) {
        roleNameByID[role.id] = role.name;
      }

      try {
        const groupsRes = await fetchJSON("ldap/groups");
        groups = groupsRes.payload ?? [];
        ldapConfigured = true;
      } catch (err) {
        const status = (err as Error & { status?: number }).status;
        // 424: no enabled LDAP config
        ldapConfigured = status !== 424;
        groups = [];
        if (ldapConfigured) throw err;
      }
    } catch (err) {
      panelError = err instanceof Error ? err.message : String(err);
    } finally {
      loading = false;
    }
  }

  function mappedRoles(groupName: string): string[] {
    const lmap = lmapByName[groupName];
    if (!lmap) return [];

    return (lmap.role_ids ?? []).map((id) => roleNameByID[id] ?? id);
  }

  async function syncNow() {
    syncing = true;
    panelError = "";
    panelNotice = "";
    try {
      const res = await fetch(`${apiBase}/ldap/sync`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ force: false }),
      });
      const body = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(body?.message ?? `sync failed: ${res.status}`);

      panelNotice = "LDAP SYNC COMPLETE";
      await load();
    } catch (err) {
      panelError = err instanceof Error ? err.message : String(err);
    } finally {
      syncing = false;
    }
  }

  onMount(load);
</script>

<div class="bg-panel">
  <div class="flex flex-wrap items-center justify-between gap-3 border-b border-line px-4 py-2">
    <span class="t-label text-fg">[ LIVE LDAP GROUPS ]</span>
    <button class="btn-t-solid" disabled={syncing || !ldapConfigured} on:click={syncNow}>
      {syncing ? "SYNCING..." : "RUN LDAP SYNC"}
    </button>
  </div>

  <div class="grid gap-px bg-line p-px">
    <p class="bg-panel p-3 text-[11px] leading-4 text-dim">
      Automatic mapping: on every sync each LDAP group gets a <span class="text-fg">role with the same name</span> (created when missing) and a group map pointing to it. Group members receive the mapped roles as <span class="text-fg">sync roles</span>; users that leave all groups have their sync roles cleared. Edit a group map below to attach more roles to an LDAP group.
    </p>

    {#if panelError}
      <p class="bg-panel px-3 py-2 text-[11px] font-bold uppercase tracking-[0.12em] text-alert">{panelError}</p>
    {/if}
    {#if panelNotice}
      <p class="bg-panel px-3 py-2 text-[11px] font-bold uppercase tracking-[0.12em] text-fg">{panelNotice}</p>
    {/if}

    {#if loading}
      <p class="bg-panel p-3 text-[11px] uppercase tracking-[0.2em] text-dim">QUERYING LDAP...</p>
    {:else if !ldapConfigured}
      <p class="bg-panel p-3 text-[11px] leading-4 text-dim">
        No enabled LDAP configuration. Add one under <span class="text-fg">LDAP / LDAP CONFIGS</span> to see live groups here.
      </p>
    {:else if groups.length === 0}
      <p class="bg-panel p-3 text-[11px] leading-4 text-dim">LDAP returned no groups for the configured filters.</p>
    {:else}
      <div class="hidden grid-cols-[minmax(0,1fr),90px,minmax(0,1.2fr)] gap-4 bg-panel px-3 py-2 md:grid">
        <span class="t-label text-fg">LDAP GROUP</span>
        <span class="t-label text-fg">MEMBERS</span>
        <span class="t-label text-fg">MAPPED ROLES</span>
      </div>
      <div class="grid gap-px bg-line">
        {#each groups as group}
          <div class="grid gap-2 bg-crt px-3 py-2 text-[11px] leading-4 md:grid-cols-[minmax(0,1fr),90px,minmax(0,1.2fr)] md:items-center md:gap-4">
            <div class="min-w-0">
              <p class="truncate font-bold text-fg">{group.name}</p>
              {#if group.description}
                <p class="truncate text-dim">{group.description}</p>
              {/if}
            </div>
            <span class="text-dim">{(group.members ?? []).length}</span>
            <div class="min-w-0">
              {#if lmapByName[group.name]}
                <p class="truncate text-fg">{mappedRoles(group.name).join(", ") || "—"}</p>
              {:else}
                <p class="text-dim">NOT MAPPED — role + map auto-created on next sync</p>
              {/if}
            </div>
          </div>
        {/each}
      </div>
    {/if}
  </div>
</div>
