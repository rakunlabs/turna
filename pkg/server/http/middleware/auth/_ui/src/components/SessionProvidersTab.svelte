<script lang="ts">
  import Instrument from "./ui/Instrument.svelte";
  import Section from "./ui/Section.svelte";
  import Switch from "./ui/Switch.svelte";
  import Seal from "./ui/Seal.svelte";

  import type { AnyRecord, SettingNamespace } from "../lib/api";
  import { session } from "../lib/state/session.svelte";
  import { saveSetting, setSettingRecord, settingRecord } from "../lib/state/settings.svelte";

  const ns: SettingNamespace = "session_providers";

  type Oauth2Row = {
    client_id: string;
    client_secret: string;
    scopes: string;
    cert_url: string;
    auth_url: string;
    token_url: string;
    logout_url: string;
    introspect_url: string;
    userinfo_url: string;
    revocation_url: string;
    passkey_url: string;
    api_key_url: string;
    signup_url: string;
    password_reset_url: string;
  };

  type ClaimHeaderRow = { id: number; header: string; claim: string };

  type ProviderRow = {
    id: number;
    key: string;
    /** Named group this provider belongs to; empty = the ungrouped list. */
    group: string;
    name: string;
    authMiddleware: string;
    passkey: boolean;
    passwordFlow: boolean;
    apiKey: boolean;
    apiKeyHeader: string;
    priority: number;
    hide: boolean;
    emailVerifyCheck: boolean;
    xUser: string;
    claimHeaders: ClaimHeaderRow[];
    oauth2: Oauth2Row;
    advancedOpen: boolean;
  };

  type VisibilityOverride = "inherit" | "show" | "hide";
  type InheritedProviderRow = {
    id: number;
    key: string;
    group: string;
    visibility: VisibilityOverride;
  };

  let uid = 0;
  let providers = $state<ProviderRow[]>([]);
  let inheritedProviders = $state<InheritedProviderRow[]>([]);
  let inheritGroup = $state("");
  let inheritProvider = $state("");
  let openGroups = $state<Record<string, boolean>>({});

  /**
   * The editable model is local: typing must not write through to the
   * canonical record, or every keystroke would be a pending change. Re-sync
   * only when the record itself is replaced — initial load and after commit.
   */
  let syncedFrom: AnyRecord | null = null;

  $effect(() => {
    const record = settingRecord(ns);
    if (record === syncedFrom) return;

    syncedFrom = record;
    syncFromRecord(record);
  });

  function asRecord(value: unknown): Record<string, unknown> {
    return value && typeof value === "object" && !Array.isArray(value)
      ? (value as Record<string, unknown>)
      : {};
  }

  function str(value: unknown): string {
    return typeof value === "string" ? value : "";
  }

  function emptyOauth2(): Oauth2Row {
    return {
      client_id: "",
      client_secret: "",
      scopes: "",
      cert_url: "",
      auth_url: "",
      token_url: "",
      logout_url: "",
      introspect_url: "",
      userinfo_url: "",
      revocation_url: "",
      passkey_url: "",
      api_key_url: "",
      signup_url: "",
      password_reset_url: "",
    };
  }

  function newProvider(key = "", group = ""): ProviderRow {
    return {
      id: uid++,
      key,
      group,
      name: "",
      authMiddleware: "",
      passkey: false,
      passwordFlow: false,
      apiKey: false,
      apiKeyHeader: "",
      priority: 0,
      hide: false,
      emailVerifyCheck: false,
      xUser: "",
      claimHeaders: [],
      oauth2: emptyOauth2(),
      advancedOpen: false,
    };
  }

  function syncFromRecord(value: unknown) {
    const rec = asRecord(value);

    const next: ProviderRow[] = [];
    const nextInherited: InheritedProviderRow[] = [];
    for (const [key, raw] of Object.entries(asRecord(rec.providers))) {
      next.push(rowFromRecord(key, "", raw));
    }
    for (const [groupName, groupRaw] of Object.entries(asRecord(rec.groups))) {
      const group = asRecord(groupRaw);
      for (const [key, raw] of Object.entries(asRecord(group.providers))) {
        next.push(rowFromRecord(key, groupName, raw));
      }
      for (const [key, raw] of Object.entries(asRecord(group.inherit))) {
        const override = asRecord(raw);
        nextInherited.push({
          id: uid++,
          key,
          group: groupName,
          visibility:
            typeof override.hide === "boolean" ? (override.hide ? "hide" : "show") : "inherit",
        });
      }
    }

    providers = next;
    inheritedProviders = nextInherited;
  }

  function rowFromRecord(key: string, group: string, raw: unknown): ProviderRow {
    const p = asRecord(raw);
    const oauth2Raw = asRecord(p.oauth2);
    const row = newProvider(key, group);

    row.name = str(p.name);
    row.authMiddleware = str(p.auth_middleware);
    row.passkey = Boolean(p.passkey);
    row.passwordFlow = Boolean(p.password_flow);
    row.apiKey = Boolean(p.api_key);
    row.apiKeyHeader = str(p.api_key_header);
    row.priority = typeof p.priority === "number" ? p.priority : 0;
    row.hide = Boolean(p.hide);
    row.emailVerifyCheck = Boolean(p.email_verify_check);
    row.xUser = Array.isArray(p.x_user) ? p.x_user.join(", ") : "";
    row.claimHeaders = Object.entries(asRecord(p.claim_header)).map(([header, claim]) => ({
      id: uid++,
      header,
      claim: str(claim),
    }));

    const oauth2 = emptyOauth2();
    for (const field of Object.keys(oauth2) as (keyof Oauth2Row)[]) {
      if (field === "scopes") {
        oauth2.scopes = Array.isArray(oauth2Raw.scopes) ? oauth2Raw.scopes.join(", ") : "";
        continue;
      }
      oauth2[field] = str(oauth2Raw[field]);
    }
    row.oauth2 = oauth2;

    return row;
  }

  function splitList(value: string): string[] {
    return value
      .split(",")
      .map((item) => item.trim())
      .filter(Boolean);
  }

  function buildRecord(): AnyRecord {
    const out: Record<string, unknown> = {};
    const groups: Record<
      string,
      { providers: Record<string, unknown>; inherit: Record<string, unknown> }
    > = {};

    function groupRecord(name: string) {
      return (groups[name] ??= { providers: {}, inherit: {} });
    }

    for (const row of providers) {
      const key = row.key.trim();
      if (!key) continue;

      const provider: Record<string, unknown> = {};
      if (row.name.trim()) provider.name = row.name.trim();
      if (row.authMiddleware.trim()) provider.auth_middleware = row.authMiddleware.trim();
      if (row.passkey) provider.passkey = true;
      if (row.passwordFlow) provider.password_flow = true;
      if (row.apiKey) provider.api_key = true;
      if (row.apiKeyHeader.trim()) provider.api_key_header = row.apiKeyHeader.trim();
      if (row.priority) provider.priority = row.priority;
      if (row.hide) provider.hide = true;
      if (row.emailVerifyCheck) provider.email_verify_check = true;

      const xUser = splitList(row.xUser);
      if (xUser.length) provider.x_user = xUser;

      const claimHeader: Record<string, string> = {};
      for (const item of row.claimHeaders) {
        const header = item.header.trim();
        if (!header) continue;
        claimHeader[header] = item.claim.trim();
      }
      if (Object.keys(claimHeader).length) provider.claim_header = claimHeader;

      const oauth2: Record<string, unknown> = {};
      for (const [field, value] of Object.entries(row.oauth2)) {
        if (field === "scopes") {
          const scopes = splitList(value);
          if (scopes.length) oauth2.scopes = scopes;
          continue;
        }
        if (value.trim()) oauth2[field] = value.trim();
      }
      if (Object.keys(oauth2).length) provider.oauth2 = oauth2;

      const group = row.group.trim();
      if (group) {
        groupRecord(group).providers[key] = provider;
      } else {
        out[key] = provider;
      }
    }

    for (const row of inheritedProviders) {
      const group = row.group.trim();
      const key = row.key.trim();
      if (!group || !key) continue;

      const override: Record<string, unknown> = {};
      if (row.visibility !== "inherit") override.hide = row.visibility === "hide";
      groupRecord(group).inherit[key] = override;
    }

    for (const group of Object.values(groups)) {
      if (!Object.keys(group.providers).length) delete (group as { providers?: unknown }).providers;
      if (!Object.keys(group.inherit).length) delete (group as { inherit?: unknown }).inherit;
    }

    const record: AnyRecord = { providers: out };
    if (Object.keys(groups).length) record.groups = groups;

    return record;
  }

  function addProvider() {
    providers = [...providers, newProvider()];
  }

  function removeProvider(id: number) {
    providers = providers.filter((row) => row.id !== id);
  }

  function addInheritedProvider() {
    const group = inheritGroup.trim();
    const key = inheritProvider.trim();
    if (
      !group ||
      !key ||
      !canonicalProviders.some((row) => row.key.trim() === key) ||
      inheritedProviders.some((row) => row.group.trim() === group && row.key === key)
    ) {
      return;
    }

    inheritedProviders = [
      ...inheritedProviders,
      { id: uid++, key, group, visibility: "inherit" },
    ];
    openGroups[group] = true;
  }

  function removeInheritedProvider(id: number) {
    inheritedProviders = inheritedProviders.filter((row) => row.id !== id);
  }

  function addClaimHeader(providerID: number) {
    providers = providers.map((row) =>
      row.id === providerID
        ? { ...row, claimHeaders: [...row.claimHeaders, { id: uid++, header: "", claim: "" }] }
        : row,
    );
  }

  function removeClaimHeader(providerID: number, headerID: number) {
    providers = providers.map((row) =>
      row.id === providerID
        ? { ...row, claimHeaders: row.claimHeaders.filter((item) => item.id !== headerID) }
        : row,
    );
  }

  async function save() {
    setSettingRecord(ns, buildRecord());
    await saveSetting(ns);
  }

  const named = $derived(providers.filter((row) => row.key.trim()).length);
  const canonicalProviders = $derived(
    providers
      .filter((row) => !row.group.trim() && row.key.trim())
      .sort((a, b) => a.key.localeCompare(b.key)),
  );

  const groupNames = $derived(
    [
      ...new Set([
        ...providers.map((row) => row.group.trim()).filter(Boolean),
        ...inheritedProviders.map((row) => row.group.trim()).filter(Boolean),
      ]),
    ].sort(),
  );

  const providerGroups = $derived.by(() => {
    const grouped = new Map<string, ProviderRow[]>();

    for (const provider of providers) {
      const group = provider.group.trim();
      grouped.set(group, [...(grouped.get(group) ?? []), provider]);
    }

    return [...grouped.entries()]
      .sort(([a], [b]) => {
        if (!a) return -1;
        if (!b) return 1;
        return a.localeCompare(b);
      })
      .map(([name, rows]) => ({ name, providers: rows }));
  });

  const inheritedProviderGroups = $derived.by(() => {
    const grouped = new Map<string, InheritedProviderRow[]>();
    for (const provider of inheritedProviders) {
      const group = provider.group.trim();
      grouped.set(group, [...(grouped.get(group) ?? []), provider]);
    }

    return [...grouped.entries()]
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([name, rows]) => ({ name, providers: rows }));
  });

  const inheritedProblems = $derived.by(() => {
    const canonical = new Set(canonicalProviders.map((row) => row.key.trim()));
    const seen = new Set<string>();
    const problems = new Set<string>();
    for (const row of inheritedProviders) {
      const key = row.key.trim();
      const group = row.group.trim();
      const identity = `${group}\u0000${key}`;
      if (!group || !canonical.has(key)) problems.add(`${group || "(no group)"}/${key || "(no provider)"}`);
      if (seen.has(identity)) problems.add(`${group}/${key}`);
      seen.add(identity);
    }
    return [...problems].sort();
  });

  const canAddInherited = $derived(
    Boolean(inheritGroup.trim() && inheritProvider) &&
      canonicalProviders.some((row) => row.key.trim() === inheritProvider) &&
      !inheritedProviders.some(
        (row) => row.group.trim() === inheritGroup.trim() && row.key === inheritProvider,
      ),
  );

  // Full provider definitions remain globally unique. Inherited references
  // are tracked separately and may intentionally repeat across groups.
  const duplicateKeys = $derived.by(() => {
    const seen = new Set<string>();
    const dups = new Set<string>();
    for (const row of providers) {
      const key = row.key.trim();
      if (!key) continue;
      if (seen.has(key)) dups.add(key);
      seen.add(key);
    }

    return [...dups].sort();
  });

  const standing = $derived.by(() => {
    if (named === 0) {
      return {
        state: "void" as const,
        label: "No providers",
        detail:
          "Session middlewares pulling this list get nothing beyond their static YAML providers until one is added and committed.",
      };
    }

    if (duplicateKeys.length) {
      return {
        state: "held" as const,
        label: "Duplicate keys",
        detail: `Full provider definitions must have unique keys; use inheritance to reuse a Default provider across groups. Duplicated: ${duplicateKeys.join(", ")}.`,
      };
    }

    if (inheritedProblems.length) {
      return {
        state: "held" as const,
        label: "Invalid inheritance",
        detail: `Inherited providers must reference a provider from Default and be unique inside their group. Check: ${inheritedProblems.join(", ")}.`,
      };
    }

    const grouped = groupNames.length
      ? ` ${groupNames.length} ${groupNames.length === 1 ? "group" : "groups"} (${groupNames.join(", ")}) can be pulled selectively with provider_source.group or the /session-providers/{group} endpoint.`
      : "";

    return {
      state: "endorsed" as const,
      label: "Published",
      detail: `${named} ${named === 1 ? "provider is" : "providers are"} published with ${inheritedProviders.length} inherited group ${inheritedProviders.length === 1 ? "entry" : "entries"}. Same-named static YAML providers are overridden.${grouped}`,
    };
  });

  const inProcessExample = `provider_source:\n  auth_middleware: auth  # this middleware's instance name\n  # group: internal      # optional: pull one named group only`;
  const remoteExample = `provider_source:\n  url: ${typeof window !== "undefined" ? window.location.origin : ""}${session.apiBase ?? ""}/session-providers\n  # append /{group} to the url to pull one named group only\n  ttl: 30s\n  headers:\n    X-API-Key: "<admin api key>"`;
</script>

<Instrument
  title="Session providers"
  note="The provider list of the session middleware, managed here instead of static YAML. A session middleware pulls it with provider_source — in-process by middleware name, or over HTTP from another instance — and applies changes without a restart."
  wide
>
  {#snippet actions()}
    <button type="button" class="act act-primary" disabled={session.busy} onclick={save}>
      {session.busy ? "Committing…" : "Commit"}
    </button>
  {/snippet}

  {#snippet custody()}
    <span class="stamp">Namespace <span class="serial stamp-raw">session_providers</span></span>
    <span class="stamp">{providers.length} {providers.length === 1 ? "provider" : "providers"}</span>
    <span class="serial stamp-raw">{session.apiBase ?? ""}/session-providers</span>
  {/snippet}

  <div class="max-w-[104ch] border border-rule bg-sheet px-4 py-3.5">
    <div class="flex flex-wrap items-baseline gap-x-5 gap-y-2">
      <span class="shrink-0"><Seal state={standing.state} label={standing.label} /></span>
      <p class="min-w-0 flex-1 basis-72 max-w-[70ch] text-[13px] leading-[1.6] text-ink">
        {standing.detail}
      </p>
    </div>
  </div>

  <div class="max-w-[104ch]">
    <Section
      title="Providers"
      note="The map key is the provider name the session middleware and login page use. Set auth_middleware to this instance's middleware name for in-process validation, or fill the OAuth2 endpoints for a remote identity provider. Named groups can be pulled selectively; providers without a group appear under Default and can be inherited into several groups. Full provider definitions must have unique keys. Edits are local until you commit."
    >
      {#snippet aside()}
        <button type="button" class="act act-quiet" onclick={addProvider}>Add provider</button>
      {/snippet}

      <datalist id="sp-group-names">
        {#each groupNames as name (name)}
          <option value={name}></option>
        {/each}
      </datalist>

      {#if providers.length === 0}
        <div class="border border-dashed border-rule px-6 py-14 text-center">
          <p class="text-[15px] font-semibold text-ink">No session providers</p>
          <p class="mx-auto mt-2 max-w-[56ch] text-[13px] leading-[1.6] text-muted">
            A provider named <span class="serial">turna</span> with
            <span class="serial">auth_middleware</span> pointing at this instance is the usual
            starting point; the login page lists it immediately.
          </p>
          <button type="button" class="act act-primary mt-6" onclick={addProvider}>
            Add provider
          </button>
        </div>
      {:else}
        <div class="grid gap-5">
          {#each providerGroups as providerGroup (providerGroup.name)}
            <details
              class="group border-t border-rule"
              open={openGroups[providerGroup.name] ?? false}
              ontoggle={(event) =>
                (openGroups[providerGroup.name] = event.currentTarget.open)}
            >
              <summary
                class="flex list-none items-center gap-3 px-1 py-3 text-ink transition-colors hover:bg-raised [&::-webkit-details-marker]:hidden"
              >
                <svg
                  class="h-3.5 w-3.5 shrink-0 transition-transform duration-150 group-open:rotate-90"
                  viewBox="0 0 12 12"
                  fill="none"
                  stroke="currentColor"
                  stroke-width="1.5"
                  aria-hidden="true"
                >
                  <path d="m4 2 4 4-4 4"></path>
                </svg>
                <span class="serial text-[13px] font-semibold">
                  {providerGroup.name || "Default"}
                </span>
                <span class="stamp">
                  {providerGroup.name ? "Named group" : "Ungrouped providers"}
                </span>
                <span class="serial ml-auto text-[12px] text-muted">
                  {providerGroup.providers.length}
                  {providerGroup.providers.length === 1 ? "provider" : "providers"}
                </span>
              </summary>

              <div class="grid gap-8 border-l-2 border-rule py-2 pl-3 sm:pl-5">
                {#each providerGroup.providers as provider (provider.id)}
                  <div class="border border-rule bg-sheet">
              <div class="flex flex-wrap items-end gap-x-6 gap-y-3 border-b border-rule px-4 py-3">
                <div class="min-w-0 flex-1 basis-52">
                  <label class="stamp block" for="sp-key-{provider.id}">Provider name · map key</label>
                  <input
                    id="sp-key-{provider.id}"
                    class="entry serial mt-1.5"
                    placeholder="turna"
                    autocomplete="off"
                    spellcheck="false"
                    bind:value={provider.key}
                  />
                </div>

                <div class="min-w-0 flex-1 basis-52">
                  <label class="stamp block" for="sp-name-{provider.id}">Display name</label>
                  <input
                    id="sp-name-{provider.id}"
                    class="entry mt-1.5"
                    placeholder="optional — shown on the login page"
                    autocomplete="off"
                    bind:value={provider.name}
                  />
                </div>

                <div class="min-w-0 flex-1 basis-40">
                  <label class="stamp block" for="sp-group-{provider.id}">Group</label>
                  <input
                    id="sp-group-{provider.id}"
                    class="entry serial mt-1.5"
                    placeholder="Default"
                    autocomplete="off"
                    spellcheck="false"
                    list="sp-group-names"
                    value={provider.group}
                    onchange={(event) => (provider.group = event.currentTarget.value)}
                  />
                </div>

                <div class="w-24">
                  <label class="stamp block" for="sp-priority-{provider.id}">Priority</label>
                  <input
                    id="sp-priority-{provider.id}"
                    class="entry serial mt-1.5"
                    type="number"
                    bind:value={provider.priority}
                  />
                </div>

                <button
                  type="button"
                  class="act act-quiet shrink-0 text-seal hover:bg-seal/10 hover:text-seal"
                  onclick={() => removeProvider(provider.id)}
                >
                  Remove
                </button>
              </div>

              <div class="grid gap-x-8 gap-y-5 border-b border-rule px-4 py-4 lg:grid-cols-2">
                <div class="min-w-0">
                  <label class="stamp block" for="sp-auth-{provider.id}">Auth middleware</label>
                  <input
                    id="sp-auth-{provider.id}"
                    class="entry serial mt-1.5"
                    placeholder="auth"
                    autocomplete="off"
                    spellcheck="false"
                    aria-describedby="sp-auth-hint-{provider.id}"
                    bind:value={provider.authMiddleware}
                  />
                  <p
                    id="sp-auth-hint-{provider.id}"
                    class="mt-1.5 max-w-[62ch] text-[12px] leading-[1.5] text-muted"
                  >
                    Instance name of an in-process auth middleware. Tokens are validated and
                    refreshed directly — no cert_url or token_url needed. Leave empty for a remote
                    provider and fill the OAuth2 endpoints instead.
                  </p>
                </div>

                <div class="min-w-0">
                  <label class="stamp block" for="sp-client-id-{provider.id}">OAuth2 client ID</label>
                  <input
                    id="sp-client-id-{provider.id}"
                    class="entry serial mt-1.5"
                    placeholder="turna-ui"
                    autocomplete="off"
                    spellcheck="false"
                    bind:value={provider.oauth2.client_id}
                  />
                  <label class="stamp mt-4 block" for="sp-client-secret-{provider.id}">
                    OAuth2 client secret
                  </label>
                  <input
                    id="sp-client-secret-{provider.id}"
                    class="entry serial mt-1.5"
                    placeholder="optional for public clients"
                    autocomplete="off"
                    spellcheck="false"
                    bind:value={provider.oauth2.client_secret}
                  />
                </div>
              </div>

              <div class="grid gap-x-10 gap-y-4 border-b border-rule px-4 py-4 sm:grid-cols-2 lg:grid-cols-3">
                <Switch label="Passkey login" bind:checked={provider.passkey} />
                <Switch label="Password flow" bind:checked={provider.passwordFlow} />
                <Switch label="API key auth" bind:checked={provider.apiKey} />
                <Switch label="Hide on login page" bind:checked={provider.hide} />
                <Switch label="Require verified email" bind:checked={provider.emailVerifyCheck} />
              </div>

              <div class="px-4 py-3">
                <button
                  type="button"
                  class="act act-quiet"
                  onclick={() => (provider.advancedOpen = !provider.advancedOpen)}
                >
                  {provider.advancedOpen ? "Hide" : "Show"} endpoints &amp; headers
                </button>

                {#if provider.advancedOpen}
                  <div class="mt-4 grid gap-x-8 gap-y-4 lg:grid-cols-2">
                    {#each [
                      ["scopes", "Scopes (comma separated)", "openid, profile, email"],
                      ["cert_url", "Cert URL (JWKS)", ""],
                      ["auth_url", "Auth URL", ""],
                      ["token_url", "Token URL", ""],
                      ["logout_url", "Logout URL", ""],
                      ["introspect_url", "Introspect URL", ""],
                      ["userinfo_url", "Userinfo URL", ""],
                      ["revocation_url", "Revocation URL", ""],
                      ["passkey_url", "Passkey URL (remote)", ""],
                      ["api_key_url", "API key URL (remote)", ""],
                      ["signup_url", "Signup URL (remote)", ""],
                      ["password_reset_url", "Password reset URL (remote)", ""],
                    ] as [field, label, placeholder] (field)}
                      <div class="min-w-0">
                        <label class="stamp block" for="sp-{field}-{provider.id}">{label}</label>
                        <input
                          id="sp-{field}-{provider.id}"
                          class="entry serial mt-1.5"
                          autocomplete="off"
                          spellcheck="false"
                          {placeholder}
                          bind:value={provider.oauth2[field as keyof Oauth2Row]}
                        />
                      </div>
                    {/each}

                    <div class="min-w-0">
                      <label class="stamp block" for="sp-xuser-{provider.id}">
                        X-User claims (comma separated)
                      </label>
                      <input
                        id="sp-xuser-{provider.id}"
                        class="entry serial mt-1.5"
                        placeholder="email, preferred_username"
                        autocomplete="off"
                        spellcheck="false"
                        bind:value={provider.xUser}
                      />
                    </div>

                    <div class="min-w-0">
                      <label class="stamp block" for="sp-apikey-header-{provider.id}">
                        API key header
                      </label>
                      <input
                        id="sp-apikey-header-{provider.id}"
                        class="entry serial mt-1.5"
                        placeholder="X-API-Key"
                        autocomplete="off"
                        spellcheck="false"
                        bind:value={provider.apiKeyHeader}
                      />
                    </div>
                  </div>

                  <div class="mt-5">
                    <span class="stamp block">Claim headers</span>
                    <p class="mt-1 max-w-[62ch] text-[12px] leading-[1.5] text-muted">
                      Header name → claim path set on every proxied request. An empty claim deletes
                      the header.
                    </p>

                    {#each provider.claimHeaders as item (item.id)}
                      <div class="mt-2 flex flex-wrap items-end gap-3">
                        <input
                          class="entry serial min-w-0 flex-1 basis-44"
                          placeholder="X-User-Email"
                          autocomplete="off"
                          spellcheck="false"
                          aria-label="Header name"
                          bind:value={item.header}
                        />
                        <input
                          class="entry serial min-w-0 flex-1 basis-44"
                          placeholder="email"
                          autocomplete="off"
                          spellcheck="false"
                          aria-label="Claim"
                          bind:value={item.claim}
                        />
                        <button
                          type="button"
                          class="act act-quiet text-seal hover:bg-seal/10 hover:text-seal"
                          onclick={() => removeClaimHeader(provider.id, item.id)}
                        >
                          Remove
                        </button>
                      </div>
                    {/each}

                    <button type="button" class="act mt-3" onclick={() => addClaimHeader(provider.id)}>
                      Add claim header
                    </button>
                  </div>
                {/if}
              </div>
                  </div>
                {/each}
              </div>
            </details>
          {/each}
        </div>
      {/if}
    </Section>

    <Section
      title="Inherited group providers"
      note="Add a provider from Default to any named group without copying its credentials or endpoints. The group may only override whether that provider appears on its login methods page."
    >
      <div class="grid gap-4 border border-rule bg-sheet p-4 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] sm:items-end">
        <div class="min-w-0">
          <label class="stamp block" for="sp-inherit-group">Target group</label>
          <input
            id="sp-inherit-group"
            class="entry serial mt-1.5"
            placeholder="internal"
            autocomplete="off"
            spellcheck="false"
            list="sp-group-names"
            bind:value={inheritGroup}
          />
        </div>
        <div class="min-w-0">
          <label class="stamp block" for="sp-inherit-provider">Provider from Default</label>
          <select
            id="sp-inherit-provider"
            class="entry serial mt-1.5"
            disabled={canonicalProviders.length === 0}
            bind:value={inheritProvider}
          >
            <option value="">Select provider</option>
            {#each canonicalProviders as provider (provider.id)}
              <option value={provider.key}>{provider.key}{provider.name ? ` · ${provider.name}` : ""}</option>
            {/each}
          </select>
        </div>
        <button
          type="button"
          class="act act-primary"
          disabled={!canAddInherited}
          onclick={addInheritedProvider}
        >
          Add to group
        </button>
      </div>

      {#if canonicalProviders.length === 0}
        <p class="mt-3 text-[12.5px] leading-[1.55] text-muted">
          Create a provider under <span class="serial">Default</span> first. Providers defined only
          inside a named group cannot be inherited.
        </p>
      {/if}

      {#if inheritedProviderGroups.length > 0}
        <div class="mt-6 grid gap-5">
          {#each inheritedProviderGroups as providerGroup (providerGroup.name)}
            <div class="border-t border-rule">
              <div class="flex items-baseline justify-between gap-4 px-1 py-3">
                <span class="serial text-[13px] font-semibold">{providerGroup.name}</span>
                <span class="stamp">Inherited view</span>
              </div>
              <div class="divide-y divide-rule border border-rule bg-sheet">
                {#each providerGroup.providers as provider (provider.id)}
                  <div class="grid gap-4 px-4 py-3 sm:grid-cols-[minmax(0,1fr)_minmax(12rem,0.8fr)_auto] sm:items-end">
                    <div class="min-w-0">
                      <span class="stamp block">Canonical provider</span>
                      <span class="serial mt-1.5 block break-all text-[13px] text-ink">
                        {provider.key}
                      </span>
                    </div>
                    <div class="min-w-0">
                      <label class="stamp block" for="sp-inherit-visibility-{provider.id}">
                        Login visibility
                      </label>
                      <select
                        id="sp-inherit-visibility-{provider.id}"
                        class="entry mt-1.5"
                        bind:value={provider.visibility}
                      >
                        <option value="inherit">Use Default setting</option>
                        <option value="show">Always show</option>
                        <option value="hide">Always hide</option>
                      </select>
                    </div>
                    <button
                      type="button"
                      class="act act-quiet text-seal hover:bg-seal/10 hover:text-seal"
                      onclick={() => removeInheritedProvider(provider.id)}
                    >
                      Remove
                    </button>
                  </div>
                {/each}
              </div>
            </div>
          {/each}
        </div>
      {/if}
    </Section>

    <Section
      title="Wire a session middleware to this list"
      note="Both modes overlay the static provider map; a provider committed here overrides a same-named YAML provider. Without a group selection an instance receives the shared list plus every group merged; with one it receives that group's own and inherited providers."
    >
      <div class="grid gap-8 lg:grid-cols-2">
        <div class="min-w-0">
          <p class="stamp stamp-ink">Same process</p>
          <pre class="exhibit mt-3 overflow-auto">{inProcessExample}</pre>
          <p class="mt-3 max-w-[62ch] text-[12.5px] leading-[1.55] text-muted">
            The session middleware reads this instance's cache directly and notices a commit on the
            next request — no polling interval, no HTTP.
          </p>
        </div>

        <div class="min-w-0">
          <p class="stamp stamp-ink">Another instance</p>
          <pre class="exhibit mt-3 overflow-auto">{remoteExample}</pre>
          <p class="mt-3 max-w-[62ch] text-[12.5px] leading-[1.55] text-muted">
            The endpoint is admin-protected because provider client secrets travel in the payload —
            authenticate the poll with an admin credential and keep it on a trusted network.
          </p>
        </div>
      </div>
    </Section>
  </div>
</Instrument>
