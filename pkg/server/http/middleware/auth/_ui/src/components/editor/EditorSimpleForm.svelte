<script lang="ts">
  import TemporaryAccessPanel from "./TemporaryAccessPanel.svelte";
  import PasskeyPanel from "./PasskeyPanel.svelte";
  import type { AnyRecord, KindSpec, ResourceKind } from "../../lib/api";

  export let editorKind: ResourceKind = "settings";
  export let editorSpec: KindSpec;
  export let apiBase = "/auth/v1";
  export let editorID = "";
  export let editorLoadedID = "";
  export let editorJSON = "";
  export let tempAccessRoleIDs = "";
  export let tempAccessPermissionIDs = "";
  export let tempAccessStartsAt = "";
  export let tempAccessExpiresIn = "1h";
  export let tempAccessExpiresAt = "";
  export let canGrantTemporaryAccess = false;
  export let canRemoveTemporaryAccess = false;
  export let getStringField: (key: string) => string = () => "";
  export let setStringField: (key: string, value: string) => void = () => {};
  export let getBoolField: (key: string, fallback?: boolean) => boolean = () => false;
  export let setBoolField: (key: string, value: boolean) => void = () => {};
  export let setLocalUser: (value: boolean) => void = () => {};
  export let getListField: (key: string) => string = () => "";
  export let setListField: (key: string, value: string) => void = () => {};
  export let getNestedString: (parent: string, key: string) => string = () => "";
  export let setNestedString: (parent: string, key: string, value: string) => void = () => {};
  export let getFirstArrayString: (parent: string, key: string) => string = () => "";
  export let setFirstArrayString: (parent: string, key: string, value: string) => void = () => {};
  export let getFirstArrayList: (parent: string, key: string) => string = () => "";
  export let setFirstArrayList: (parent: string, key: string, value: string) => void = () => {};
  export let getJSONField: (key: string) => string = () => "{}";
  export let setJSONField: (key: string, value: string) => void = () => {};
  export let getPathString: (path: string[]) => string = () => "";
  export let setPathString: (path: string[], value: string) => void = () => {};
  export let setPathBool: (path: string[], value: boolean) => void = () => {};
  export let getPathBool: (path: string[], fallback?: boolean) => boolean = () => false;
  export let getPathNumber: (path: string[], fallback?: number) => number = () => 0;
  export let setPathNumber: (path: string[], value: string) => void = () => {};
  export let getPathList: (path: string[]) => string = () => "";
  export let setPathList: (path: string[], value: string) => void = () => {};
  export let addPermissionResource: () => void = () => {};
  export let removePermissionResource: (index: number) => void = () => {};
  export let getResourceList: (index: number, key: string) => string = () => "";
  export let setResourceList: (index: number, key: string, value: string) => void = () => {};
  export let temporaryAccessItems: (key: "tmp_role_ids" | "tmp_permission_ids", json: string) => AnyRecord[] = () => [];
  export let patchTemporaryAccess: (remove?: boolean) => void | Promise<void> = () => {};

  // Derive the resource list straight from editorJSON (a reactive prop) so
  // ADD/REMOVE/edit re-render it, including down to zero. permissionResources()
  // reads editorJSON through a prop function whose dependency Svelte cannot see,
  // so binding the list to it never updated after the first render.
  $: resources = parseResources(editorJSON);

  function parseResources(json: string): AnyRecord[] {
    try {
      const parsed = JSON.parse(json) as AnyRecord;
      const list = parsed?.resources;
      if (!Array.isArray(list)) return [];
      return list.map((item) =>
        item && typeof item === "object" && !Array.isArray(item) ? { ...(item as AnyRecord) } : ({} as AnyRecord),
      );
    } catch {
      return [];
    }
  }

  function inputValue(event: Event) {
    return (event.currentTarget as HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement).value;
  }

  function checkedValue(event: Event) {
    return (event.currentTarget as HTMLInputElement).checked;
  }

  function checkboxClass(checked: boolean, variant: "positive" | "danger" | "neutral" = "positive") {
    const base = "h-3.5 w-3.5 appearance-none border bg-crt";
    if (variant === "danger") return `${base} border-line checked:bg-alert`;
    if (variant === "neutral") return `${base} border-line checked:bg-fg`;

    return `${base} ${checked ? "border-line" : "border-alert"} checked:bg-fg`;
  }

  let mtlsCertPEM = "";
  let mtlsCertError = "";

  function certificateBytes(value: string) {
    const cleaned = value
      .replace(/-----BEGIN CERTIFICATE-----/g, "")
      .replace(/-----END CERTIFICATE-----/g, "")
      .replace(/\s+/g, "");
    if (!cleaned) throw new Error("CERTIFICATE IS EMPTY");

    const binary = atob(cleaned);
    const bytes = new Uint8Array(binary.length);
    for (let i = 0; i < binary.length; i += 1) {
      bytes[i] = binary.charCodeAt(i);
    }

    return bytes;
  }

  async function useMTLSCertificateFingerprint() {
    mtlsCertError = "";
    try {
      const digest = await crypto.subtle.digest("SHA-256", certificateBytes(mtlsCertPEM));
      const hex = Array.from(new Uint8Array(digest))
        .map((item) => item.toString(16).padStart(2, "0"))
        .join("");
      setNestedString("details", "cert_fingerprint", hex);
      mtlsCertPEM = "";
    } catch (err) {
      mtlsCertError = err instanceof Error ? err.message : "CANNOT READ CERTIFICATE";
    }
  }

  function editorPathFromJSON(json: string, path: string[]) {
    let value: unknown;
    try {
      value = JSON.parse(json || "{}");
    } catch {
      return undefined;
    }

    for (const key of path) {
      if (!value || typeof value !== "object" || Array.isArray(value)) return undefined;
      value = (value as AnyRecord)[key];
    }

    return value;
  }

  function currentCodeStoreActive(currentEditorJSON: string) {
    const value = editorPathFromJSON(currentEditorJSON, ["code_store", "active"]);
    return typeof value === "string" && value ? value : "memory";
  }

  function currentRedisTLS(currentEditorJSON: string) {
    return editorPathFromJSON(currentEditorJSON, ["code_store", "redis", "tls", "enabled"]) === true;
  }

  function setCodeStoreActive(event: Event) {
    codeStoreActive = inputValue(event);
    setPathString(["code_store", "active"], codeStoreActive);
  }

  function setRedisTLS(event: Event) {
    redisTLSEnabled = checkedValue(event);
    setPathBool(["code_store", "redis", "tls", "enabled"], redisTLSEnabled);
  }

  let codeStoreActive = "memory";
  let redisTLSEnabled = false;

  $: settingsNamespace = editorKind === "settings" ? editorLoadedID || editorID : "";
  $: codeStoreActive = currentCodeStoreActive(editorJSON);
  $: redisTLSEnabled = currentRedisTLS(editorJSON);
</script>

<div class="grid gap-px bg-line p-px md:grid-cols-2 xl:grid-cols-3">
  {#if editorKind === "settings"}
    {#if settingsNamespace === "admin"}
      <label class="grid gap-1 bg-panel p-3 md:col-span-2">
        <span class="t-label">ADMIN PERMISSION</span>
        <input class="field-t" value={getStringField("permission")} placeholder="turna.auth.admin; empty = bootstrap open" on:input={(event) => setStringField("permission", inputValue(event))} />
        <span class="text-[10px] leading-4 text-dim">Matched against permission ID or name on X-User. Empty keeps bootstrap compatibility.</span>
      </label>
      <label class="flex items-center gap-3 bg-panel p-3 text-xs font-bold uppercase tracking-[0.15em]">
        <input type="checkbox" checked={getBoolField("allow_missing_x_user", true)} class={checkboxClass(getBoolField("allow_missing_x_user", true), "danger")} on:change={(event) => setBoolField("allow_missing_x_user", checkedValue(event))} />
        <span class={getBoolField("allow_missing_x_user", true) ? "text-alert" : "text-dim"}>ALLOW MISSING X-USER BREAK-GLASS ADMIN</span>
      </label>
      <p class="bg-panel p-3 text-[11px] leading-4 text-dim md:col-span-2 xl:col-span-3">
        Use break-glass only when the auth route is not publicly exposed. If enabled, removing the session chain lets direct requests administer auth.
      </p>
    {:else if settingsNamespace === "cache"}
      <label class="grid gap-1 bg-panel p-3">
        <span class="t-label">POLL INTERVAL</span>
        <input class="field-t" value={getPathString(["poll_interval"])} placeholder="5s" on:input={(event) => setPathString(["poll_interval"], inputValue(event))} />
      </label>
      <label class="grid gap-1 bg-panel p-3">
        <span class="t-label">OAUTH CODE STORE</span>
        <select class="field-t" on:change={setCodeStoreActive}>
          <option value="memory" selected={codeStoreActive === "memory"}>memory</option>
          <option value="redis" selected={codeStoreActive === "redis"}>redis</option>
        </select>
      </label>
      <p class="bg-panel p-3 text-[11px] leading-4 text-dim">
        Redis is recommended for multi-instance authorization-code, provider-state, device and email flows.
      </p>

      {#if codeStoreActive === "redis"}
        <label class="grid gap-1 bg-panel p-3 md:col-span-2">
          <span class="t-label">REDIS ADDRESSES</span>
          <input class="field-t" value={getPathList(["code_store", "redis", "address"])} placeholder="127.0.0.1:6379" on:input={(event) => setPathList(["code_store", "redis", "address"], inputValue(event))} />
        </label>
        <label class="grid gap-1 bg-panel p-3">
          <span class="t-label">REDIS USERNAME</span>
          <input class="field-t" value={getPathString(["code_store", "redis", "username"])} placeholder="optional" on:input={(event) => setPathString(["code_store", "redis", "username"], inputValue(event))} />
        </label>
        <label class="grid gap-1 bg-panel p-3">
          <span class="t-label">REDIS PASSWORD</span>
          <input class="field-t" value={getPathString(["code_store", "redis", "password"])} placeholder="optional" on:input={(event) => setPathString(["code_store", "redis", "password"], inputValue(event))} />
        </label>
        <label class="grid gap-1 bg-panel p-3">
          <span class="t-label">CLIENT NAME</span>
          <input class="field-t" value={getPathString(["code_store", "redis", "client_name"])} placeholder="turna-auth" on:input={(event) => setPathString(["code_store", "redis", "client_name"], inputValue(event))} />
        </label>
        <label class="flex items-center gap-3 bg-panel p-3 text-xs font-bold uppercase tracking-[0.15em]">
          <input type="checkbox" checked={redisTLSEnabled} class={checkboxClass(redisTLSEnabled, "neutral")} on:change={setRedisTLS} />
          <span class={redisTLSEnabled ? "text-fg" : "text-dim"}>REDIS TLS</span>
        </label>
        {#if redisTLSEnabled}
          <label class="grid gap-1 bg-panel p-3">
            <span class="t-label">TLS CA FILE</span>
            <input class="field-t" value={getPathString(["code_store", "redis", "tls", "ca_file"])} placeholder="optional" on:input={(event) => setPathString(["code_store", "redis", "tls", "ca_file"], inputValue(event))} />
          </label>
          <label class="grid gap-1 bg-panel p-3">
            <span class="t-label">TLS CERT FILE</span>
            <input class="field-t" value={getPathString(["code_store", "redis", "tls", "cert_file"])} placeholder="optional" on:input={(event) => setPathString(["code_store", "redis", "tls", "cert_file"], inputValue(event))} />
          </label>
          <label class="grid gap-1 bg-panel p-3">
            <span class="t-label">TLS KEY FILE</span>
            <input class="field-t" value={getPathString(["code_store", "redis", "tls", "key_file"])} placeholder="optional" on:input={(event) => setPathString(["code_store", "redis", "tls", "key_file"], inputValue(event))} />
          </label>
        {/if}
      {/if}
    {:else if settingsNamespace === "device"}
      <label class="flex items-center gap-3 bg-panel p-3 text-xs font-bold uppercase tracking-[0.15em]">
        <input type="checkbox" checked={getBoolField("disabled")} class={checkboxClass(getBoolField("disabled"), "danger")} on:change={(event) => setBoolField("disabled", checkedValue(event))} />
        <span class={getBoolField("disabled") ? "text-alert" : "text-dim"}>DISABLE DEVICE FLOW</span>
      </label>
      <label class="grid gap-1 bg-panel p-3">
        <span class="t-label">CODE LIFETIME</span>
        <input class="field-t" value={getStringField("code_lifetime")} placeholder="10m" on:input={(event) => setStringField("code_lifetime", inputValue(event))} />
      </label>
      <label class="grid gap-1 bg-panel p-3">
        <span class="t-label">POLL INTERVAL (SECONDS)</span>
        <input class="field-t" type="number" min="1" value={getPathNumber(["interval"], 5)} on:input={(event) => setPathNumber(["interval"], inputValue(event))} />
      </label>
      <label class="grid gap-1 bg-panel p-3 md:col-span-2 xl:col-span-3">
        <span class="t-label">VERIFICATION URI</span>
        <input class="field-t" value={getStringField("verification_uri")} placeholder="default: <prefix>/ui/device" on:input={(event) => setStringField("verification_uri", inputValue(event))} />
      </label>
    {:else if settingsNamespace === "token_exchange"}
      <label class="flex items-center gap-3 bg-panel p-3 text-xs font-bold uppercase tracking-[0.15em]">
        <input type="checkbox" checked={getBoolField("disabled")} class={checkboxClass(getBoolField("disabled"), "danger")} on:change={(event) => setBoolField("disabled", checkedValue(event))} />
        <span class={getBoolField("disabled") ? "text-alert" : "text-dim"}>DISABLE TOKEN EXCHANGE</span>
      </label>
    {:else if settingsNamespace === "totp"}
      <label class="flex items-center gap-3 bg-panel p-3 text-xs font-bold uppercase tracking-[0.15em]">
        <input type="checkbox" checked={getBoolField("disabled")} class={checkboxClass(getBoolField("disabled"), "danger")} on:change={(event) => setBoolField("disabled", checkedValue(event))} />
        <span class={getBoolField("disabled") ? "text-alert" : "text-dim"}>DISABLE TOTP</span>
      </label>
      <label class="grid gap-1 bg-panel p-3">
        <span class="t-label">ISSUER</span>
        <input class="field-t" value={getStringField("issuer")} placeholder="Turna Auth" on:input={(event) => setStringField("issuer", inputValue(event))} />
      </label>
      <label class="grid gap-1 bg-panel p-3">
        <span class="t-label">SKEW PERIODS</span>
        <input class="field-t" type="number" min="0" value={getPathNumber(["skew"], 1)} on:input={(event) => setPathNumber(["skew"], inputValue(event))} />
      </label>
    {/if}
  {:else if editorKind === "clients"}
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">CLIENT SECRET</span>
      <input class="field-t" value={getStringField("client_secret")} placeholder="change-me" on:input={(event) => setStringField("client_secret", inputValue(event))} />
    </label>
    <label class="grid gap-1 bg-panel p-3 md:col-span-2">
      <span class="t-label">SCOPES</span>
      <input class="field-t" value={getListField("scope")} placeholder="openid, profile" on:input={(event) => setListField("scope", inputValue(event))} />
    </label>
    <label class="grid gap-1 bg-panel p-3 md:col-span-2 xl:col-span-3">
      <span class="t-label">REDIRECT / WHITELIST URLS</span>
      <textarea class="field-t min-h-24" value={getListField("whitelist_urls")} placeholder="https://app.example.com/callback" on:input={(event) => setListField("whitelist_urls", inputValue(event))}></textarea>
    </label>
  {:else if editorKind === "providers"}
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">UPSTREAM CLIENT ID</span>
      <input class="field-t" value={getStringField("client_id")} placeholder="turna" on:input={(event) => setStringField("client_id", inputValue(event))} />
    </label>
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">UPSTREAM CLIENT SECRET</span>
      <input class="field-t" value={getStringField("client_secret")} placeholder="change-me" on:input={(event) => setStringField("client_secret", inputValue(event))} />
    </label>
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">SCOPES</span>
      <input class="field-t" value={getListField("scopes")} placeholder="openid, profile, email" on:input={(event) => setListField("scopes", inputValue(event))} />
    </label>
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">AUTH URL</span>
      <input class="field-t" value={getStringField("auth_url")} placeholder="https://idp.example.com/auth" on:input={(event) => setStringField("auth_url", inputValue(event))} />
    </label>
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">TOKEN URL</span>
      <input class="field-t" value={getStringField("token_url")} placeholder="https://idp.example.com/token" on:input={(event) => setStringField("token_url", inputValue(event))} />
    </label>
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">CERT / JWKS URL</span>
      <input class="field-t" value={getStringField("cert_url")} placeholder="https://idp.example.com/certs" on:input={(event) => setStringField("cert_url", inputValue(event))} />
    </label>
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">USERINFO URL</span>
      <input class="field-t" value={getStringField("userinfo_url")} placeholder="optional" on:input={(event) => setStringField("userinfo_url", inputValue(event))} />
    </label>
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">INTROSPECT URL</span>
      <input class="field-t" value={getStringField("introspect_url")} placeholder="optional" on:input={(event) => setStringField("introspect_url", inputValue(event))} />
    </label>
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">REVOCATION URL</span>
      <input class="field-t" value={getStringField("revocation_url")} placeholder="optional" on:input={(event) => setStringField("revocation_url", inputValue(event))} />
    </label>
    <label class="grid gap-1 bg-panel p-3 md:col-span-2 xl:col-span-3">
      <span class="t-label">LOGOUT URL</span>
      <input class="field-t" value={getStringField("logout_url")} placeholder="optional" on:input={(event) => setStringField("logout_url", inputValue(event))} />
    </label>
    <div class="grid gap-px bg-line p-px md:col-span-2 xl:col-span-3">
      <div class="bg-panel px-3 py-2">
        <span class="t-label text-fg">[ AUTO-REGISTER / ROLE MAPPING ]</span>
      </div>
      <div class="grid gap-px bg-line md:grid-cols-2 xl:grid-cols-3">
        <label class="flex items-center gap-3 bg-panel p-3 text-xs font-bold uppercase tracking-[0.15em]">
          <input type="checkbox" checked={getPathBool(["claim_mapping", "register"])} class={checkboxClass(getPathBool(["claim_mapping", "register"]), "neutral")} on:change={(event) => setPathBool(["claim_mapping", "register"], checkedValue(event))} />
          <span class={getPathBool(["claim_mapping", "register"]) ? "text-fg" : "text-dim"}>REGISTER UNKNOWN USERS ON FIRST LOGIN</span>
        </label>
        <label class="flex items-center gap-3 bg-panel p-3 text-xs font-bold uppercase tracking-[0.15em]">
          <input type="checkbox" checked={getPathBool(["claim_mapping", "use_lmap"])} class={checkboxClass(getPathBool(["claim_mapping", "use_lmap"]), "neutral")} on:change={(event) => setPathBool(["claim_mapping", "use_lmap"], checkedValue(event))} />
          <span class={getPathBool(["claim_mapping", "use_lmap"]) ? "text-fg" : "text-dim"}>RESOLVE ROLES VIA LDAP GROUP MAPS</span>
        </label>
        <label class="grid gap-1 bg-panel p-3">
          <span class="t-label">ROLES CLAIM</span>
          <input class="field-t" value={getPathString(["claim_mapping", "roles_claim"])} placeholder="groups / realm_access.roles" on:input={(event) => setPathString(["claim_mapping", "roles_claim"], inputValue(event))} />
        </label>
      </div>
      <p class="bg-panel p-3 text-[11px] leading-4 text-dim">
        With <span class="text-fg">register</span> on, a first-time OAuth2 login creates a non-local user from the provider claims (direct OAuth2 signup). Map claim values to roles via LDAP group maps or the <span class="text-fg">role_map</span> in Advanced JSON.
      </p>
    </div>
  {:else if editorKind === "saml"}
    <label class="grid gap-1 bg-panel p-3 md:col-span-2">
      <span class="t-label">IDP METADATA URL</span>
      <input class="field-t" value={getStringField("metadata_url")} placeholder="https://idp.example.com/metadata" on:input={(event) => setStringField("metadata_url", inputValue(event))} />
    </label>
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">SP ENTITY ID</span>
      <input class="field-t" value={getStringField("entity_id")} placeholder="optional" on:input={(event) => setStringField("entity_id", inputValue(event))} />
    </label>
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">ALIAS ATTRIBUTE</span>
      <input class="field-t" value={getStringField("alias_attribute")} placeholder="email / NameID fallback" on:input={(event) => setStringField("alias_attribute", inputValue(event))} />
    </label>
    <label class="flex items-center gap-3 bg-panel p-3 text-xs font-bold uppercase tracking-[0.15em]">
      <input type="checkbox" checked={getBoolField("sign_requests")} class={checkboxClass(getBoolField("sign_requests"), "neutral")} on:change={(event) => setBoolField("sign_requests", checkedValue(event))} />
      <span class={getBoolField("sign_requests") ? "text-fg" : "text-dim"}>SIGN AUTHNREQUESTS</span>
    </label>
    <label class="grid gap-1 bg-panel p-3 md:col-span-2 xl:col-span-3">
      <span class="t-label">INLINE IDP METADATA XML</span>
      <textarea class="field-t min-h-32" value={getStringField("metadata_xml")} placeholder="optional; takes precedence over metadata_url" on:input={(event) => setStringField("metadata_xml", inputValue(event))}></textarea>
    </label>
    <label class="grid gap-1 bg-panel p-3 md:col-span-2 xl:col-span-3">
      <span class="t-label">CLAIM MAPPING JSON</span>
      <textarea class="field-t min-h-32" value={getJSONField("claim_mapping")} placeholder={`{
  "roles_claim": "groups",
  "use_lmap": true,
  "role_map": {},
  "register": true
}`} on:change={(event) => setJSONField("claim_mapping", inputValue(event))}></textarea>
      <span class="text-[10px] leading-4 text-dim">Map SAML assertion attributes to sync roles. Use role names/IDs in role_map values.</span>
    </label>
    <p class="bg-panel p-3 text-[11px] leading-4 text-dim md:col-span-2 xl:col-span-3">
      Register this provider's SP metadata at <span class="text-fg">/auth/saml/{editorID || "provider"}/metadata</span>. Use Advanced JSON for less common SAML options.
    </p>
  {:else if editorKind === "ldap"}
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">LDAP ADDRESS</span>
      <input class="field-t" value={getStringField("addr")} placeholder="ldap://ldap.example.com:389" on:input={(event) => setStringField("addr", inputValue(event))} />
    </label>
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">BIND USERNAME</span>
      <input class="field-t" value={getNestedString("bind", "username")} placeholder="cn=readonly,dc=example,dc=com" on:input={(event) => setNestedString("bind", "username", inputValue(event))} />
    </label>
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">BIND PASSWORD</span>
      <input class="field-t" value={getNestedString("bind", "password")} placeholder="change-me" on:input={(event) => setNestedString("bind", "password", inputValue(event))} />
    </label>
    <label class="grid gap-1 bg-panel p-3 md:col-span-2">
      <span class="t-label">USER BASE DN</span>
      <input class="field-t" value={getStringField("user_base_dn")} placeholder="ou=people,dc=example,dc=com" on:input={(event) => setStringField("user_base_dn", inputValue(event))} />
    </label>
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">SYNC DURATION</span>
      <input class="field-t" value={getStringField("sync_duration")} placeholder="10m" on:input={(event) => setStringField("sync_duration", inputValue(event))} />
    </label>
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">GROUP BASE DN</span>
      <input class="field-t" value={getFirstArrayString("groups", "base_dn")} placeholder="ou=groups,dc=example,dc=com" on:input={(event) => setFirstArrayString("groups", "base_dn", inputValue(event))} />
    </label>
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">GROUP FILTER</span>
      <input class="field-t" value={getFirstArrayString("groups", "filter")} placeholder="(objectClass=groupOfUniqueNames)" on:input={(event) => setFirstArrayString("groups", "filter", inputValue(event))} />
    </label>
    <label class="grid gap-1 bg-panel p-3 md:col-span-2">
      <span class="t-label">GROUP ATTRIBUTES</span>
      <input class="field-t" value={getFirstArrayList("groups", "attributes")} placeholder="cn, uniqueMember, description" on:input={(event) => setFirstArrayList("groups", "attributes", inputValue(event))} />
    </label>
    <label class="flex items-center gap-3 bg-panel p-3 text-xs font-bold uppercase tracking-[0.15em]">
      <input type="checkbox" checked={getBoolField("disable_sync")} class={checkboxClass(getBoolField("disable_sync"), "danger")} on:change={(event) => setBoolField("disable_sync", checkedValue(event))} />
      <span class={getBoolField("disable_sync") ? "text-alert" : "text-fg"}>{getBoolField("disable_sync") ? "SYNC DISABLED" : "SYNC ENABLED"}</span>
    </label>
    <p class="bg-panel p-3 text-[11px] leading-4 text-dim md:col-span-2 xl:col-span-3">LDAP group filters can contain multiple entries. Use Advanced JSON when you need more than one group mapping.</p>
  {:else if editorKind === "users"}
    <label class="grid gap-1 bg-panel p-3 md:col-span-2">
      <span class="t-label">ALIASES / LOGIN IDS{editorLoadedID ? "" : " *"}</span>
      <input class="field-t" value={getListField("alias")} placeholder="user@example.com, user" on:input={(event) => setListField("alias", inputValue(event))} />
    </label>
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">NAME{!editorLoadedID && getBoolField("local", true) ? " *" : ""}</span>
      <input class="field-t" value={getNestedString("details", "name")} placeholder="User Name" on:input={(event) => setNestedString("details", "name", inputValue(event))} />
    </label>
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">EMAIL</span>
      <input class="field-t" value={getNestedString("details", "email")} placeholder="user@example.com" on:input={(event) => setNestedString("details", "email", inputValue(event))} />
    </label>
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">UID</span>
      <input class="field-t" value={getNestedString("details", "uid")} placeholder="user" on:input={(event) => setNestedString("details", "uid", inputValue(event))} />
    </label>
    <label class="flex items-center gap-3 bg-panel p-3 text-xs font-bold uppercase tracking-[0.15em]">
      <input type="checkbox" checked={getBoolField("local", true)} class={checkboxClass(getBoolField("local", true), "neutral")} on:change={(event) => setLocalUser(checkedValue(event))} />
      <span class={getBoolField("local", true) ? "text-fg" : "text-dim"}>LOCAL USER</span>
    </label>
    {#if getBoolField("local", true)}
      <label class="grid gap-1 bg-panel p-3">
        <span class="t-label">PASSWORD{editorLoadedID ? "" : " *"}</span>
        <input class="field-t" value={getNestedString("details", "password")} placeholder="leave empty to keep existing" on:input={(event) => setNestedString("details", "password", inputValue(event))} />
      </label>
    {/if}
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">ROLE IDS</span>
      <input class="field-t" value={getListField("role_ids")} placeholder="admin, operator" on:input={(event) => setListField("role_ids", inputValue(event))} />
    </label>
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">SYNC ROLE IDS</span>
      <input class="field-t" value={getListField("sync_role_ids")} placeholder="ldap-admin" on:input={(event) => setListField("sync_role_ids", inputValue(event))} />
    </label>
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">PERMISSION IDS</span>
      <input class="field-t" value={getListField("permission_ids")} placeholder="read-api" on:input={(event) => setListField("permission_ids", inputValue(event))} />
    </label>
    <label class="flex items-center gap-3 bg-panel p-3 text-xs font-bold uppercase tracking-[0.15em]">
      <input type="checkbox" checked={getBoolField("is_active", true)} class={checkboxClass(getBoolField("is_active", true))} on:change={(event) => setBoolField("is_active", checkedValue(event))} />
      <span class={getBoolField("is_active", true) ? "text-fg" : "text-alert"}>{getBoolField("is_active", true) ? "ACTIVE" : "DISABLED"}</span>
    </label>
    <p class="bg-panel p-3 text-[11px] leading-4 text-dim md:col-span-2 xl:col-span-3">Permanent grants use role and permission fields. Temporary grants are managed in the panel below after the user is created.</p>
  {:else if editorKind === "service-accounts"}
    <label class="grid gap-1 bg-panel p-3 md:col-span-2">
      <span class="t-label">ALIASES / CLIENT IDS{editorLoadedID ? "" : " *"}</span>
      <input class="field-t" value={getListField("alias")} placeholder="my-service" on:input={(event) => setListField("alias", inputValue(event))} />
    </label>
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">NAME{editorLoadedID ? "" : " *"}</span>
      <input class="field-t" value={getNestedString("details", "name")} placeholder="my-service" on:input={(event) => setNestedString("details", "name", inputValue(event))} />
    </label>
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">CLIENT SECRET / OR MTLS CERT{editorLoadedID || getNestedString("details", "secret") || getNestedString("details", "cert_fingerprint") || getNestedString("details", "cert_subject") ? "" : " *"}</span>
      <input class="field-t" value={getNestedString("details", "secret")} placeholder="optional for mTLS-only clients" on:input={(event) => setNestedString("details", "secret", inputValue(event))} />
    </label>
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">DEFAULT SCOPE</span>
      <input class="field-t" value={getNestedString("details", "scope")} placeholder="openid profile" on:input={(event) => setNestedString("details", "scope", inputValue(event))} />
    </label>
    <div class="grid gap-px bg-line p-px md:col-span-2 xl:col-span-3">
      <div class="flex flex-wrap items-center justify-between gap-2 bg-panel px-3 py-2">
        <span class="t-label text-fg">[ MTLS CLIENT CERTIFICATE ]</span>
        <span class="t-label">CLIENT_ID = FIRST ALIAS</span>
      </div>
      <div class="grid gap-px bg-line md:grid-cols-2">
        <label class="grid gap-1 bg-panel p-3 md:col-span-2">
          <span class="t-label">CERT SHA256 FINGERPRINT</span>
          <input class="field-t" value={getNestedString("details", "cert_fingerprint")} placeholder="lowercase sha256 hex" on:input={(event) => setNestedString("details", "cert_fingerprint", inputValue(event))} />
        </label>
        <label class="grid gap-1 bg-panel p-3 md:col-span-2">
          <span class="t-label">CERT SUBJECT</span>
          <input class="field-t" value={getNestedString("details", "cert_subject")} placeholder="CN=my-client,O=Example" on:input={(event) => setNestedString("details", "cert_subject", inputValue(event))} />
        </label>
        <label class="grid gap-1 bg-panel p-3 md:col-span-2">
          <span class="t-label">PASTE PEM CERTIFICATE TO CALCULATE FINGERPRINT</span>
          <textarea bind:value={mtlsCertPEM} class="field-t min-h-28 text-[11px] leading-4" placeholder={"-----BEGIN CERTIFICATE-----\n..."}></textarea>
          {#if mtlsCertError}<span class="text-[10px] leading-4 text-alert">{mtlsCertError}</span>{/if}
        </label>
        <div class="bg-panel p-3 md:col-span-2">
          <button class="btn-t-solid" disabled={!mtlsCertPEM.trim()} on:click={useMTLSCertificateFingerprint}>[ USE CERT FINGERPRINT ]</button>
        </div>
      </div>
      <p class="bg-panel p-3 text-[11px] leading-4 text-dim">
        With global mTLS enabled, this service account can use <span class="text-fg">grant_type=client_credentials</span> without a secret when the presented certificate matches one of these fields.
      </p>
    </div>
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">ROLE IDS</span>
      <input class="field-t" value={getListField("role_ids")} placeholder="service-role" on:input={(event) => setListField("role_ids", inputValue(event))} />
    </label>
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">SYNC ROLE IDS</span>
      <input class="field-t" value={getListField("sync_role_ids")} placeholder="ldap-service-role" on:input={(event) => setListField("sync_role_ids", inputValue(event))} />
    </label>
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">PERMISSION IDS</span>
      <input class="field-t" value={getListField("permission_ids")} placeholder="service-read" on:input={(event) => setListField("permission_ids", inputValue(event))} />
    </label>
    <label class="flex items-center gap-3 bg-panel p-3 text-xs font-bold uppercase tracking-[0.15em]">
      <input type="checkbox" checked={getBoolField("is_active", true)} class={checkboxClass(getBoolField("is_active", true))} on:change={(event) => setBoolField("is_active", checkedValue(event))} />
      <span class={getBoolField("is_active", true) ? "text-fg" : "text-alert"}>{getBoolField("is_active", true) ? "ACTIVE" : "DISABLED"}</span>
    </label>
    <p class="bg-panel p-3 text-[11px] leading-4 text-dim md:col-span-2 xl:col-span-3">Permanent grants use role and permission fields. Temporary grants are managed in the panel below after the service account is created.</p>
  {:else if editorKind === "roles"}
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">ROLE NAME</span>
      <input class="field-t" value={getStringField("name")} placeholder="my-role" on:input={(event) => setStringField("name", inputValue(event))} />
    </label>
    <label class="grid gap-1 bg-panel p-3 md:col-span-2">
      <span class="t-label">DESCRIPTION</span>
      <input class="field-t" value={getStringField("description")} placeholder="optional" on:input={(event) => setStringField("description", inputValue(event))} />
    </label>
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">PERMISSION IDS</span>
      <input class="field-t" value={getListField("permission_ids")} placeholder="read-api, write-api" on:input={(event) => setListField("permission_ids", inputValue(event))} />
    </label>
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">INCLUDED ROLE IDS</span>
      <input class="field-t" value={getListField("role_ids")} placeholder="base-role" on:input={(event) => setListField("role_ids", inputValue(event))} />
    </label>
    <label class="grid gap-1 bg-panel p-3 md:col-span-2 xl:col-span-3">
      <span class="t-label">DATA JSON</span>
      <textarea class="field-t min-h-32" value={getJSONField("data")} placeholder={`{\n  "tenant": "default"\n}`} on:change={(event) => setJSONField("data", inputValue(event))}></textarea>
      <span class="text-[10px] leading-4 text-dim">JSON object stored as role.data.</span>
    </label>
  {:else if editorKind === "permissions"}
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">PERMISSION NAME</span>
      <input class="field-t" value={getStringField("name")} placeholder="my-permission" on:input={(event) => setStringField("name", inputValue(event))} />
    </label>
    <label class="grid gap-1 bg-panel p-3 md:col-span-2">
      <span class="t-label">DESCRIPTION</span>
      <input class="field-t" value={getStringField("description")} placeholder="optional" on:input={(event) => setStringField("description", inputValue(event))} />
    </label>
    <div class="grid gap-2 bg-panel p-3 md:col-span-2 xl:col-span-3">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <span class="t-label text-fg">RESOURCES</span>
        <button class="btn-t border-0 bg-crt" on:click={addPermissionResource}>ADD RESOURCE</button>
      </div>
      {#if resources.length === 0}
        <p class="text-[11px] leading-4 text-dim">No resources yet. A permission with no resources matches nothing.</p>
      {/if}
    </div>

    {#each resources as resource, i}
      <div class="grid gap-px bg-line p-px md:col-span-2 xl:col-span-3">
        <div class="flex flex-wrap items-center justify-between gap-2 bg-panel px-3 py-2">
          <span class="t-label text-fg">RESOURCE {String(i + 1).padStart(2, "0")}</span>
          <button class="border border-line px-2.5 py-1 text-[10px] font-bold uppercase tracking-[0.1em] text-alert hover:bg-alert hover:text-white" on:click={() => removePermissionResource(i)}>REMOVE</button>
        </div>
        <div class="grid gap-px bg-line md:grid-cols-3">
          <label class="grid gap-1 bg-panel p-3">
            <span class="t-label">HOSTS</span>
            <input class="field-t" value={getResourceList(i, "hosts")} placeholder="api.example.com" on:input={(event) => setResourceList(i, "hosts", inputValue(event))} />
          </label>
          <label class="grid gap-1 bg-panel p-3">
            <span class="t-label">PATHS</span>
            <input class="field-t" value={getResourceList(i, "paths")} placeholder="/api/**" on:input={(event) => setResourceList(i, "paths", inputValue(event))} />
          </label>
          <label class="grid gap-1 bg-panel p-3">
            <span class="t-label">METHODS</span>
            <input class="field-t" value={getResourceList(i, "methods")} placeholder="GET, POST" on:input={(event) => setResourceList(i, "methods", inputValue(event))} />
          </label>
        </div>
      </div>
    {/each}

    <label class="grid gap-1 bg-panel p-3 md:col-span-2 xl:col-span-3">
      <span class="t-label">DATA JSON</span>
      <textarea class="field-t min-h-32" value={getJSONField("data")} placeholder={`{\n  "tenant": "default",\n  "region": "eu"\n}`} on:change={(event) => setJSONField("data", inputValue(event))}></textarea>
      <span class="text-[10px] leading-4 text-dim">JSON object stored as permission.data.</span>
    </label>

    <label class="grid gap-1 bg-panel p-3 md:col-span-2 xl:col-span-3">
      <span class="t-label">SCOPE ROLE MAP JSON</span>
      <textarea class="field-t min-h-32" value={getJSONField("scope")} placeholder={`{\n  "openid": ["role-id-1", "role-id-2"],\n  "admin": ["admin-role-id"]\n}`} on:change={(event) => setJSONField("scope", inputValue(event))}></textarea>
      <span class="text-[10px] leading-4 text-dim">JSON object stored as permission.scope. Each key is a scope, each value is a role ID array.</span>
    </label>

    <p class="bg-panel p-3 text-[11px] leading-4 text-dim md:col-span-2 xl:col-span-3">Use Advanced JSON for excluded resources or legacy single path.</p>
  {:else if editorKind === "lmaps"}
    <label class="grid gap-1 bg-panel p-3">
      <span class="t-label">LDAP GROUP NAME</span>
      <input class="field-t" value={getStringField("name")} placeholder="ldap-group" on:input={(event) => setStringField("name", inputValue(event))} />
    </label>
    <label class="grid gap-1 bg-panel p-3 md:col-span-2">
      <span class="t-label">ROLE IDS</span>
      <input class="field-t" value={getListField("role_ids")} placeholder="admin, operator" on:input={(event) => setListField("role_ids", inputValue(event))} />
    </label>
  {/if}

  {#if editorKind === "users" || editorKind === "service-accounts"}
    <TemporaryAccessPanel
      {editorSpec}
      {editorLoadedID}
      {editorJSON}
      bind:tempAccessRoleIDs
      bind:tempAccessPermissionIDs
      bind:tempAccessStartsAt
      bind:tempAccessExpiresIn
      bind:tempAccessExpiresAt
      {canGrantTemporaryAccess}
      {canRemoveTemporaryAccess}
      {temporaryAccessItems}
      {patchTemporaryAccess}
    />
  {/if}

  {#if editorKind === "users" && editorLoadedID}
    {#key editorLoadedID}
      <PasskeyPanel {apiBase} userID={editorLoadedID} />
    {/key}
  {/if}
</div>
