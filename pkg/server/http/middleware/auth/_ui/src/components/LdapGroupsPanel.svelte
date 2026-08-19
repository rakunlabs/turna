<script lang="ts">
  import { onMount } from "svelte";

  import Section from "./ui/Section.svelte";
  import Seal from "./ui/Seal.svelte";
  import Switch from "./ui/Switch.svelte";
  import { messageOf, session, statusOf } from "../lib/state/session.svelte";
  import { registry } from "../lib/state/registry.svelte";

  type LdapGroup = { name: string; members?: string[]; description?: string };
  type LMap = { name: string; role_ids?: string[] };
  type Role = { id: string; name: string };

  /**
   * A panel, not a page: it hangs under the group-map register and reports what
   * the directory actually holds right now, beside the maps that were written
   * for it. Everything here is read from LDAP on load — nothing is stored.
   */
  let groups = $state<LdapGroup[]>([]);
  let lmapByName = $state<Record<string, LMap>>({});
  let roleNameByID = $state<Record<string, string>>({});
  let problem = $state("");
  let ldapConfigured = $state(true);
  let loading = $state(true);
  let syncing = $state(false);
  let forceSync = $state(false);

  const unmapped = $derived(groups.filter((group) => !lmapByName[group.name]).length);

  function mappedRoles(groupName: string) {
    const lmap = lmapByName[groupName];
    if (!lmap) return [];

    return (lmap.role_ids ?? []).map((id) => roleNameByID[id] ?? id);
  }

  async function load() {
    loading = true;
    problem = "";

    try {
      const [lmapsRes, rolesRes] = await Promise.all([
        session.request<LMap[]>("lmaps?_limit=1000"),
        session.request<Role[]>("roles?_limit=1000"),
      ]);

      const maps: Record<string, LMap> = {};
      for (const lmap of lmapsRes.payload ?? []) maps[lmap.name] = lmap;
      lmapByName = maps;

      const names: Record<string, string> = {};
      for (const role of rolesRes.payload ?? []) names[role.id] = role.name;
      roleNameByID = names;

      try {
        const groupsRes = await session.request<LdapGroup[]>("ldap/groups");
        groups = groupsRes.payload ?? [];
        ldapConfigured = true;
      } catch (err) {
        // 424 is the directory answering "no enabled LDAP config", which is a
        // standing, not a fault — so this one call is read by status.
        if (statusOf(err) !== 424) throw err;

        ldapConfigured = false;
        groups = [];
      }
    } catch (err) {
      problem = messageOf(err, "The directory could not be read");
      groups = [];
    } finally {
      loading = false;
    }
  }

  /**
   * The register owns the sync: it posts once, reloads every list the sync
   * touches, and reports to the docket. This panel only re-reads the directory
   * afterwards so the group list matches what was just written.
   */
  async function syncNow() {
    syncing = true;
    problem = "";

    try {
      if (await registry.syncLdap(forceSync)) await load();
    } finally {
      syncing = false;
    }
  }

  onMount(load);
</script>

<Section
  title="Live LDAP groups"
  note="Read from the directory each time this page opens. Every sync gives each LDAP group a role of the same name, creating it when missing, and a map pointing at it. Members receive those roles as sync roles; a user who leaves every group has its sync roles cleared."
>
  {#snippet aside()}
    {#if !loading && ldapConfigured}
      <span class="stamp">
        {groups.length}
        {groups.length === 1 ? "group" : "groups"}{unmapped ? ` · ${unmapped} unmapped` : ""}
      </span>
    {/if}
    <button type="button" class="act" disabled={syncing || !ldapConfigured} onclick={() => void syncNow()}>
      {syncing ? "Syncing…" : forceSync ? "Run force sync" : "Run sync"}
    </button>
  {/snippet}

  <div class="max-w-[70ch]">
    <Switch
      label="Force: overwrite stored profiles from the directory"
      consequential
      disabled={syncing || !ldapConfigured}
      hint="A normal sync only creates new users and updates sync roles. Force re-fetches every existing user and overwrites the details held here — email, uid, name, given and family name, and aliases — with whatever LDAP currently says. Use it when directory profile fields changed."
      bind:checked={forceSync}
    />
  </div>

  {#if problem}
    <div class="mt-6 border border-seal/45 px-4 py-3">
      <p class="stamp text-seal">Not read</p>
      <p class="mt-1.5 max-w-[70ch] text-[13px] leading-[1.55] text-ink">{problem}</p>
      <button type="button" class="act mt-3" disabled={loading} onclick={() => void load()}>
        {loading ? "Retrying…" : "Try again"}
      </button>
    </div>
  {:else if loading}
    <p class="mt-6 border border-dashed border-rule px-6 py-12 text-center text-[13px] text-muted">
      Querying the directory…
    </p>
  {:else if !ldapConfigured}
    <div class="mt-6 border border-dashed border-rule px-6 py-12 text-center">
      <p class="text-[15px] font-semibold text-ink">No LDAP configuration is enabled</p>
      <p class="mx-auto mt-2 max-w-[56ch] text-[13px] leading-[1.6] text-muted">
        Add one under LDAP configs and enable it. Until then there is no directory to read, and the
        maps above are never filled by a sync.
      </p>
    </div>
  {:else if groups.length === 0}
    <div class="mt-6 border border-dashed border-rule px-6 py-12 text-center">
      <p class="text-[15px] font-semibold text-ink">The directory returned no groups</p>
      <p class="mx-auto mt-2 max-w-[56ch] text-[13px] leading-[1.6] text-muted">
        The connection worked, but the configured base DN and filter matched nothing. Check the
        groups block on the active LDAP config.
      </p>
    </div>
  {:else}
    <div class="mt-6 border border-rule bg-sheet">
      <div
        class="hidden items-baseline gap-4 border-b border-rule px-4 py-2 md:grid md:grid-cols-[minmax(0,1fr)_6rem_minmax(0,1.2fr)]"
      >
        <span class="stamp">Directory group</span>
        <span class="stamp text-right">Members</span>
        <span class="stamp">Mapped roles</span>
      </div>

      <ul>
        {#each groups as group (group.name)}
          <li class="border-b border-rule last:border-b-0">
            <div
              class="grid gap-x-4 gap-y-1.5 px-4 py-3 md:grid-cols-[minmax(0,1fr)_6rem_minmax(0,1.2fr)] md:items-center"
            >
              <div class="min-w-0">
                <p class="serial truncate text-[13.5px] font-medium text-ink">{group.name}</p>
                {#if group.description}
                  <p class="mt-0.5 truncate text-[12px] text-muted">{group.description}</p>
                {/if}
              </div>

              <p class="serial text-[13px] text-muted md:text-right">
                {(group.members ?? []).length}
              </p>

              <div class="min-w-0">
                {#if lmapByName[group.name]}
                  <p class="truncate text-[13px] text-ink">
                    {mappedRoles(group.name).join(", ") || "—"}
                  </p>
                {:else}
                  <span class="inline-flex items-center gap-2.5">
                    <Seal state="void" />
                    <span class="text-[12.5px] text-muted">
                      Not mapped — role and map are created on the next sync
                    </span>
                  </span>
                {/if}
              </div>
            </div>
          </li>
        {/each}
      </ul>
    </div>
  {/if}
</Section>
