<script lang="ts">
  import { onMount, untrack } from "svelte";

  import Instrument from "./ui/Instrument.svelte";
  import Section from "./ui/Section.svelte";
  import Seal from "./ui/Seal.svelte";
  import Switch from "./ui/Switch.svelte";
  import BreakSeal from "./ui/BreakSeal.svelte";
  import type { AnyRecord } from "../lib/api";
  import { formatStamp, joinValues, splitValues } from "../lib/records";
  import { docket, messageOf, session, statusOf } from "../lib/state/session.svelte";
  import { issuedSecrets } from "../lib/state/issued-secrets.svelte";
  import { runMutation } from "../lib/mutation";
  import { apiKeyRevisionHeaders } from "../lib/revisions";
  import {
    getSettingBool,
    getLiveSettingBool,
    setSettingBool,
    getSettingString,
    setSettingString,
    saveSetting,
    settings,
  } from "../lib/state/settings.svelte";

  /**
   * API keys are the only credential this console hands back in plaintext, and
   * it does so exactly once: the server keeps a hash. The whole page is built
   * around that one moment — everything else on it is a register.
   */

  type AccessRef = { id: string; name?: string };

  type Owner = {
    id: string;
    alias?: string[];
    details?: AnyRecord;
    roles?: AccessRef[];
    permissions?: AccessRef[];
    is_active?: boolean;
    service_account?: boolean;
  };

  type APIKeyMeta = {
    id: string;
    user_id: string;
    name: string;
    description: string;
    role_ids: string[];
    permission_ids: string[];
    details?: AnyRecord;
    disabled: boolean;
    revision: number;
    expires_at?: string;
    created_at: string;
    updated_at: string;
    last_used_at?: string;
    draft_name?: string;
    draft_description?: string;
    draft_email?: string;
    draft_role_ids?: string;
    draft_permission_ids?: string;
    draft_disabled?: boolean;
  };

  type View = "list" | "create" | "edit" | "settings";

  let owners = $state<Owner[]>([]);
  let keys = $state<APIKeyMeta[]>([]);
  let loadError = $state("");
  let surveyed = $state(false);
  let ownerDetailError = $state("");
  let editConflict = $state(false);
  let selectedOwnerID = $state("");
  let keyName = $state("");
  let keyDescription = $state("");
  let keyEmail = $state("");
  let expiresIn = $state("720h");
  let keyRoleIDs = $state("");
  let keyPermissionIDs = $state("");
  let view = $state<View>("list");
  let editKey = $state<APIKeyMeta | null>(null);
  let pendingRevoke = $state("");

  const presets = [
    { label: "24 hours", value: "24h" },
    { label: "7 days", value: "168h" },
    { label: "30 days", value: "720h" },
    { label: "90 days", value: "2160h" },
    { label: "No expiry", value: "" },
  ];

  const ownerByID = $derived(new Map(owners.map((owner) => [owner.id, owner])));
  const selectableOwners = $derived(owners.filter((owner) => owner.service_account));
  const selectedOwner = $derived(ownerByID.get(selectedOwnerID) ?? null);
  const apiKeysDisabled = $derived(getLiveSettingBool("api_key", ["disabled"]));
  const maxLifetime = $derived(String(settings.pathValue("api_key", ["max_lifetime"], true) ?? ""));

  /**
   * The owner index is read whole but light — names and aliases only. The
   * roles and permissions of the one owner being considered are read on
   * selection, so choosing from thousands stays cheap.
   */
  let ownerDetail = $state<Owner | null>(null);
  let ownerDetailLoading = $state(false);
  let ownerLoadSeq = 0;

  async function loadOwnerDetail(id: string) {
    const seq = ++ownerLoadSeq;
    ownerDetail = null;
    ownerDetailError = "";
    // Only the selection drives this read; reloading the owner index after an
    // unrelated write must not blank and refetch the detail.
    const owner = untrack(() => ownerByID.get(id));
    if (!owner) {
      ownerDetailLoading = false;
      return;
    }

    const base = owner.service_account ? "service-accounts" : "users";
    ownerDetailLoading = true;

    try {
      const res = await session.request<Owner>(
        `${base}/${encodeURIComponent(id)}?add_roles=true&add_permissions=true`,
      );
      if (seq === ownerLoadSeq && selectedOwnerID === id) ownerDetail = res.payload;
    } catch (err) {
      if (seq === ownerLoadSeq && selectedOwnerID === id) ownerDetailError = messageOf(err, "Cannot read the owner's access");
    } finally {
      if (seq === ownerLoadSeq && selectedOwnerID === id) ownerDetailLoading = false;
    }
  }

  $effect(() => {
    void loadOwnerDetail(selectedOwnerID);
  });

  async function copyText(value: string, what: string) {
    try {
      await navigator.clipboard.writeText(value);
      docket.commit(`${what} copied to the clipboard.`);
    } catch {
      docket.reject(
        "This browser did not allow the page to use the clipboard. Select the key text and copy it by hand — it will not be shown again.",
      );
    }
  }

  function decorateKeys(items: APIKeyMeta[]) {
    return items.map((key) => ({
      ...key,
      role_ids: key.role_ids ?? [],
      permission_ids: key.permission_ids ?? [],
      details: key.details ?? {},
      draft_name: key.name ?? "",
      draft_description: key.description ?? "",
      draft_email: typeof key.details?.email === "string" ? key.details.email : "",
      draft_role_ids: joinValues(key.role_ids),
      draft_permission_ids: joinValues(key.permission_ids),
      draft_disabled: Boolean(key.disabled),
    }));
  }

  function ownerLabel(owner: Owner | null | undefined) {
    if (!owner) return "Unknown owner";
    const aliases = owner.alias?.filter(Boolean).join(", ") || owner.id;
    const name = typeof owner.details?.name === "string" ? owner.details.name : "";
    const email = typeof owner.details?.email === "string" ? owner.details.email : "";
    const suffix = [name, email].filter(Boolean).join(" / ");

    return suffix ? `${aliases} — ${suffix}` : aliases;
  }

  function ownerLabelFromID(id: string) {
    if (!id) return "System key — no owner";
    return ownerLabel(ownerByID.get(id));
  }

  function accessIDs(owner: Owner | null, key: "roles" | "permissions") {
    const values = owner?.[key] ?? [];
    return (
      values
        .map((item) => item.id)
        .filter(Boolean)
        .join(", ") || "none"
    );
  }

  function keyStanding(key: APIKeyMeta): { label: string; state: "endorsed" | "broken" | "held" } {
    if (key.disabled) return { label: "Disabled", state: "held" };
    if (key.expires_at && new Date(key.expires_at).getTime() < Date.now()) {
      return { label: "Expired", state: "broken" };
    }

    return { label: "Active", state: "endorsed" };
  }

  function keyLabel(key: APIKeyMeta) {
    return key.name || key.id;
  }

  /** The bare read, so a write that reloads reports under its own wording. */
  async function fetchAll() {
    try {
      const [userOwners, serviceOwners, keyList] = await Promise.all([
        session.request<Owner[]>("users?add_roles=false&add_permissions=false&_limit=0"),
        session.request<Owner[]>("service-accounts?add_roles=false&add_permissions=false&_limit=0"),
        session.request<APIKeyMeta[]>("api-key-principals"),
      ]);

      owners = [...(userOwners.payload ?? []), ...(serviceOwners.payload ?? [])];
      keys = decorateKeys(keyList.payload ?? []);
      surveyed = true;
      loadError = "";
    } catch (err) {
      loadError = messageOf(err, "Cannot load API key principals");
      throw err;
    }
  }

  async function load() {
    await session.run(fetchAll, "Cannot load API key principals");
  }

  async function createKey() {
    if (session.busy || issuedSecrets.adminKey || !surveyed || loadError || apiKeysDisabled || ownerDetailLoading || ownerDetailError) return;
    const generation = issuedSecrets.generation;
    await runMutation(async () => {
      const res = await session.request<{ id: string; key: string; expires_at?: string }>(
        "api-key-principals",
        {
          method: "POST",
          body: JSON.stringify({
            user_id: selectedOwnerID,
            name: keyName.trim(),
            description: keyDescription.trim(),
            details: selectedOwnerID ? {} : { email: keyEmail.trim() },
            expires_in: expiresIn.trim(),
            role_ids: splitValues(keyRoleIDs),
            permission_ids: splitValues(keyPermissionIDs),
          }),
        },
      );

      if (generation !== issuedSecrets.generation) throw new Error("The account changed while the key was being issued; its secret was not exposed to the new account");
      issuedSecrets.adminKey = res.payload.key;
      keyName = "";
      keyDescription = "";
      keyEmail = "";
      keyRoleIDs = "";
      keyPermissionIDs = "";
      view = "list";
      pendingRevoke = "";
    }, fetchAll, "API key issued. Copy it now — it is not stored and cannot be shown again.", "API key create failed");
  }

  async function saveKey(key: APIKeyMeta) {
    if (session.busy || editConflict) return;
    const body: AnyRecord = {
      name: (key.draft_name ?? "").trim(),
      description: (key.draft_description ?? "").trim(),
      details: { ...(key.details ?? {}), email: (key.draft_email ?? "").trim() },
      disabled: Boolean(key.draft_disabled),
    };
    // Access is re-checked against the owner whenever it is sent, so only send
    // it when it changed; a rename must not fail on access the owner lost since.
    const roleIDs = splitValues(key.draft_role_ids ?? "");
    const permissionIDs = splitValues(key.draft_permission_ids ?? "");
    if (roleIDs.join("\n") !== (key.role_ids ?? []).join("\n")) body.role_ids = roleIDs;
    if (permissionIDs.join("\n") !== (key.permission_ids ?? []).join("\n")) body.permission_ids = permissionIDs;

    await runMutation(async () => {
      try {
        await session.request(`api-key-principals/${encodeURIComponent(key.id)}`, {
          method: "PATCH",
          headers: apiKeyRevisionHeaders(key.revision),
          body: JSON.stringify(body),
        });
      } catch (err) {
        if (statusOf(err) === 412) editConflict = true;
        throw err;
      }
      view = "list";
      editKey = null;
    }, fetchAll, "API key updated. The change applies to the next request that uses it.", "API key update failed");
  }

  async function revokeKey(id: string) {
    if (session.busy) return;
    pendingRevoke = "";

    const key = keys.find((key) => key.id === id);
    await runMutation(async () => {
      await session.request(`api-key-principals/${encodeURIComponent(id)}`, { method: "DELETE", headers: apiKeyRevisionHeaders(key?.revision) });
      keys = keys.filter((key) => key.id !== id);
      view = "list";
      editKey = null;
    }, fetchAll, "API key revoked. Anything still sending it is refused from now on.", "API key revoke failed");
  }

  function resetDraft(key: APIKeyMeta) {
    key.draft_name = key.name ?? "";
    key.draft_description = key.description ?? "";
    key.draft_email = typeof key.details?.email === "string" ? key.details.email : "";
    key.draft_role_ids = joinValues(key.role_ids);
    key.draft_permission_ids = joinValues(key.permission_ids);
    key.draft_disabled = Boolean(key.disabled);
  }

  function openCreate() {
    if (issuedSecrets.adminKey) { docket.reject("Store and acknowledge the displayed key before issuing another one"); return; }
    // Only explicit acknowledgment clears the tab's copy of an issued key.
    pendingRevoke = "";
    docket.clearRejections();
    view = "create";
  }

  function openEdit(key: APIKeyMeta) {
    editConflict = false;
    editKey = key;
    pendingRevoke = "";
    docket.clearRejections();
    view = "edit";
  }

  function openSettings() {
    pendingRevoke = "";
    docket.clearRejections();
    view = "settings";
  }

  function backToList() {
    if (view === "edit" && editKey) resetDraft(editKey);
    // The list reads committed settings; its draft is preserved independently.
    editKey = null;
    pendingRevoke = "";
    docket.clearRejections();
    view = "list";
  }

  onMount(() => {
    void load();
  });
</script>

{#snippet copyGlyph()}
  <svg width="13" height="13" viewBox="0 0 16 16" fill="none" aria-hidden="true">
    <rect x="5.6" y="5.6" width="8.6" height="8.6" stroke="currentColor" stroke-width="1.4" />
    <path
      d="M10.9 5.6V2.6a.8.8 0 0 0-.8-.8H2.6a.8.8 0 0 0-.8.8v7.5a.8.8 0 0 0 .8.8h3"
      stroke="currentColor"
      stroke-width="1.4"
      stroke-linecap="square"
    />
  </svg>
{/snippet}

<Instrument
  title="API keys"
  note="Static machine credentials — standalone system keys with their own access, or keys owned by a service account. A key is checked against the database on every request, so revoking or disabling one stops access immediately."
  wide
>
  {#snippet actions()}
    {#if view === "list"}
      <button
        type="button"
        class="act act-primary"
        disabled={session.busy || apiKeysDisabled || Boolean(issuedSecrets.adminKey) || !surveyed || Boolean(loadError)}
        onclick={openCreate}
      >
        Issue a key
      </button>
      <button type="button" class="act" disabled={session.busy} onclick={openSettings}>Settings</button>
      <button type="button" class="act" disabled={session.busy} onclick={() => void load()}>
        {session.busy ? "Reading…" : "Refresh"}
      </button>
    {:else if view === "create"}
      <button type="button" class="act" disabled={session.busy} onclick={backToList}>Cancel</button>
      <button
        type="button"
        class="act act-primary"
        disabled={session.busy || apiKeysDisabled || Boolean(issuedSecrets.adminKey) || !surveyed || Boolean(loadError) || ownerDetailLoading || Boolean(ownerDetailError)}
        onclick={() => void createKey()}
      >
        {session.busy ? "Issuing…" : "Issue key"}
      </button>
    {:else if view === "edit"}
      <button type="button" class="act" disabled={session.busy} onclick={backToList}>Discard</button>
      <button
        type="button"
        class="act act-primary"
        disabled={session.busy || editConflict}
        onclick={() => editKey && void saveKey(editKey)}
      >
        {session.busy ? "Committing…" : "Commit"}
      </button>
    {:else}
      <button type="button" class="act" disabled={session.busy} onclick={backToList}>Back</button>
      <button
        type="button"
        class="act act-primary"
        disabled={session.busy}
        onclick={() => void saveSetting("api_key")}
      >
        Commit
      </button>
    {/if}
  {/snippet}

  {#snippet custody()}
    <span class="stamp">{keys.length} {keys.length === 1 ? "key" : "keys"} on record</span>
    <span class="stamp">Hashes stored — never the key</span>
    {#if apiKeysDisabled}
      <Seal state="broken" label="Issuing and validation off" />
    {/if}
    <span class="serial stamp-raw">{session.apiBase}/api-key-principals</span>
  {/snippet}

  <!-- The one-time instrument. While it is on the sheet it is the page. -->
  {#if issuedSecrets.adminKey}
    <div class="guilloche stamp-in mb-12 max-w-[104ch] border-2 border-seal bg-sheet">
      <div class="hatch flex flex-wrap items-center gap-x-4 gap-y-1 border-b border-seal/40 px-5 py-3">
        <span class="stamp text-seal">Issued once</span>
        <span class="text-[13px] leading-[1.5] text-ink">
          This is the only time this key will ever be shown.
        </span>
      </div>

      <div class="px-5 py-7 sm:px-7">
        <p class="stamp">The key</p>
        <p
          class="serial mt-3 break-all text-[19px] font-semibold leading-[1.4] text-ink sm:text-[25px] sm:leading-[1.35]"
        >
          {issuedSecrets.adminKey}
        </p>

        <div class="mt-7 flex flex-wrap items-center gap-3">
          <button
            type="button"
            class="act act-primary"
            onclick={() => void copyText(issuedSecrets.adminKey, "API key")}
          >
            {@render copyGlyph()}
            Copy key
          </button>
          <button type="button" class="act" onclick={() => (issuedSecrets.adminKey = "")}>
            I have stored it
          </button>
        </div>

        <p class="mt-6 max-w-[70ch] text-[13px] leading-[1.6] text-muted">
          Only a hash of this key is kept, so nothing here or in the database can recover it. Once
          this panel is dismissed the only way forward is to issue a new key and update whatever was
          meant to use this one.
        </p>
      </div>
    </div>
  {/if}

  {#if loadError}
    <div class="mb-6 border border-dashed border-seal/60 px-5 py-4" role="alert">
      <p class="text-[13px] leading-[1.6] text-ink">{loadError}</p>
      <p class="mt-2 text-[12px] text-muted">{surveyed ? "The last successfully read list is kept below; it may be out of date." : "The key list could not be read. This does not mean there are no keys."}</p>
      <button type="button" class="act mt-3" disabled={session.busy} onclick={() => void load()}>Try again</button>
    </div>
  {/if}

  {#if view === "edit" && editConflict}
    <div class="mb-6 border border-dashed border-seal/60 px-5 py-4" role="alert">
      <p class="text-[13px] leading-[1.6] text-ink">This key changed on the server. Your draft is kept and nothing was overwritten. Copy any edits you want to keep, then discard this draft, refresh the list and reopen the key.</p>
    </div>
  {/if}

  {#if view === "list"}
    {#if apiKeysDisabled}
      <p class="hatch mb-8 max-w-[104ch] border border-seal/40 px-4 py-3 text-[13px] leading-[1.55] text-ink">
        <span class="stamp text-seal">Off</span>
        <span class="ml-3">
          API keys are switched off for this instance: none can be issued, and existing keys are
          refused at validation. Turn them back on under Settings.
        </span>
      </p>
    {/if}

    {#if !surveyed && !loadError}
      <p class="text-[13px] text-muted">Reading API keys…</p>
    {:else if !surveyed || (loadError && keys.length === 0)}
      <!-- The load error above is not an empty key registry. -->
    {:else if keys.length === 0}
      <div class="border border-dashed border-rule px-6 py-14 text-center">
        <p class="text-[15px] font-semibold text-ink">No API keys issued</p>
        <p class="mx-auto mt-2 max-w-[58ch] text-[13px] leading-[1.6] text-muted">
          A key gives a script, pipeline or service a way to call your API as a service account,
          without an interactive login.
        </p>
        <button
          type="button"
          class="act act-primary mt-6"
          disabled={session.busy || apiKeysDisabled || Boolean(issuedSecrets.adminKey) || Boolean(loadError)}
          onclick={openCreate}
        >
          Issue a key
        </button>
      </div>
    {:else}
      <div class="border border-rule bg-sheet">
        <div
          class="hidden items-baseline gap-4 border-b border-rule px-4 py-2 md:grid md:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_7rem_9rem_10rem]"
        >
          <span class="stamp">Name · Principal</span>
          <span class="stamp">Owner</span>
          <span class="stamp">Standing</span>
          <span class="stamp">Expires</span>
          <span class="stamp text-right">Instrument</span>
        </div>

        <ul>
          {#each keys as key (key.id)}
            {@const standing = keyStanding(key)}
            <li class="border-b border-rule last:border-b-0">
              <div
                class="grid gap-x-4 gap-y-1.5 px-4 py-3 transition-colors hover:bg-raised md:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_7rem_9rem_10rem] md:items-center"
              >
                <div class="min-w-0">
                  <p class="truncate text-[13.5px] font-medium text-ink">{keyLabel(key)}</p>
                  <p class="serial mt-0.5 truncate text-[12px] text-muted">api-key:{key.id}</p>
                  {#if key.description}
                    <p class="mt-1 truncate text-[12px] text-muted">{key.description}</p>
                  {/if}
                </div>

                <p class="min-w-0 truncate text-[12.5px] text-muted">
                  {ownerLabelFromID(key.user_id)}
                </p>

                <div class="min-w-0">
                  <Seal state={standing.state} label={standing.label} />
                </div>

                <p class="serial min-w-0 truncate text-[12px] text-muted">
                  {key.expires_at ? formatStamp(key.expires_at) : "Never"}
                </p>

                <div class="flex gap-2 md:justify-end">
                  <button
                    type="button"
                    class="act"
                    disabled={session.busy}
                    onclick={() => openEdit(key)}
                  >
                    Amend
                  </button>
                  <button
                    type="button"
                    class="act act-quiet text-seal hover:bg-seal/10 hover:text-seal"
                    aria-expanded={pendingRevoke === key.id}
                    onclick={() => (pendingRevoke = pendingRevoke === key.id ? "" : key.id)}
                  >
                    Revoke
                  </button>
                </div>
              </div>

              {#if pendingRevoke === key.id}
                <div class="px-4 pb-4">
                  <BreakSeal
                    consequence={`Revoking “${keyLabel(key)}” deletes it from PostgreSQL. Every script, pipeline or service still sending this key is refused on its very next request, and the key cannot be restored — you would have to issue a new one and update whatever used it.`}
                    action="Revoke this key"
                    disabled={session.busy}
                    onconfirm={() => void revokeKey(key.id)}
                  />
                </div>
              {/if}
            </li>
          {/each}
        </ul>
      </div>
    {/if}
  {:else if view === "create"}
   <div class="max-w-[104ch]">
    {#if apiKeysDisabled}
      <p class="hatch mb-8 border border-seal/40 px-4 py-3 text-[13px] leading-[1.55] text-ink">
        <span class="stamp text-seal">Off</span>
        <span class="ml-3">
          API keys are switched off for this instance, so nothing can be issued until they are turned
          back on under Settings.
        </span>
      </p>
    {/if}

    <Section
      title="Owner"
      note="A system key stands on its own and carries exactly the roles and permissions you list below. Choosing a service account instead makes its access the ceiling, and disabling it stops the key."
      first
    >
      <div class="max-w-md">
        <label class="stamp block" for="key-owner">Owner</label>
        <select id="key-owner" class="entry mt-1.5" bind:value={selectedOwnerID}>
          <option value="">System key — no owner</option>
          {#each selectableOwners as owner (owner.id)}
            <option value={owner.id}>{ownerLabel(owner)}</option>
          {/each}
        </select>
      </div>

      {#if selectedOwner}
        {#if ownerDetailLoading}
          <p class="mt-5 text-[12.5px] text-muted" role="status" aria-live="polite">
            Reading their access…
          </p>
        {:else if ownerDetailError}
          <div role="alert" class="text-[13px] text-seal">
            <p>{ownerDetailError}</p>
            <button type="button" class="act mt-2" disabled={session.busy} onclick={() => void loadOwnerDetail(selectedOwnerID)}>Read owner access again</button>
          </div>
        {:else if ownerDetail}
          <dl class="mt-5 max-w-[80ch] divide-y divide-rule border-y border-rule">
            <div class="grid gap-x-4 gap-y-0.5 py-2 sm:grid-cols-[9rem_minmax(0,1fr)]">
              <dt class="stamp sm:pt-[3px]">Their roles</dt>
              <dd class="serial min-w-0 break-all text-[12.5px] text-ink">
                {accessIDs(ownerDetail, "roles")}
              </dd>
            </div>
            <div class="grid gap-x-4 gap-y-0.5 py-2 sm:grid-cols-[9rem_minmax(0,1fr)]">
              <dt class="stamp sm:pt-[3px]">Their permissions</dt>
              <dd class="serial min-w-0 break-all text-[12.5px] text-ink">
                {accessIDs(ownerDetail, "permissions")}
              </dd>
            </div>
          </dl>
        {/if}
      {/if}
    </Section>

    <Section title="Identity and lifetime">
      <div class="grid gap-6 sm:grid-cols-2">
        <div class="min-w-0">
          <label class="stamp block" for="key-name">Name</label>
          <input
            id="key-name"
            class="entry mt-1.5"
            placeholder="ci-pipeline"
            autocomplete="off"
            bind:value={keyName}
          />
          <p class="mt-1.5 text-[12px] leading-[1.5] text-muted">
            How this key appears in the register. Name it after what will hold it.
          </p>
        </div>

        <div class="min-w-0 sm:col-span-2">
          <label class="stamp block" for="key-description">Description</label>
          <textarea
            id="key-description"
            class="entry mt-1.5 min-h-20 resize-y"
            placeholder="Used by the production deployment pipeline"
            bind:value={keyDescription}
          ></textarea>
          <p class="mt-1.5 text-[12px] leading-[1.5] text-muted">
            Optional context for operators reviewing this key later.
          </p>
        </div>

        <div class="min-w-0">
          <label class="stamp block" for="key-email">Email</label>
          <input
            id="key-email"
            class="entry mt-1.5"
            type="email"
            placeholder="robot@example.com"
            disabled={Boolean(selectedOwnerID)}
            autocomplete="off"
            bind:value={keyEmail}
          />
          <p class="mt-1.5 text-[12px] leading-[1.5] text-muted">
            {selectedOwnerID ? "Owned keys use the owner's account email; overrides are not accepted." : "Optional email claim for this administrator-managed system key."}
          </p>
        </div>

        <div class="min-w-0">
          <label class="stamp block" for="key-expires">Expires in</label>
          <input
            id="key-expires"
            class="entry serial mt-1.5"
            placeholder="720h — leave empty for no expiry"
            autocomplete="off"
            bind:value={expiresIn}
          />
          <div class="mt-3 flex flex-wrap gap-2">
            {#each presets as preset (preset.label)}
              <button
                type="button"
                class="act {expiresIn === preset.value ? 'act-primary' : ''}"
                aria-pressed={expiresIn === preset.value}
                onclick={() => (expiresIn = preset.value)}
              >
                {preset.label}
              </button>
            {/each}
          </div>
          {#if maxLifetime}
            <p class="mt-3 text-[12px] leading-[1.5] text-muted">
              This instance caps key lifetime at <span class="serial">{maxLifetime}</span>. A longer
              request is shortened to the cap rather than refused.
            </p>
          {/if}
        </div>
      </div>
    </Section>

    <Section
      title="Access"
      note={selectedOwnerID
        ? "Leave both empty and the key inherits whatever its owner has at the time of each request. Fill either one and the key carries only what you list here — each entry must be something the owner carries."
        : "A system key has no owner to inherit from, so it carries exactly what you list here. At least one role or permission is required."}
    >
      <div class="grid gap-6 sm:grid-cols-2">
        <div class="min-w-0">
          <label class="stamp block" for="key-roles">Role IDs</label>
          <input
            id="key-roles"
            class="entry serial mt-1.5"
            placeholder="role-id-a, role-id-b"
            autocomplete="off"
            bind:value={keyRoleIDs}
          />
        </div>

        <div class="min-w-0">
          <label class="stamp block" for="key-permissions">Permission IDs</label>
          <input
            id="key-permissions"
            class="entry serial mt-1.5"
            placeholder="perm-id-a, perm-id-b"
            autocomplete="off"
            bind:value={keyPermissionIDs}
          />
        </div>
      </div>
      <p class="mt-4 max-w-[70ch] text-[12px] leading-[1.5] text-muted">
        Separate IDs with commas or new lines.
      </p>
    </Section>
   </div>
  {:else if view === "edit" && editKey}
   <div class="max-w-[104ch]">
    <Section title="Key" first>
      <div class="grid gap-6 sm:grid-cols-2">
        <div class="min-w-0">
          <label class="stamp block" for="edit-key-name">Name</label>
          <input
            id="edit-key-name"
            class="entry mt-1.5"
            placeholder={editKey.id}
            autocomplete="off"
            bind:value={editKey.draft_name}
          />
        </div>

        <div class="min-w-0">
          <label class="stamp block" for="edit-key-email">Email</label>
          <input
            id="edit-key-email"
            class="entry mt-1.5"
            type="email"
            placeholder={editKey.user_id ? "Uses owner email" : "Optional system-key email"}
            disabled={Boolean(editKey.user_id)}
            autocomplete="off"
            bind:value={editKey.draft_email}
          />
          {#if editKey.user_id}
            <p class="mt-1.5 text-[12px] leading-[1.5] text-muted">The email claim comes from the owner, not key metadata.</p>
          {/if}
        </div>

        <div class="min-w-0 sm:col-span-2">
          <label class="stamp block" for="edit-key-description">Description</label>
          <textarea
            id="edit-key-description"
            class="entry mt-1.5 min-h-20 resize-y"
            placeholder="Where this key is used"
            bind:value={editKey.draft_description}
          ></textarea>
        </div>

        <div class="min-w-0">
          <label class="stamp block" for="edit-key-roles">Role IDs</label>
          <input
            id="edit-key-roles"
            class="entry serial mt-1.5"
            placeholder="role-id-a, role-id-b"
            autocomplete="off"
            bind:value={editKey.draft_role_ids}
          />
        </div>

        <div class="min-w-0">
          <label class="stamp block" for="edit-key-permissions">Permission IDs</label>
          <input
            id="edit-key-permissions"
            class="entry serial mt-1.5"
            placeholder="perm-id-a, perm-id-b"
            autocomplete="off"
            bind:value={editKey.draft_permission_ids}
          />
          <p class="mt-1.5 text-[12px] leading-[1.5] text-muted">
            {editKey.user_id
              ? "Empty on both means the key inherits its owner's access."
              : "A system key carries exactly these — it must keep at least one role or permission."}
          </p>
        </div>

        <div class="min-w-0 sm:pt-5">
          <Switch
            label="Suspend this key"
            consequential
            disabled={session.busy}
            hint="A suspended key is refused at validation but stays on the register, so you can let it back in later. Takes effect when you commit."
            bind:checked={
              () => editKey?.draft_disabled ?? false,
              (value: boolean) => {
                if (editKey) editKey.draft_disabled = value;
              }
            }
          />
        </div>
      </div>
    </Section>

    <Section title="Record">
      <dl class="divide-y divide-rule border-y border-rule">
        <div class="grid gap-x-4 gap-y-0.5 py-2.5 sm:grid-cols-[11rem_minmax(0,1fr)]">
          <dt class="stamp sm:pt-[3px]">Principal</dt>
          <dd class="serial min-w-0 break-all text-[13px] text-ink">api-key:{editKey.id}</dd>
        </div>
        <div class="grid gap-x-4 gap-y-0.5 py-2.5 sm:grid-cols-[11rem_minmax(0,1fr)]">
          <dt class="stamp sm:pt-[3px]">Owner</dt>
          <dd class="min-w-0 break-words text-[13px] text-ink">
            {ownerLabelFromID(editKey.user_id)}
          </dd>
        </div>
        <div class="grid gap-x-4 gap-y-0.5 py-2.5 sm:grid-cols-[11rem_minmax(0,1fr)]">
          <dt class="stamp sm:pt-[3px]">Revision</dt>
          <dd class="serial min-w-0 text-[13px] text-ink">{editKey.revision}</dd>
        </div>
        <div class="grid gap-x-4 gap-y-0.5 py-2.5 sm:grid-cols-[11rem_minmax(0,1fr)]">
          <dt class="stamp sm:pt-[3px]">Created</dt>
          <dd class="serial min-w-0 text-[13px] text-ink">
            {formatStamp(editKey.created_at) || editKey.created_at || "—"}
          </dd>
        </div>
        <div class="grid gap-x-4 gap-y-0.5 py-2.5 sm:grid-cols-[11rem_minmax(0,1fr)]">
          <dt class="stamp sm:pt-[3px]">Amended</dt>
          <dd class="serial min-w-0 text-[13px] text-ink">
            {formatStamp(editKey.updated_at) || editKey.updated_at || "—"}
          </dd>
        </div>
        <div class="grid gap-x-4 gap-y-0.5 py-2.5 sm:grid-cols-[11rem_minmax(0,1fr)]">
          <dt class="stamp sm:pt-[3px]">Expires</dt>
          <dd class="serial min-w-0 text-[13px] text-ink">
            {editKey.expires_at ? formatStamp(editKey.expires_at) : "Never"}
          </dd>
        </div>
        <div class="grid gap-x-4 gap-y-0.5 py-2.5 sm:grid-cols-[11rem_minmax(0,1fr)]">
          <dt class="stamp sm:pt-[3px]">Last used</dt>
          <dd class="serial min-w-0 text-[13px] text-ink">
            {editKey.last_used_at ? formatStamp(editKey.last_used_at) : "Never used"}
          </dd>
        </div>
      </dl>
    </Section>

    <Section title="Revoke" note="Suspending is reversible. Revoking is not.">
      <BreakSeal
        consequence={`Revoking “${keyLabel(editKey)}” deletes it from PostgreSQL. Every script, pipeline or service still sending this key is refused on its very next request, and the key cannot be restored — you would have to issue a new one and update whatever used it.`}
        action="Revoke this key"
        disabled={session.busy}
        onconfirm={() => editKey && void revokeKey(editKey.id)}
      />
    </Section>
   </div>
  {:else if view === "settings"}
   <div class="max-w-[104ch]">
    <Section title="Availability" first>
      <Switch
        label="Switch API keys off for this instance"
        consequential
        disabled={session.busy}
        hint="No key can be issued while this is on, and every key already in use is refused at validation. Anything authenticating with X-API-Key stops working as soon as you commit."
        bind:checked={
          () => getSettingBool("api_key", ["disabled"]),
          (value: boolean) => setSettingBool("api_key", ["disabled"], value)
        }
      />

      <div class="mt-6">
        <Switch
          label="Let people issue their own keys"
          disabled={session.busy}
          hint="Everyone with a signed-in session gets a “Personal access keys” panel on their account page. A key they issue there carries only their own roles and permissions — never more. Off means only administrators can issue keys."
          bind:checked={
            () => getSettingBool("api_key", ["self_service"]),
            (value: boolean) => setSettingBool("api_key", ["self_service"], value)
          }
        />
      </div>
    </Section>

    <Section title="Lifetime cap">
      <div class="max-w-sm">
        <label class="stamp block" for="api-key-max-lifetime">Maximum lifetime</label>
        <input
          id="api-key-max-lifetime"
          class="entry serial mt-1.5"
          placeholder="720h — empty means no cap"
          autocomplete="off"
          value={maxLifetime}
          oninput={(e) => setSettingString("api_key", ["max_lifetime"], e.currentTarget.value)}
        />
        <p class="mt-1.5 text-[12px] leading-[1.5] text-muted">
          A creation request asking for longer than this is shortened to the cap, not refused.
        </p>
      </div>
    </Section>

    <Section
      title="Using a key"
      note="The key is a static credential sent on every request — no token exchange, no refresh."
    >
      <div class="grid gap-8 lg:grid-cols-2">
        <div class="min-w-0">
          <p class="stamp stamp-ink">Calling your API directly</p>
          <pre class="exhibit mt-3 overflow-auto">curl https://app.example.com/api \
  -H 'X-API-Key: tak_...'</pre>
          <p class="mt-3 max-w-[62ch] text-[12.5px] leading-[1.55] text-muted">
            Validated against the database on each call, so revoking or suspending the key stops
            access on the next request rather than when a token expires.
          </p>
        </div>

        <div class="min-w-0">
          <p class="stamp stamp-ink">Behind the session middleware</p>
          <pre class="exhibit mt-3 overflow-auto"># session provider config
api_key: true            # accept X-API-Key
# remote auth instance:
# oauth2:
#   api_key_url: {session.oauthIssuer}/api-key</pre>
          <p class="mt-3 max-w-[62ch] text-[12.5px] leading-[1.55] text-muted">
            Session checks <span class="serial">X-API-Key</span> on each request, strips the raw key
            header, and forwards <span class="serial">X-User: api-key:&lt;id&gt;</span> carrying the
            key's own roles and permissions.
          </p>
        </div>
      </div>
    </Section>
   </div>
  {/if}
</Instrument>
