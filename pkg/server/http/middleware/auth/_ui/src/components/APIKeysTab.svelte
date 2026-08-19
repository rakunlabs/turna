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
    { label: "No expiry", value: "" },
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
    if (!owner) return "Unknown owner";
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
      fail(err, "Cannot load API key principals");
    } finally {
      apiBusy = false;
    }
  }

  async function createKey() {
    if (!selectedOwnerID) {
      error = "Owner is required";
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
      flash("API key created - copy it now, it is shown once");
    } catch (err) {
      fail(err, "API key create failed");
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
      flash("API key updated - changes apply immediately");
    } catch (err) {
      fail(err, "API key update failed");
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
      flash("API key revoked");
    } catch (err) {
      fail(err, "API key revoke failed");
    } finally {
      apiBusy = false;
    }
  }

  async function copyText(value: string) {
    try {
      await navigator.clipboard.writeText(value);
      flash("Copied to clipboard");
    } catch {
      error = "Clipboard unavailable";
    }
  }

  function keyStatus(key: APIKeyMeta): { label: string; ok: boolean } {
    if (key.disabled) return { label: "Disabled", ok: false };
    if (key.expires_at && new Date(key.expires_at).getTime() < Date.now()) return { label: "Expired", ok: false };
    return { label: "Active", ok: true };
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
      <span class="bg-alert px-2 py-0.5 text-xs font-bold text-white">Fault</span>
      <span class="text-xs text-alert">{error}</span>
    </div>
  {/if}
  {#if notice}
    <div class="flex items-center gap-3 bg-panel px-4 py-2">
      <span class="bg-fg px-2 py-0.5 text-xs font-bold text-crt">Ok</span>
      <span class="text-xs">{notice}</span>
    </div>
  {/if}

  {#if view === "list"}
    <div class="grid gap-3 bg-panel p-4">
      <div class="flex flex-wrap items-start justify-between gap-3">
        <div>
          <span class="t-label text-fg">API keys</span>
          <h3 class="mt-2 font-display text-3xl leading-none tracking-tight md:text-4xl">Machine Principals</h3>
        </div>
        <div class="flex flex-wrap gap-px">
          <button class="btn-t-solid" disabled={working || apiKeysDisabled} on:click={openCreate}>+ NEW API KEY</button>
          <button class="btn-t border-0 bg-crt" disabled={working} on:click={openSettings}>Settings</button>
          <button class="btn-t border-0 bg-crt" disabled={working} on:click={load}>Refresh</button>
        </div>
      </div>
      <p class="max-w-3xl text-xs leading-5 text-dim">
        API keys are static machine credentials owned by a user or service account. They carry their own role and permission IDs, are validated against the database on every request, and stop working immediately when revoked or disabled.
      </p>
      {#if apiKeysDisabled}
        <p class="t-label text-alert">API key creation and validation are disabled — enable them in settings</p>
      {/if}
    </div>

    {#if createdKey}
      <div class="grid gap-2 border-l-2 border-alert bg-panel p-4">
        <span class="t-label text-alert">NEW KEY — SHOWN ONCE, COPY IT NOW</span>
        <p class="break-all text-xs font-bold text-fg">{createdKey}</p>
        <div class="flex flex-wrap gap-2">
          <button class="btn-t-solid" on:click={() => copyText(createdKey)}>Copy key</button>
          <button class="btn-t" on:click={() => (createdKey ="")}>Dismiss</button>
        </div>
      </div>
    {/if}

    <div class="bg-panel">
      <div class="flex items-center justify-between border-b border-line px-4 py-2">
        <span class="t-label text-fg">Key principals / REC.COUNT {String(keys.length).padStart(3,"0")}</span>
        <span class="t-label">Hashes only stored</span>
      </div>

      {#if keys.length === 0}
        <div class="grid min-h-48 place-items-center p-8 text-center">
          <div>
            <p class="text-sm font-bold">No API keys</p>
            <p class="t-label mt-3">Press + new API key to issue a machine credential</p>
          </div>
        </div>
      {:else}
        <div class="hidden grid-cols-[1fr,1fr,110px,130px] gap-4 border-b border-line px-4 py-2 md:grid">
          <span class="t-label text-fg">Name / principal</span>
          <span class="t-label text-fg">Owner</span>
          <span class="t-label text-fg">Status</span>
          <span class="t-label text-right text-fg">Actions</span>
        </div>
        <div class="divide-y divide-line">
          {#each keys as key, index}
            {@const status = keyStatus(key)}
            <div class="grid gap-2 px-4 py-3 md:grid-cols-[1fr,1fr,110px,130px] md:items-center md:gap-4">
              <div class="min-w-0">
                <p class="truncate text-sm font-bold text-fg">
                  <span class="mr-2 text-xs font-medium text-dim">{String(index + 1).padStart(2,"0")}</span>{key.name || key.id}
                </p>
                <p class="mt-0.5 truncate pl-6 text-xs text-dim">api-key:{key.id}</p>
              </div>
              <p class="min-w-0 truncate text-xs text-dim">{ownerLabelFromID(key.user_id)}</p>
              <div>
                {#if status.ok}
                  <span class="text-xs font-bold text-fg">[ {status.label} ]</span>
                {:else}
                  <span class="text-xs font-bold text-alert">[ {status.label} ]</span>
                {/if}
              </div>
              <div class="flex gap-px md:justify-end">
                <button
                  class="rounded-md border border-line px-3 py-1 text-xs font-medium text-fg hover:bg-panel-hover"
                  disabled={working}
                  on:click={() => openEdit(key)}
                >
                  Edit
                </button>
                <button
                  class="rounded-md border border-alert/40 px-3 py-1 text-xs font-medium text-alert hover:bg-alert hover:text-white"
                  disabled={working}
                  on:click={() => revokeKey(key.id)}
                >
                  Revoke
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
          <span class="t-label text-fg">NEW API KEY / <span class="text-alert">Draft</span></span>
        </div>
        <button class="btn-t-solid" disabled={working || !selectedOwnerID || apiKeysDisabled} on:click={createKey}>Create API key</button>
      </div>

      {#if apiKeysDisabled}
        <p class="border-b border-line bg-panel px-4 py-2 text-xs font-bold text-alert">
          API key creation is disabled — enable it in settings first
        </p>
      {/if}

      <div class="grid gap-px bg-line p-px">
        <label class="grid gap-1 bg-panel p-3">
          <span class="t-label">Owner</span>
          <select bind:value={selectedOwnerID} class="field-t">
            <option value="">select owner</option>
            {#each owners as owner}
              <option value={owner.id}>{ownerLabel(owner)}</option>
            {/each}
          </select>
          {#if selectedOwner}
            <span class="text-xs leading-4 text-dim">ROLES: {accessIDs(selectedOwner,"roles")} / PERMISSIONS: {accessIDs(selectedOwner,"permissions")}</span>
          {/if}
        </label>
        <label class="grid gap-1 bg-panel p-3">
          <span class="t-label">Key name</span>
          <input bind:value={keyName} class="field-t" placeholder="ci-pipeline" />
        </label>
        <label class="grid gap-1 bg-panel p-3">
          <span class="t-label">Lifetime / expires in</span>
          <input bind:value={expiresIn} class="field-t" placeholder="720h; empty = no expiry" />
          <div class="mt-1 flex flex-wrap gap-px">
            {#each presets as preset}
              <button
                class={`border px-2.5 py-1 text-xs font-bold ${expiresIn === preset.value ?"border-alert bg-alert text-white" :"border-line text-dim hover:text-fg"}`}
                on:click={() => (expiresIn = preset.value)}
              >
                {preset.label}
              </button>
            {/each}
          </div>
          {#if maxLifetime}
            <span class="text-xs leading-4 text-dim">MAX LIFETIME CAP: {maxLifetime} — LONGER REQUESTS ARE SHORTENED</span>
          {/if}
        </label>
        <label class="grid gap-1 bg-panel p-3">
          <span class="t-label">Role IDs</span>
          <input bind:value={keyRoleIDs} class="field-t" placeholder="role-id-a, role-id-b" />
        </label>
        <label class="grid gap-1 bg-panel p-3">
          <span class="t-label">Permission IDs</span>
          <input bind:value={keyPermissionIDs} class="field-t" placeholder="perm-id-a, perm-id-b" />
          <span class="text-xs leading-4 text-dim">Leave role/permission IDs empty to inherit the owner's access.</span>
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
          <button class="btn-t-solid" disabled={working} on:click={() => editKey && saveKey(editKey)}>Save</button>
          <button
            class="rounded-md border border-alert/40 px-3 py-1 text-xs font-medium text-alert hover:bg-alert hover:text-white"
            disabled={working}
            on:click={() => editKey && revokeKey(editKey.id)}
          >
            Revoke
          </button>
        </div>
      </div>

      <div class="grid gap-px bg-line p-px">
        <label class="grid gap-1 bg-panel p-3">
          <span class="t-label">Name</span>
          <input bind:value={editKey.draft_name} class="field-t" placeholder={editKey.id} />
        </label>
        <label class="grid gap-1 bg-panel p-3">
          <span class="t-label">Role IDs</span>
          <input bind:value={editKey.draft_role_ids} class="field-t" placeholder="role-id-a, role-id-b" />
        </label>
        <label class="grid gap-1 bg-panel p-3">
          <span class="t-label">Permission IDs</span>
          <input bind:value={editKey.draft_permission_ids} class="field-t" placeholder="perm-id-a, perm-id-b" />
        </label>
        <label class="flex items-center gap-3 bg-panel p-3 text-xs font-bold">
          <input bind:checked={editKey.disabled} type="checkbox" class={checkboxClass(!editKey.disabled, true)} />
          <span class={editKey.disabled ?"text-alert" :"text-dim"}>{editKey.disabled ?"Disabled" :"Enabled"}</span>
        </label>
        <div class="bg-panel p-3 text-xs leading-5 text-dim">
          <p class="break-all">
            Owner {ownerLabelFromID(editKey.user_id)} / principal api-key:{editKey.id} / rev {editKey.revision}
          </p>
          <p class="break-all">
            Created {editKey.created_at} / updated {editKey.updated_at}
            {#if editKey.expires_at} / expires {editKey.expires_at}{:else} / no expiry{/if}
            {#if editKey.last_used_at} / last used {editKey.last_used_at}{/if}
          </p>
        </div>
      </div>
    </div>
  {:else if view === "settings"}
    <div class="bg-panel">
      <div class="flex flex-wrap items-center justify-between gap-3 border-b border-line px-4 py-2">
        <div class="flex items-center gap-3">
          <button class="btn-t border-0 bg-crt" disabled={working} on:click={backToList}>[ &lt; BACK ]</button>
          <span class="t-label text-fg">API key settings</span>
        </div>
        <button class="btn-t-solid" disabled={working} on:click={() => saveSetting("api_key")}>Save settings</button>
      </div>

      <div class="grid gap-px bg-line p-px">
        <label class="flex items-center gap-3 bg-panel p-3 text-xs font-bold">
          <input type="checkbox" checked={apiKeysDisabled} class={checkboxClass(apiKeysDisabled, true)} on:change={(event) => setSettingBool("api_key", ["disabled"], checkedValue(event))} />
          <span class={apiKeysDisabled ?"text-alert" :"text-dim"}>Disable API key creation and validation</span>
        </label>
        <label class="grid gap-1 bg-panel p-3">
          <span class="t-label">Max lifetime</span>
          <input class="field-t" value={maxLifetime} placeholder="empty = no cap, e.g. 720h" on:input={(event) => setSettingString("api_key", ["max_lifetime"], inputValue(event))} />
          <span class="text-xs leading-4 text-dim">Creation requests longer than this cap are shortened automatically.</span>
        </label>
      </div>
    </div>

    <div class="grid gap-px bg-line lg:grid-cols-2">
      <div class="bg-panel p-4">
        <span class="t-label text-fg">Direct usage</span>
        <pre class="mt-3 overflow-auto border border-line bg-crt p-3 text-xs leading-5 text-fg">curl https://app.example.com/api \
  -H 'X-API-Key: tak_...'</pre>
        <p class="mt-3 text-xs leading-4 text-dim">
          The key is a static credential sent on every request. No token exchange, no refresh dance; revoke or disable the key and access stops immediately.
        </p>
      </div>
      <div class="bg-panel p-4">
        <span class="t-label text-fg">Session integration</span>
        <pre class="mt-3 overflow-auto border border-line bg-crt p-3 text-xs leading-5 text-fg"># session provider config
api_key: true            # accept X-API-Key
# remote auth instance:
# oauth2:
#   api_key_url: {oauthBase}/oauth2/api-key</pre>
        <p class="mt-3 text-xs leading-4 text-dim">
          Session validates <span class="text-fg">X-API-Key</span> against the database on each request, deletes the raw key header, and forwards <span class="text-fg">X-User: api-key:&lt;id&gt;</span> with the key's own roles/permissions.
        </p>
      </div>
    </div>
  {/if}
</div>
