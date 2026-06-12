<script lang="ts">
  import { onMount } from "svelte";
  import type { AnyRecord, SettingNamespace } from "../lib/api";

  export let apiBase = "/auth/v1";
  export let busy = false;
  export let settingsRevision = 0;
  export let getSettingBool: (namespace: SettingNamespace, path: string[], fallback?: boolean) => boolean = () => false;
  export let setSettingBool: (namespace: SettingNamespace, path: string[], value: boolean) => void = () => {};
  export let getSettingString: (namespace: SettingNamespace, path: string[]) => string = () => "";
  export let setSettingString: (namespace: SettingNamespace, path: string[], value: string) => void = () => {};
  export let saveSetting: (namespace: SettingNamespace) => void | Promise<void> = () => {};

  type AccessRef = { id: string; name?: string };
  type Owner = {
    id: string;
    alias?: string[];
    details?: AnyRecord;
    roles?: AccessRef[];
    permissions?: AccessRef[];
    is_active?: boolean;
  };
  type APIKeyMeta = {
    id: string;
    user_id: string;
    name: string;
    role_ids: string[];
    permission_ids: string[];
    disabled: boolean;
    revision: number;
    expires_at?: string;
    created_at: string;
    updated_at: string;
    last_used_at?: string;
    draft_name?: string;
    draft_role_ids?: string;
    draft_permission_ids?: string;
  };
  type ApiResponse<T> = { payload: T; message?: { text?: string; error?: string } };

  let owners: Owner[] = [];
  let keys: APIKeyMeta[] = [];
  let apiBusy = false;
  let error = "";
  let notice = "";
  let createdKey = "";
  let selectedOwnerID = "";
  let keyName = "";
  let expiresIn = "720h";
  let keyRoleIDs = "";
  let keyPermissionIDs = "";

  type View = "list" | "create" | "edit" | "settings";
  let view: View = "list";
  let editKey: APIKeyMeta | null = null;

  const presets = [
    { label: "24H", value: "24h" },
    { label: "7D", value: "168h" },
    { label: "30D", value: "720h" },
    { label: "90D", value: "2160h" },
    { label: "NO EXPIRY", value: "" },
  ];

  $: working = busy || apiBusy;
  $: oauthBase = apiBase.replace(/\/v1$/, "");
  $: ownerByID = new Map(owners.map((owner) => [owner.id, owner]));
  $: selectedOwner = ownerByID.get(selectedOwnerID) ?? null;
  $: apiKeysDisabled = settingBool(settingsRevision, ["disabled"]);
  $: maxLifetime = settingString(settingsRevision, ["max_lifetime"]);

  function flash(message: string) {
    notice = message;
    error = "";
    window.setTimeout(() => {
      if (notice === message) notice = "";
    }, 4000);
  }

  function fail(err: unknown, fallback: string) {
    error = err instanceof Error ? err.message : fallback;
    notice = "";
  }

  function splitValues(value: string) {
    return value
      .split(/[\n,]+/)
      .map((item) => item.trim())
      .filter(Boolean);
  }

  function joinValues(values: string[] | undefined) {
    return (values ?? []).join(", ");
  }

  function decorateKeys(items: APIKeyMeta[]) {
    return items.map((key) => ({
      ...key,
      role_ids: key.role_ids ?? [],
      permission_ids: key.permission_ids ?? [],
      draft_name: key.name ?? "",
      draft_role_ids: joinValues(key.role_ids),
      draft_permission_ids: joinValues(key.permission_ids),
    }));
  }

  function inputValue(event: Event) {
    return (event.currentTarget as HTMLInputElement | HTMLSelectElement).value;
  }

  function checkedValue(event: Event) {
    return (event.currentTarget as HTMLInputElement).checked;
  }

  function settingBool(_revision: number, path: string[], fallback = false) {
    return getSettingBool("api_key", path, fallback);
  }

  function settingString(_revision: number, path: string[]) {
    return getSettingString("api_key", path);
  }

  function checkboxClass(checked: boolean, danger = false) {
    const base = "h-3.5 w-3.5 appearance-none border bg-crt";
    if (danger) return `${base} border-line checked:bg-alert`;

    return `${base} border-line checked:bg-fg ${checked ? "" : "border-alert"}`;
  }

  function ownerLabel(owner: Owner | null | undefined) {
    if (!owner) return "UNKNOWN OWNER";
    const aliases = owner.alias?.filter(Boolean).join(", ") || owner.id;
    const name = typeof owner.details?.name === "string" ? owner.details.name : "";
    const email = typeof owner.details?.email === "string" ? owner.details.email : "";
    const suffix = [name, email].filter(Boolean).join(" / ");

    return suffix ? `${aliases} - ${suffix}` : aliases;
  }

  function ownerLabelFromID(id: string) {
    return ownerLabel(ownerByID.get(id));
  }

  function accessIDs(owner: Owner | null, key: "roles" | "permissions") {
    const values = owner?.[key] ?? [];
    return values.map((item) => item.id).filter(Boolean).join(", ") || "none";
  }

  async function api<T>(path: string, init?: RequestInit): Promise<T> {
    const res = await fetch(`${apiBase}/${path}`, {
      headers: { "Content-Type": "application/json", ...(init?.headers ?? {}) },
      ...init,
    });

    let body: AnyRecord = {};
    try {
      body = await res.json();
    } catch {
      // ignore empty bodies
    }

    if (!res.ok) {
      throw new Error(String(body.message ?? body.error ?? res.statusText));
    }

    return (body as ApiResponse<T>).payload;
  }

  async function load() {
    apiBusy = true;
    error = "";
    try {
      const [userOwners, serviceOwners, keyList] = await Promise.all([
        api<Owner[]>("users?add_roles=true&add_permissions=true&_limit=500"),
        api<Owner[]>("service-accounts?add_roles=true&add_permissions=true&_limit=500"),
        api<APIKeyMeta[]>("api-key-principals"),
      ]);
      owners = [...(userOwners ?? []), ...(serviceOwners ?? [])];
      keys = decorateKeys(keyList ?? []);
      if (!selectedOwnerID && owners[0]) selectedOwnerID = owners[0].id;
    } catch (err) {
      owners = [];
      keys = [];
      fail(err, "CANNOT LOAD API KEY PRINCIPALS");
    } finally {
      apiBusy = false;
    }
  }

  async function createKey() {
    if (!selectedOwnerID) {
      error = "OWNER IS REQUIRED";
      return;
    }

    apiBusy = true;
    error = "";
    try {
      const payload = await api<{ id: string; key: string; expires_at?: string }>("api-key-principals", {
        method: "POST",
        body: JSON.stringify({
          user_id: selectedOwnerID,
          name: keyName.trim(),
          expires_in: expiresIn.trim(),
          role_ids: splitValues(keyRoleIDs),
          permission_ids: splitValues(keyPermissionIDs),
        }),
      });

      createdKey = payload.key;
      keyName = "";
      keyRoleIDs = "";
      keyPermissionIDs = "";
      await load();
      view = "list";
      flash("API KEY CREATED - COPY IT NOW, IT IS SHOWN ONCE");
    } catch (err) {
      fail(err, "API KEY CREATE FAILED");
    } finally {
      apiBusy = false;
    }
  }

  async function saveKey(key: APIKeyMeta) {
    apiBusy = true;
    error = "";
    try {
      await api(`api-key-principals/${encodeURIComponent(key.id)}`, {
        method: "PATCH",
        body: JSON.stringify({
          name: (key.draft_name ?? "").trim(),
          role_ids: splitValues(key.draft_role_ids ?? ""),
          permission_ids: splitValues(key.draft_permission_ids ?? ""),
          disabled: key.disabled,
        }),
      });
      await load();
      view = "list";
      editKey = null;
      flash("API KEY UPDATED - CHANGES APPLY IMMEDIATELY");
    } catch (err) {
      fail(err, "API KEY UPDATE FAILED");
    } finally {
      apiBusy = false;
    }
  }

  async function revokeKey(id: string) {
    if (!confirm("REVOKE API KEY?")) return;

    apiBusy = true;
    error = "";
    try {
      await api(`api-key-principals/${encodeURIComponent(id)}`, { method: "DELETE" });
      await load();
      view = "list";
      editKey = null;
      flash("API KEY REVOKED");
    } catch (err) {
      fail(err, "API KEY REVOKE FAILED");
    } finally {
      apiBusy = false;
    }
  }

  async function copyText(value: string) {
    try {
      await navigator.clipboard.writeText(value);
      flash("COPIED TO CLIPBOARD");
    } catch {
      error = "CLIPBOARD UNAVAILABLE";
    }
  }

  function keyStatus(key: APIKeyMeta): { label: string; ok: boolean } {
    if (key.disabled) return { label: "DISABLED", ok: false };
    if (key.expires_at && new Date(key.expires_at).getTime() < Date.now()) return { label: "EXPIRED", ok: false };
    return { label: "ACTIVE", ok: true };
  }

  function resetDraft(key: APIKeyMeta) {
    key.draft_name = key.name ?? "";
    key.draft_role_ids = joinValues(key.role_ids);
    key.draft_permission_ids = joinValues(key.permission_ids);
    keys = keys;
  }

  function openCreate() {
    createdKey = "";
    error = "";
    view = "create";
  }

  function openEdit(key: APIKeyMeta) {
    editKey = key;
    error = "";
    view = "edit";
  }

  function openSettings() {
    error = "";
    view = "settings";
  }

  function backToList() {
    if (view === "edit" && editKey) resetDraft(editKey);
    editKey = null;
    error = "";
    view = "list";
  }

  onMount(() => {
    void load();
  });
</script>

<div class="grid gap-px bg-line p-px">
  {#if error}
    <div class="flex items-center gap-3 bg-panel px-4 py-2">
      <span class="bg-alert px-2 py-0.5 text-[10px] font-bold uppercase tracking-[0.15em] text-white">FAULT</span>
      <span class="text-xs uppercase tracking-[0.05em] text-alert">{error}</span>
    </div>
  {/if}
  {#if notice}
    <div class="flex items-center gap-3 bg-panel px-4 py-2">
      <span class="bg-fg px-2 py-0.5 text-[10px] font-bold uppercase tracking-[0.15em] text-crt">OK</span>
      <span class="text-xs uppercase tracking-[0.05em]">{notice}</span>
    </div>
  {/if}

  {#if view === "list"}
    <div class="grid gap-3 bg-panel p-4">
      <div class="flex flex-wrap items-start justify-between gap-3">
        <div>
          <span class="t-label text-fg">[ API KEYS ]</span>
          <h3 class="mt-2 font-display text-3xl uppercase leading-none tracking-tight md:text-4xl">Machine Principals</h3>
        </div>
        <div class="flex flex-wrap gap-px">
          <button class="btn-t-solid" disabled={working || apiKeysDisabled} on:click={openCreate}>[+] NEW API KEY</button>
          <button class="btn-t border-0 bg-crt" disabled={working} on:click={openSettings}>[ SETTINGS ]</button>
          <button class="btn-t border-0 bg-crt" disabled={working} on:click={load}>[ REFRESH ]</button>
        </div>
      </div>
      <p class="max-w-3xl text-xs leading-5 text-dim">
        API keys are static machine credentials owned by a user or service account. They carry their own role and permission IDs, are validated against the database on every request, and stop working immediately when revoked or disabled.
      </p>
      {#if apiKeysDisabled}
        <p class="t-label text-alert">/// API KEY CREATION AND VALIDATION ARE DISABLED — ENABLE THEM IN [ SETTINGS ]</p>
      {/if}
    </div>

    {#if createdKey}
      <div class="grid gap-2 border-l-2 border-alert bg-panel p-4">
        <span class="t-label text-alert">NEW KEY — SHOWN ONCE, COPY IT NOW</span>
        <p class="break-all text-[12px] font-bold text-fg">{createdKey}</p>
        <div class="flex flex-wrap gap-2">
          <button class="btn-t-solid" on:click={() => copyText(createdKey)}>[ COPY KEY ]</button>
          <button class="btn-t" on:click={() => (createdKey = "")}>DISMISS</button>
        </div>
      </div>
    {/if}

    <div class="bg-panel">
      <div class="flex items-center justify-between border-b border-line px-4 py-2">
        <span class="t-label text-fg">[ KEY PRINCIPALS ] / REC.COUNT {String(keys.length).padStart(3, "0")}</span>
        <span class="t-label">HASHES ONLY STORED</span>
      </div>

      {#if keys.length === 0}
        <div class="grid min-h-48 place-items-center p-8 text-center">
          <div>
            <p class="text-sm font-bold uppercase tracking-[0.2em]">/// NO API KEYS ///</p>
            <p class="t-label mt-3">PRESS [+] NEW API KEY TO ISSUE A MACHINE CREDENTIAL</p>
          </div>
        </div>
      {:else}
        <div class="hidden grid-cols-[1fr,1fr,110px,130px] gap-4 border-b border-line px-4 py-2 md:grid">
          <span class="t-label text-fg">NAME / PRINCIPAL</span>
          <span class="t-label text-fg">OWNER</span>
          <span class="t-label text-fg">STATUS</span>
          <span class="t-label text-right text-fg">ACTIONS</span>
        </div>
        <div class="divide-y divide-line">
          {#each keys as key, index}
            {@const status = keyStatus(key)}
            <div class="grid gap-2 px-4 py-3 md:grid-cols-[1fr,1fr,110px,130px] md:items-center md:gap-4">
              <div class="min-w-0">
                <p class="truncate text-sm font-bold text-fg">
                  <span class="mr-2 text-[10px] font-medium text-dim">{String(index + 1).padStart(2, "0")}</span>{key.name || key.id}
                </p>
                <p class="mt-0.5 truncate pl-6 text-[11px] text-dim">api-key:{key.id}</p>
              </div>
              <p class="min-w-0 truncate text-[11px] text-dim">{ownerLabelFromID(key.user_id)}</p>
              <div>
                {#if status.ok}
                  <span class="text-[11px] font-bold uppercase tracking-[0.1em] text-fg">[ {status.label} ]</span>
                {:else}
                  <span class="text-[11px] font-bold uppercase tracking-[0.1em] text-alert">[ {status.label} ]</span>
                {/if}
              </div>
              <div class="flex gap-px md:justify-end">
                <button
                  class="border border-line px-3 py-1 text-[11px] font-bold uppercase tracking-[0.1em] text-fg hover:bg-fg hover:text-crt"
                  disabled={working}
                  on:click={() => openEdit(key)}
                >
                  EDIT
                </button>
                <button
                  class="border border-line px-3 py-1 text-[11px] font-bold uppercase tracking-[0.1em] text-alert hover:bg-alert hover:text-white"
                  disabled={working}
                  on:click={() => revokeKey(key.id)}
                >
                  REVOKE
                </button>
              </div>
            </div>
          {/each}
        </div>
      {/if}
    </div>
  {:else if view === "create"}
    <div class="bg-panel">
      <div class="flex flex-wrap items-center justify-between gap-3 border-b border-line px-4 py-2">
        <div class="flex items-center gap-3">
          <button class="btn-t border-0 bg-crt" disabled={working} on:click={backToList}>[ &lt; BACK ]</button>
          <span class="t-label text-fg">NEW API KEY / <span class="text-alert">DRAFT</span></span>
        </div>
        <button class="btn-t-solid" disabled={working || !selectedOwnerID || apiKeysDisabled} on:click={createKey}>[ CREATE API KEY ]</button>
      </div>

      {#if apiKeysDisabled}
        <p class="border-b border-line bg-panel px-4 py-2 text-[11px] font-bold uppercase tracking-[0.12em] text-alert">
          API KEY CREATION IS DISABLED — ENABLE IT IN SETTINGS FIRST
        </p>
      {/if}

      <div class="grid gap-px bg-line p-px">
        <label class="grid gap-1 bg-panel p-3">
          <span class="t-label">OWNER</span>
          <select bind:value={selectedOwnerID} class="field-t">
            <option value="">select owner</option>
            {#each owners as owner}
              <option value={owner.id}>{ownerLabel(owner)}</option>
            {/each}
          </select>
          {#if selectedOwner}
            <span class="text-[10px] leading-4 text-dim">ROLES: {accessIDs(selectedOwner, "roles")} / PERMISSIONS: {accessIDs(selectedOwner, "permissions")}</span>
          {/if}
        </label>
        <label class="grid gap-1 bg-panel p-3">
          <span class="t-label">KEY NAME</span>
          <input bind:value={keyName} class="field-t" placeholder="ci-pipeline" />
        </label>
        <label class="grid gap-1 bg-panel p-3">
          <span class="t-label">LIFETIME / EXPIRES IN</span>
          <input bind:value={expiresIn} class="field-t" placeholder="720h; empty = no expiry" />
          <div class="mt-1 flex flex-wrap gap-px">
            {#each presets as preset}
              <button
                class={`border px-2.5 py-1 text-[10px] font-bold uppercase tracking-[0.1em] ${expiresIn === preset.value ? "border-alert bg-alert text-white" : "border-line text-dim hover:text-fg"}`}
                on:click={() => (expiresIn = preset.value)}
              >
                {preset.label}
              </button>
            {/each}
          </div>
          {#if maxLifetime}
            <span class="text-[10px] leading-4 text-dim">MAX LIFETIME CAP: {maxLifetime} — LONGER REQUESTS ARE SHORTENED</span>
          {/if}
        </label>
        <label class="grid gap-1 bg-panel p-3">
          <span class="t-label">ROLE IDS</span>
          <input bind:value={keyRoleIDs} class="field-t" placeholder="role-id-a, role-id-b" />
        </label>
        <label class="grid gap-1 bg-panel p-3">
          <span class="t-label">PERMISSION IDS</span>
          <input bind:value={keyPermissionIDs} class="field-t" placeholder="perm-id-a, perm-id-b" />
          <span class="text-[10px] leading-4 text-dim">Leave role/permission IDs empty to inherit the owner's access.</span>
        </label>
      </div>
    </div>
  {:else if view === "edit" && editKey}
    <div class="bg-panel">
      <div class="flex flex-wrap items-center justify-between gap-3 border-b border-line px-4 py-2">
        <div class="flex items-center gap-3">
          <button class="btn-t border-0 bg-crt" disabled={working} on:click={backToList}>[ &lt; BACK ]</button>
          <span class="t-label text-fg">EDIT KEY / <span class="text-dim">{editKey.id}</span></span>
        </div>
        <div class="flex flex-wrap gap-px">
          <button class="btn-t-solid" disabled={working} on:click={() => editKey && saveKey(editKey)}>[ SAVE ]</button>
          <button
            class="border border-line px-3 py-1 text-[11px] font-bold uppercase tracking-[0.1em] text-alert hover:bg-alert hover:text-white"
            disabled={working}
            on:click={() => editKey && revokeKey(editKey.id)}
          >
            REVOKE
          </button>
        </div>
      </div>

      <div class="grid gap-px bg-line p-px">
        <label class="grid gap-1 bg-panel p-3">
          <span class="t-label">NAME</span>
          <input bind:value={editKey.draft_name} class="field-t" placeholder={editKey.id} />
        </label>
        <label class="grid gap-1 bg-panel p-3">
          <span class="t-label">ROLE IDS</span>
          <input bind:value={editKey.draft_role_ids} class="field-t" placeholder="role-id-a, role-id-b" />
        </label>
        <label class="grid gap-1 bg-panel p-3">
          <span class="t-label">PERMISSION IDS</span>
          <input bind:value={editKey.draft_permission_ids} class="field-t" placeholder="perm-id-a, perm-id-b" />
        </label>
        <label class="flex items-center gap-3 bg-panel p-3 text-xs font-bold uppercase tracking-[0.15em]">
          <input bind:checked={editKey.disabled} type="checkbox" class={checkboxClass(!editKey.disabled, true)} />
          <span class={editKey.disabled ? "text-alert" : "text-dim"}>{editKey.disabled ? "DISABLED" : "ENABLED"}</span>
        </label>
        <div class="bg-panel p-3 text-[11px] leading-5 text-dim">
          <p class="break-all">
            OWNER {ownerLabelFromID(editKey.user_id)} / PRINCIPAL api-key:{editKey.id} / REV {editKey.revision}
          </p>
          <p class="break-all">
            CREATED {editKey.created_at} / UPDATED {editKey.updated_at}
            {#if editKey.expires_at} / EXPIRES {editKey.expires_at}{:else} / NO EXPIRY{/if}
            {#if editKey.last_used_at} / LAST USED {editKey.last_used_at}{/if}
          </p>
        </div>
      </div>
    </div>
  {:else if view === "settings"}
    <div class="bg-panel">
      <div class="flex flex-wrap items-center justify-between gap-3 border-b border-line px-4 py-2">
        <div class="flex items-center gap-3">
          <button class="btn-t border-0 bg-crt" disabled={working} on:click={backToList}>[ &lt; BACK ]</button>
          <span class="t-label text-fg">API KEY SETTINGS</span>
        </div>
        <button class="btn-t-solid" disabled={working} on:click={() => saveSetting("api_key")}>[ SAVE SETTINGS ]</button>
      </div>

      <div class="grid gap-px bg-line p-px">
        <label class="flex items-center gap-3 bg-panel p-3 text-xs font-bold uppercase tracking-[0.15em]">
          <input type="checkbox" checked={apiKeysDisabled} class={checkboxClass(apiKeysDisabled, true)} on:change={(event) => setSettingBool("api_key", ["disabled"], checkedValue(event))} />
          <span class={apiKeysDisabled ? "text-alert" : "text-dim"}>DISABLE API KEY CREATION AND VALIDATION</span>
        </label>
        <label class="grid gap-1 bg-panel p-3">
          <span class="t-label">MAX LIFETIME</span>
          <input class="field-t" value={maxLifetime} placeholder="empty = no cap, e.g. 720h" on:input={(event) => setSettingString("api_key", ["max_lifetime"], inputValue(event))} />
          <span class="text-[10px] leading-4 text-dim">Creation requests longer than this cap are shortened automatically.</span>
        </label>
      </div>
    </div>

    <div class="grid gap-px bg-line lg:grid-cols-2">
      <div class="bg-panel p-4">
        <span class="t-label text-fg">[ DIRECT USAGE ]</span>
        <pre class="mt-3 overflow-auto border border-line bg-crt p-3 text-[11px] leading-5 text-fg">curl https://app.example.com/api \
  -H 'X-API-Key: tak_...'</pre>
        <p class="mt-3 text-[11px] leading-4 text-dim">
          The key is a static credential sent on every request. No token exchange, no refresh dance; revoke or disable the key and access stops immediately.
        </p>
      </div>
      <div class="bg-panel p-4">
        <span class="t-label text-fg">[ SESSION INTEGRATION ]</span>
        <pre class="mt-3 overflow-auto border border-line bg-crt p-3 text-[11px] leading-5 text-fg"># session provider config
api_key: true            # accept X-API-Key
# remote auth instance:
# oauth2:
#   api_key_url: {oauthBase}/oauth2/api-key</pre>
        <p class="mt-3 text-[11px] leading-4 text-dim">
          Session validates <span class="text-fg">X-API-Key</span> against the database on each request, deletes the raw key header, and forwards <span class="text-fg">X-User: api-key:&lt;id&gt;</span> with the key's own roles/permissions.
        </p>
      </div>
    </div>
  {/if}
</div>
