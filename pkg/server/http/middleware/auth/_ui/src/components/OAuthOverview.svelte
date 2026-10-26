<script lang="ts">
  import Instrument from "./ui/Instrument.svelte";
  import Section from "./ui/Section.svelte";
  import Serial from "./ui/Serial.svelte";
  import Switch from "./ui/Switch.svelte";
  import BreakSeal from "./ui/BreakSeal.svelte";
  import ListEntry from "./ui/ListEntry.svelte";
  import HeldEntry from "./ui/HeldEntry.svelte";
  import { docket, session } from "../lib/state/session.svelte";
  import { registry } from "../lib/state/registry.svelte";
  import {
    getSettingString,
    setSettingString,
    getSettingBool,
    setSettingBool,
    getSettingList,
    setSettingList,
    getSettingNumber,
    setSettingNumber,
    getSettingValue,
    setSettingValue,
    saveSettings,
  } from "../lib/state/settings.svelte";
  import { fieldText } from "../lib/records";

  /** Everything that decides what a token is and who may ask for one. */
  const namespaces = ["token", "oauth2", "authorize", "registration", "password", "passkey", "jwt"] as const;
  const schema = $derived(getSettingString("oauth2", ["schema"]) || "https");
  const publishedOAuth = $derived(session.info?.oauth2);
  const editedBaseURL = $derived(getSettingString("oauth2", ["base_url"]).trim());
  const baseURLPending = $derived(
    Boolean(publishedOAuth) && editedBaseURL !== (publishedOAuth?.configured_base_url ?? "").trim(),
  );
  const userVerification = $derived(getSettingString("passkey", ["user_verification"]) || "preferred");
  type PasskeySite = {
    name: string;
    rp_id: string;
    rp_display_name: string;
    origins: string[];
    related_origins?: string[];
  };
  const passkeySites = $derived((getSettingValue("passkey", ["sites"]) as PasskeySite[] | undefined) ?? []);

  function updatePasskeySite(index: number, patch: Partial<PasskeySite>) {
    const sites = passkeySites.map((site) => ({
      ...site,
      origins: [...(site.origins ?? [])],
      related_origins: [...(site.related_origins ?? [])],
    }));
    sites[index] = { ...sites[index], ...patch };
    setSettingValue("passkey", ["sites"], sites);
  }

  function addPasskeySite() {
    const taken = new Set(passkeySites.map((site) => site.name.trim()));
    let n = passkeySites.length + 1;
    while (taken.has(`site-${n}`)) n++;
    setSettingValue("passkey", ["sites"], [
      ...passkeySites,
      { name: `site-${n}`, rp_id: "", rp_display_name: "", origins: [], related_origins: [] },
    ]);
  }

  function removePasskeySite(index: number) {
    setSettingValue(
      "passkey",
      ["sites"],
      passkeySites.filter((_, siteIndex) => siteIndex !== index),
    );
  }

  type SiteIssues = { name?: string; rp_id?: string; origins?: string; related_origins?: string };

  function normalizeOrigin(origin: string) {
    return origin.trim().replace(/\/$/, "");
  }

  function isIP(host: string) {
    return /^\d{1,3}(\.\d{1,3}){3}$/.test(host) || host.includes(":");
  }

  /** Parse a scheme://host[:port] origin; a string is the problem with it. */
  function parseOrigin(raw: string): { origin: string; hostname: string } | string {
    const origin = normalizeOrigin(raw);
    let parsed: URL;
    try {
      parsed = new URL(origin);
    } catch {
      return `"${raw}" is not a valid origin.`;
    }
    if ((parsed.protocol !== "http:" && parsed.protocol !== "https:") || parsed.origin !== origin) {
      return `"${raw}" must be scheme://host[:port] with no path.`;
    }
    return { origin, hostname: parsed.hostname.replace(/^\[|\]$/g, "").toLowerCase() };
  }

  /**
   * Related origins sit on other registrable domains; anything inside the RP
   * ID belongs under Origins. The label limit (5) needs the public suffix list,
   * so the server checks that one.
   */
  function relatedOriginIssue(
    rpIDRaw: string,
    related: string[],
    owner: string,
    owners: Map<string, string>,
  ): string | undefined {
    if (related.length === 0) return undefined;
    const rpID = rpIDRaw.trim().toLowerCase();
    if (!rpID) return "Set an explicit relying party ID first — browsers fetch its /.well-known/webauthn.";
    if (isIP(rpID)) return "Related origins need a domain relying party ID, not an IP address.";

    for (const raw of related) {
      const parsed = parseOrigin(raw);
      if (typeof parsed === "string") return parsed;
      if (parsed.hostname === rpID || parsed.hostname.endsWith(`.${rpID}`)) {
        return `"${raw}" is already inside "${rpIDRaw.trim()}"; list it under origins instead.`;
      }
      const previous = owners.get(parsed.origin);
      if (previous !== undefined) return `"${raw}" is already assigned to "${previous}".`;
      owners.set(parsed.origin, owner);
    }
    return undefined;
  }

  const defaultRelatedOrigins = $derived(
    (getSettingValue("passkey", ["related_origins"]) as string[] | undefined) ?? [],
  );

  // Mirrors validatePasskeySettings so a bad profile is caught here instead of
  // failing mid-way through Commit all, after earlier namespaces were written.
  const passkeyValidation = $derived.by(() => {
    const issues: SiteIssues[] = passkeySites.map(() => ({}));
    const names = new Map<string, number>();
    const owners = new Map<string, string>();

    const defaultRelated = relatedOriginIssue(
      getSettingString("passkey", ["rp_id"]),
      defaultRelatedOrigins,
      "the default relying party",
      owners,
    );

    passkeySites.forEach((site, i) => {
      const name = (site.name ?? "").trim();
      if (!name) issues[i].name = "Required.";
      else if (names.has(name)) issues[i].name = `Duplicate of site ${names.get(name)! + 1}.`;
      else names.set(name, i);

      const rpID = (site.rp_id ?? "").trim().toLowerCase();
      if (!rpID) issues[i].rp_id = "Required.";

      const origins = site.origins ?? [];
      if (origins.length === 0) {
        issues[i].origins = "At least one origin is required.";
      }

      for (const raw of origins) {
        const parsed = parseOrigin(raw);
        if (typeof parsed === "string") {
          issues[i].origins = parsed;
          break;
        }
        const { origin, hostname } = parsed;
        if (rpID && hostname !== rpID && (isIP(rpID) || !hostname.endsWith(`.${rpID}`))) {
          issues[i].origins = `"${raw}" is outside relying party ID "${site.rp_id}". Use related origins for other domains.`;
          break;
        }
        const owner = owners.get(origin);
        if (owner !== undefined) {
          issues[i].origins = `"${raw}" is already assigned to "${owner}".`;
          break;
        }
        owners.set(origin, name || `site ${i + 1}`);
      }

      issues[i].related_origins = relatedOriginIssue(
        site.rp_id ?? "",
        site.related_origins ?? [],
        name || `site ${i + 1}`,
        owners,
      );
    });

    return { issues, defaultRelated };
  });

  const passkeySiteIssues = $derived(passkeyValidation.issues);
  const defaultRelatedIssue = $derived(passkeyValidation.defaultRelated);

  const passkeySitesInvalid = $derived(
    Boolean(defaultRelatedIssue) ||
      passkeySiteIssues.some((issue) => issue.name || issue.rp_id || issue.origins || issue.related_origins),
  );

  /** Where a browser on a related origin checks that this RP ID allows it. */
  function wellKnownURL(rpID: string) {
    return `https://${rpID.trim() || "{rp_id}"}/.well-known/webauthn`;
  }


  function enrollmentMethodEnabled(method: string) {
    const configured = getSettingList("passkey", ["enrollment", "methods"])
      .split(",")
      .map((value) => value.trim())
      .filter(Boolean);
    return configured.length === 0 || configured.includes(method);
  }

  function setEnrollmentMethod(method: string, enabled: boolean) {
    const configured = getSettingList("passkey", ["enrollment", "methods"])
      .split(",")
      .map((value) => value.trim())
      .filter(Boolean);
    const explicit = configured.length === 0 ? ["password", "code", "passkey"] : configured;
    const next = new Set(explicit);
    if (enabled) next.add(method);
    else next.delete(method);
    if (next.size === 0) return;
    setSettingList("passkey", ["enrollment", "methods"], [...next].join(", "));
  }

  // The published key is what verifiers actually see, not what is stored.
  const publishedKid = $derived(fieldText(registry.signingKey.kid));
  const publishedAlg = $derived(fieldText(registry.signingKey.alg));
  const publishedKty = $derived(fieldText(registry.signingKey.kty));
  const published = $derived(registry.jwks.length > 0);

  const references = $derived([
    { label: "JWKS", href: `${session.oauthIssuer}/certs` },
    {
      label: "OpenID configuration",
      href: `${session.oauthIssuer}/.well-known/openid-configuration`,
    },
  ]);

  async function purgeExpiredAuthRecords() {
    let deleted = 0;
    const ok = await session.run(async () => {
      const response = await session.request<{ deleted: number }>("maintenance/flow-codes/purge", {
        method: "POST",
        body: "{}",
      });
      deleted = response.payload.deleted;
    }, "Expired auth records could not be purged");

    if (ok) docket.commit(`${deleted} expired auth record${deleted === 1 ? "" : "s"} purged`);
  }
</script>

<Instrument
  title="OAuth2"
  note="Token lifetimes, the redirect surface for upstream code flows, which credentials the token endpoint accepts, and the key everything is signed with. Commit all applies every section on this page."
>
  {#snippet actions()}
    <button
      type="button"
      class="act act-primary"
      disabled={session.busy || passkeySitesInvalid}
      title={passkeySitesInvalid ? "Fix the highlighted passkey site fields first" : undefined}
      onclick={() => void saveSettings(namespaces)}
    >
      {session.busy ? "Committing…" : "Commit all"}
    </button>
  {/snippet}

  {#snippet custody()}
    <span class="stamp">
      Namespaces <span class="serial stamp-raw">token · oauth2 · authorize · registration · password · passkey · jwt</span>
    </span>
    <span class="serial stamp-raw">{session.oauthIssuer}/token</span>
  {/snippet}

  <Section title="Token lifetimes" note="How long an issued token stays good. Duration strings, e.g. 15m, 24h." first>
    <div class="grid gap-6 sm:grid-cols-2 xl:grid-cols-3">
      <div class="min-w-0">
        <label class="stamp block" for="token-lifetime">Access token lifetime</label>
        <input
          id="token-lifetime"
          class="entry serial mt-1.5"
          autocomplete="off"
          aria-describedby="token-lifetime-hint"
          placeholder="15m"
          value={getSettingString("token", ["token_lifetime"])}
          oninput={(event) => setSettingString("token", ["token_lifetime"], event.currentTarget.value)}
        />
        <p id="token-lifetime-hint" class="mt-1.5 text-[12px] leading-[1.5] text-muted">
          Already-issued tokens keep the lifetime they were minted with.
        </p>
      </div>

      <div class="min-w-0">
        <label class="stamp block" for="refresh-lifetime">Refresh idle lifetime</label>
        <input
          id="refresh-lifetime"
          class="entry serial mt-1.5"
          autocomplete="off"
          aria-describedby="refresh-lifetime-hint"
          placeholder="24h"
          value={getSettingString("token", ["refresh_lifetime"])}
          oninput={(event) => setSettingString("token", ["refresh_lifetime"], event.currentTarget.value)}
        />
        <p id="refresh-lifetime-hint" class="mt-1.5 text-[12px] leading-[1.5] text-muted">
          Standard sessions end this long after login. Default 24h.
        </p>
      </div>

      <div class="min-w-0">
        <label class="stamp block" for="remember-idle-lifetime">Remembered idle lifetime</label>
        <input
          id="remember-idle-lifetime"
          class="entry serial mt-1.5"
          autocomplete="off"
          aria-describedby="remember-idle-lifetime-hint"
          placeholder="168h"
          value={getSettingString("token", ["remember_idle_lifetime"])}
          oninput={(event) => setSettingString("token", ["remember_idle_lifetime"], event.currentTarget.value)}
        />
        <p id="remember-idle-lifetime-hint" class="mt-1.5 text-[12px] leading-[1.5] text-muted">
          Remembered sessions stay signed in while used at least once in this window. Default 168h (7 days).
        </p>
      </div>

      <div class="min-w-0">
        <label class="stamp block" for="refresh-absolute-lifetime">Maximum session lifetime</label>
        <input
          id="refresh-absolute-lifetime"
          class="entry serial mt-1.5"
          autocomplete="off"
          aria-describedby="refresh-absolute-lifetime-hint"
          placeholder="720h"
          value={getSettingString("token", ["refresh_absolute_lifetime"])}
          oninput={(event) =>
            setSettingString("token", ["refresh_absolute_lifetime"], event.currentTarget.value)}
        />
        <p id="refresh-absolute-lifetime-hint" class="mt-1.5 text-[12px] leading-[1.5] text-muted">
          Remembered sessions cannot slide past this boundary. Default 720h (30 days).
        </p>
      </div>

      <div class="min-w-0">
        <label class="stamp block" for="refresh-reuse-grace">Refresh reuse grace</label>
        <input
          id="refresh-reuse-grace"
          class="entry serial mt-1.5"
          autocomplete="off"
          aria-describedby="refresh-reuse-grace-hint"
          placeholder="off"
          value={getSettingString("token", ["refresh_reuse_grace"])}
          oninput={(event) => setSettingString("token", ["refresh_reuse_grace"], event.currentTarget.value)}
        />
        <p id="refresh-reuse-grace-hint" class="mt-1.5 text-[12px] leading-[1.5] text-muted">
          A rotated refresh token used again after this window revokes the whole session. Empty turns detection off. Suggested 30s.
        </p>
      </div>
    </div>

    <div class="mt-7 flex flex-wrap items-center justify-between gap-4 border-t border-rule pt-5">
      <p class="max-w-[68ch] text-[12px] leading-[1.6] text-muted">
        Expired authorization state and revoke records are removed hourly. Run the same indexed, batch-safe cleanup now.
      </p>
      <button type="button" class="act" disabled={session.busy} onclick={() => void purgeExpiredAuthRecords()}>
        Purge expired records
      </button>
    </div>
  </Section>

  <Section
    title="Code flow redirects"
    note="How this instance addresses itself when it sends a browser to an upstream provider and back. The same origin is the canonical token issuer."
  >
    <div class="grid gap-6 sm:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
      <div class="min-w-0">
        <label class="stamp block" for="oauth2-base-url">Base URL</label>
        <input
          id="oauth2-base-url"
          class="entry serial mt-1.5"
          autocomplete="off"
          aria-describedby="oauth2-base-url-hint"
          placeholder="https://auth.example.com"
          value={getSettingString("oauth2", ["base_url"])}
          oninput={(event) => setSettingString("oauth2", ["base_url"], event.currentTarget.value)}
        />
        <p id="oauth2-base-url-hint" class="mt-1.5 max-w-[62ch] text-[12px] leading-[1.5] text-muted">
          Empty means the address and token issuer are derived from each incoming request. Set it
          explicitly when this instance sits behind a proxy or serves sessions on more than one
          host. Instance host replacements apply to this origin before discovery, token issuance,
          and upstream callbacks are published.
        </p>
      </div>

      <div class="min-w-0">
        <label class="stamp block" for="oauth2-schema">Schema</label>
        <select
          id="oauth2-schema"
          class="entry mt-1.5"
          aria-describedby="oauth2-schema-hint"
          onchange={(event) => setSettingString("oauth2", ["schema"], event.currentTarget.value)}
        >
          <option value="https" selected={schema === "https"}>https</option>
          <option value="http" selected={schema === "http"}>http</option>
        </select>
        <p id="oauth2-schema-hint" class="mt-1.5 text-[12px] leading-[1.5] text-muted">
          Used when the address is derived rather than set above.
        </p>
      </div>
    </div>

    {#if publishedOAuth}
      <div class="mt-7 border-y border-rule py-5" aria-live="polite">
        <div class="flex flex-wrap items-baseline justify-between gap-x-6 gap-y-2">
          <div>
            <h3 class="text-[13.5px] font-semibold text-ink">Published OAuth addresses</h3>
            <p class="mt-1 max-w-[70ch] text-[12px] leading-[1.6] text-muted">
              The configured origin is rewritten for this instance before any public OAuth address is produced.
              Discovery, token claims, client registration, device flow and upstream callbacks all use the published origin.
            </p>
          </div>
          <div class="flex flex-wrap items-center justify-end gap-x-4 gap-y-2">
            {#if baseURLPending}
              <span class="stamp text-caution">Pending commit</span>
            {/if}
            {#if publishedOAuth.replacement_from && publishedOAuth.replacement_to}
              <span class="stamp-raw text-caution">
                Instance replacement {publishedOAuth.replacement_from} → {publishedOAuth.replacement_to}
              </span>
            {:else}
              <span class="stamp text-muted">No host replacement applied</span>
            {/if}
          </div>
        </div>

        <dl class="mt-5 grid gap-x-8 gap-y-4 sm:grid-cols-[10rem_minmax(0,1fr)]">
          <dt class="stamp">Edited base</dt>
          <dd class="serial min-w-0 break-all text-[12.5px] text-ink">
            {editedBaseURL || "Derived from request"}
          </dd>
          {#if baseURLPending}
            <dt class="stamp">Active configured base</dt>
            <dd class="serial min-w-0 break-all text-[12.5px] text-muted">
              {publishedOAuth.configured_base_url || "Derived from request"}
            </dd>
          {/if}
          <dt class="stamp">Effective runtime base</dt>
          <dd class="serial min-w-0 break-all text-[12.5px] text-ink">{publishedOAuth.effective_base_url}</dd>
          <dt class="stamp">Canonical issuer</dt>
          <dd class="serial min-w-0 break-all text-[12.5px] text-ink">{publishedOAuth.issuer_url}</dd>
          <dt class="stamp">OpenID discovery</dt>
          <dd class="serial min-w-0 break-all text-[12.5px] text-ink">{publishedOAuth.openid_configuration_url}</dd>
          <dt class="stamp">Token endpoint</dt>
          <dd class="serial min-w-0 break-all text-[12.5px] text-ink">{publishedOAuth.token_url}</dd>
          <dt class="stamp">JWKS endpoint</dt>
          <dd class="serial min-w-0 break-all text-[12.5px] text-ink">{publishedOAuth.jwks_url}</dd>
          <dt class="stamp">Upstream callback</dt>
          <dd class="serial min-w-0 break-all text-[12.5px] text-ink">{publishedOAuth.callback_url_pattern}</dd>
        </dl>

        {#if baseURLPending}
          <p class="mt-5 border-t border-rule pt-4 text-[12px] leading-[1.6] text-caution">
            The edited base has not changed the published OAuth surface yet. Commit all to apply it;
            the effective URLs above will then be recalculated with this instance's host replacement.
          </p>
        {/if}

        {#if publishedOAuth.replacement_from && publishedOAuth.replacement_to}
          <p class:border-t={!baseURLPending} class="mt-5 border-rule pt-4 text-[12px] leading-[1.6] text-muted">
            Clients must discover and validate <span class="serial text-ink">{publishedOAuth.issuer_url}</span>.
            Redirect URIs registered with upstream providers must use the replaced host. Tokens issued
            before this instance published the replacement carry the previous issuer and may require users to sign in again.
          </p>
        {/if}
      </div>
    {/if}

    <div class="mt-7">
      <Switch
        label="Skip TLS verification upstream"
        consequential
        hint="Certificates presented by upstream providers are no longer checked. Any host on the path can impersonate a provider — for a self-signed lab only, never in production."
        bind:checked={
          () => getSettingBool("oauth2", ["insecure_skip_verify"]),
          (value: boolean) => setSettingBool("oauth2", ["insecure_skip_verify"], value)
        }
      />
    </div>
  </Section>

  <Section
    title="Local authorization"
    note="The browser authorization endpoint holds a pending request, sends anonymous visitors to the login screen, then resumes at consent."
  >
    <div class="grid gap-6 sm:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
      <div class="min-w-0">
        <label class="stamp block" for="authorize-login-url">Anonymous login URL</label>
        <input
          id="authorize-login-url"
          class="entry serial mt-1.5"
          autocomplete="off"
          aria-describedby="authorize-login-url-hint"
          placeholder="/login/"
          value={getSettingString("authorize", ["login_url"])}
          oninput={(event) => setSettingString("authorize", ["login_url"], event.currentTarget.value)}
        />
        <p id="authorize-login-url-hint" class="mt-1.5 max-w-[70ch] text-[12px] leading-[1.5] text-muted">
          Same-origin paths and absolute URLs are accepted. Auth appends a
          <span class="serial">redirect_path</span> back to the pending consent request. Empty leaves
          anonymous visitors on an authorization error page.
        </p>
      </div>

      <div class="min-w-0">
        <label class="stamp block" for="authorize-flow-lifetime">Pending flow lifetime</label>
        <input
          id="authorize-flow-lifetime"
          class="entry serial mt-1.5"
          autocomplete="off"
          aria-describedby="authorize-flow-lifetime-hint"
          placeholder="10m"
          value={getSettingString("authorize", ["flow_lifetime"])}
          oninput={(event) =>
            setSettingString("authorize", ["flow_lifetime"], event.currentTarget.value)}
        />
        <p id="authorize-flow-lifetime-hint" class="mt-1.5 text-[12px] leading-[1.5] text-muted">
          How long a login may take before its consent request expires.
        </p>
      </div>
    </div>

    <div class="mt-7">
      <Switch
        label="Disable local authorization"
        hint="The /oauth2/authorize endpoint stops accepting new browser authorization requests. Pending requests are not deleted."
        bind:checked={
          () => getSettingBool("authorize", ["disabled"]),
          (value: boolean) => setSettingBool("authorize", ["disabled"], value)
        }
      />
    </div>
  </Section>

  <Section
    title="Dynamic client registration"
    note="RFC 7591 lets remote MCP clients such as OpenCode register a public PKCE client automatically. Registration is anonymous, so keep registrations short-lived and capped."
  >
    <div class="grid gap-6 sm:grid-cols-2 xl:grid-cols-3">
      <div class="min-w-0">
        <label class="stamp block" for="registration-client-lifetime">Client lifetime</label>
        <input
          id="registration-client-lifetime"
          class="entry serial mt-1.5"
          autocomplete="off"
          aria-describedby="registration-client-lifetime-hint"
          placeholder="720h"
          value={getSettingString("registration", ["client_lifetime"])}
          oninput={(event) =>
            setSettingString("registration", ["client_lifetime"], event.currentTarget.value)}
        />
        <p id="registration-client-lifetime-hint" class="mt-1.5 text-[12px] leading-[1.5] text-muted">
          A Go duration. Empty keeps dynamic clients indefinitely; 720h is 30 days.
        </p>
      </div>

      <div class="min-w-0">
        <label class="stamp block" for="registration-max-clients">Maximum clients</label>
        <HeldEntry
          id="registration-max-clients"
          type="number"
          min="1"
          placeholder="1000"
          describedby="registration-max-clients-hint"
          value={String(getSettingNumber("registration", ["max_clients"], 1000))}
          onvalue={(v) => setSettingNumber("registration", ["max_clients"], v)}
        />
        <p id="registration-max-clients-hint" class="mt-1.5 text-[12px] leading-[1.5] text-muted">
          Caps stored dynamic clients. Expired registrations are pruned before the limit is enforced.
        </p>
      </div>

      <div class="min-w-0 sm:col-span-2 xl:col-span-1">
        <label class="stamp block" for="registration-default-scope">Default scopes</label>
          <ListEntry
            id="registration-default-scope"
            placeholder="openid, profile"
            describedby="registration-default-scope-hint"
            value={getSettingValue("registration", ["default_scope"])}
            onchange={(items) => setSettingValue("registration", ["default_scope"], items)}
          />
        <p id="registration-default-scope-hint" class="mt-1.5 text-[12px] leading-[1.5] text-muted">
          Comma separated. Used only when a registering client does not request a scope.
        </p>
      </div>
    </div>

    <div class="mt-7 border-t border-rule pt-5">
      <Switch
        label="Accept dynamic client registration"
        consequential
        hint="Publishes registration_endpoint and accepts anonymous POST requests at /oauth2/register. OpenCode stores and reuses the returned client ID for this MCP server."
        bind:checked={
          () => getSettingBool("registration", ["enabled"]),
          (value: boolean) => setSettingBool("registration", ["enabled"], value)
        }
      />
    </div>
  </Section>

  <Section
    title="Password grant"
    note="Local users verify against the stored bcrypt password; non-local users bind against the active LDAP config. An unknown alias is created from LDAP on first login unless auto-register is off."
  >
    <div class="grid gap-6 sm:grid-cols-2">
      <Switch
        label="Disable the password grant"
        hint="The token endpoint rejects every username and password exchange, whatever the source."
        bind:checked={
          () => getSettingBool("password", ["disabled"]),
          (value: boolean) => setSettingBool("password", ["disabled"], value)
        }
      />

      <Switch
        label="Disable local passwords"
        hint="Users stored in this instance can no longer log in with their own bcrypt password."
        bind:checked={
          () => getSettingBool("password", ["local_disabled"]),
          (value: boolean) => setSettingBool("password", ["local_disabled"], value)
        }
      />

      <Switch
        label="Disable LDAP passwords"
        hint="Non-local users stop binding against the directory, which leaves them no way to log in."
        bind:checked={
          () => getSettingBool("password", ["ldap_disabled"]),
          (value: boolean) => setSettingBool("password", ["ldap_disabled"], value)
        }
      />

      <Switch
        label="Disable LDAP auto-register"
        hint="Only aliases already stored here may log in; a valid directory account that has never signed in is refused."
        bind:checked={
          () => getSettingBool("password", ["ldap_register_disabled"]),
          (value: boolean) => setSettingBool("password", ["ldap_register_disabled"], value)
        }
      />
    </div>
  </Section>

  <Section
    title="Passkey"
    note="Set the relying party explicitly when the login page is served from a different domain than this auth host — otherwise both are derived from the request."
  >
    <div class="grid gap-6 sm:grid-cols-2">
      <Switch
        label="Disable passkey login"
        hint="Enrolled authenticators stop being accepted; existing enrolments are kept."
        bind:checked={
          () => getSettingBool("passkey", ["disabled"]),
          (value: boolean) => setSettingBool("passkey", ["disabled"], value)
        }
      />

      <div class="min-w-0">
        <label class="stamp block" for="passkey-user-verification">User verification</label>
        <select
          id="passkey-user-verification"
          class="entry mt-1.5"
          aria-describedby="passkey-user-verification-hint"
          onchange={(event) =>
            setSettingString("passkey", ["user_verification"], event.currentTarget.value)}
        >
          <option value="preferred" selected={userVerification === "preferred"}>preferred</option>
          <option value="required" selected={userVerification === "required"}>required</option>
          <option value="discouraged" selected={userVerification === "discouraged"}>discouraged</option>
        </select>
        <p id="passkey-user-verification-hint" class="mt-1.5 text-[12px] leading-[1.5] text-muted">
          Whether the authenticator must prove a person is present, not just the key.
        </p>
      </div>

      <div class="min-w-0">
        <label class="stamp block" for="passkey-rp-id">Relying party ID</label>
        <input
          id="passkey-rp-id"
          class="entry serial mt-1.5"
          autocomplete="off"
          aria-describedby="passkey-rp-id-hint"
          placeholder="derived from request host"
          value={getSettingString("passkey", ["rp_id"])}
          oninput={(event) => setSettingString("passkey", ["rp_id"], event.currentTarget.value)}
        />
        <p id="passkey-rp-id-hint" class="mt-1.5 text-[12px] leading-[1.5] text-muted">
          The domain credentials are bound to. Changing it invalidates existing enrolments.
        </p>
      </div>

      <div class="min-w-0">
        <label class="stamp block" for="passkey-rp-name">Relying party display name</label>
        <input
          id="passkey-rp-name"
          class="entry mt-1.5"
          autocomplete="off"
          aria-describedby="passkey-rp-name-hint"
          placeholder="Turna Auth"
          value={getSettingString("passkey", ["rp_display_name"])}
          oninput={(event) =>
            setSettingString("passkey", ["rp_display_name"], event.currentTarget.value)}
        />
        <p id="passkey-rp-name-hint" class="mt-1.5 text-[12px] leading-[1.5] text-muted">
          What the operating system prompt calls this site.
        </p>
      </div>

      <div class="min-w-0 sm:col-span-2">
        <label class="stamp block" for="passkey-origins">Origins</label>
        <ListEntry
          id="passkey-origins"
          placeholder="https://app.example.com"
          describedby="passkey-origins-hint"
          value={getSettingValue("passkey", ["origins"])}
          onchange={(items) => setSettingValue("passkey", ["origins"], items)}
        />
        <p id="passkey-origins-hint" class="mt-1.5 max-w-[70ch] text-[12px] leading-[1.5] text-muted">
          Comma separated. Empty means the origin is taken from the request.
        </p>
      </div>

      <div class="min-w-0 sm:col-span-2">
        <label class="stamp block" for="passkey-related-origins">Related origins</label>
        <ListEntry
          id="passkey-related-origins"
          placeholder="https://example.co.uk, https://example-rewards.com"
          describedby="passkey-related-origins-hint"
          invalid={Boolean(defaultRelatedIssue)}
          value={defaultRelatedOrigins}
          onchange={(items) => setSettingValue("passkey", ["related_origins"], items)}
        />
        <p
          id="passkey-related-origins-hint"
          class="mt-1.5 max-w-[70ch] text-[12px] leading-[1.5] {defaultRelatedIssue ? 'text-seal' : 'text-muted'}"
        >
          {#if defaultRelatedIssue}
            {defaultRelatedIssue}
          {:else}
            Other domains that may use the same passkeys (WebAuthn Related Origin Requests). Published at
            <span class="serial">{wellKnownURL(getSettingString("passkey", ["rp_id"]))}</span>. Needs an
            explicit relying party ID, and at most 5 distinct domain names.
          {/if}
        </p>
      </div>

      <div class="sm:col-span-2 mt-2 border-t border-rule pt-7">
        <div class="flex flex-wrap items-start justify-between gap-4">
          <div>
            <h3 class="text-[13.5px] font-semibold text-ink">Site relying parties</h3>
            <p class="mt-1 max-w-[70ch] text-[12px] leading-[1.6] text-muted">
              Use separate RP IDs when unrelated domains share this Auth database. The request origin selects one profile; unmatched requests use the default fields above.
            </p>
          </div>
          <button type="button" class="act shrink-0" onclick={addPasskeySite}>Add site</button>
        </div>

        {#if passkeySitesInvalid}
          <p class="mt-4 text-[12px] leading-[1.6] text-seal" role="status">
            Commit all is held until every site has a unique name, a relying party ID and valid origins,
            and every related origin list is valid.
          </p>
        {/if}

        {#if passkeySites.length > 0}
          <div class="mt-5 border-y border-rule">
            <!-- Keyed by position: keying by an editable field remounts the row on every keystroke and drops focus. -->
            {#each passkeySites as site, index (index)}
              {@const issue = passkeySiteIssues[index] ?? {}}
              <div class="border-b border-rule py-5 last:border-b-0">
                <p class="stamp mb-4">Site {index + 1}</p>
                <div class="grid gap-6 sm:grid-cols-2">
                  <div class="min-w-0">
                    <label class="stamp block" for="passkey-site-name-{index}">Profile name</label>
                    <input
                      id="passkey-site-name-{index}"
                      class="entry serial mt-1.5"
                      autocomplete="off"
                      spellcheck="false"
                      placeholder="customer-portal"
                      aria-invalid={issue.name ? true : undefined}
                      aria-describedby={issue.name ? `passkey-site-name-${index}-issue` : undefined}
                      style={issue.name ? "border-bottom-color: var(--w-seal)" : undefined}
                      value={site.name}
                      oninput={(event) => updatePasskeySite(index, { name: event.currentTarget.value })}
                    />
                    {#if issue.name}
                      <p id="passkey-site-name-{index}-issue" class="mt-1.5 text-[12px] leading-[1.5] text-seal">
                        {issue.name}
                      </p>
                    {/if}
                  </div>
                  <div class="min-w-0">
                    <label class="stamp block" for="passkey-site-rp-{index}">Relying party ID</label>
                    <input
                      id="passkey-site-rp-{index}"
                      class="entry serial mt-1.5"
                      autocomplete="off"
                      spellcheck="false"
                      placeholder="customer.example"
                      aria-invalid={issue.rp_id ? true : undefined}
                      aria-describedby="passkey-site-rp-{index}-hint"
                      style={issue.rp_id ? "border-bottom-color: var(--w-seal)" : undefined}
                      value={site.rp_id}
                      oninput={(event) => updatePasskeySite(index, { rp_id: event.currentTarget.value })}
                    />
                    <p
                      id="passkey-site-rp-{index}-hint"
                      class="mt-1.5 text-[12px] leading-[1.5] {issue.rp_id ? 'text-seal' : 'text-muted'}"
                    >
                      {issue.rp_id ?? "Bare domain. Every origin below must be this host or a subdomain of it."}
                    </p>
                  </div>
                  <div class="min-w-0">
                    <label class="stamp block" for="passkey-site-display-{index}">Display name</label>
                    <input
                      id="passkey-site-display-{index}"
                      class="entry mt-1.5"
                      autocomplete="off"
                      placeholder="Turna Auth"
                      value={site.rp_display_name}
                      oninput={(event) => updatePasskeySite(index, { rp_display_name: event.currentTarget.value })}
                    />
                  </div>
                  <div class="min-w-0">
                    <label class="stamp block" for="passkey-site-origins-{index}">Origins</label>
                    <ListEntry
                      id="passkey-site-origins-{index}"
                      placeholder="https://customer.example"
                      describedby="passkey-site-origins-{index}-hint"
                      invalid={Boolean(issue.origins)}
                      value={site.origins ?? []}
                      onchange={(origins) => updatePasskeySite(index, { origins })}
                    />
                    <p
                      id="passkey-site-origins-{index}-hint"
                      class="mt-1.5 text-[12px] leading-[1.5] {issue.origins ? 'text-seal' : 'text-muted'}"
                    >
                      {issue.origins ?? "Comma separated, e.g. https://login.customer.example. No path."}
                    </p>
                  </div>
                  <div class="min-w-0 sm:col-span-2">
                    <label class="stamp block" for="passkey-site-related-{index}">Related origins</label>
                    <ListEntry
                      id="passkey-site-related-{index}"
                      placeholder="https://customer.de, https://customer-rewards.com"
                      describedby="passkey-site-related-{index}-hint"
                      invalid={Boolean(issue.related_origins)}
                      value={site.related_origins ?? []}
                      onchange={(related_origins) => updatePasskeySite(index, { related_origins })}
                    />
                    <p
                      id="passkey-site-related-{index}-hint"
                      class="mt-1.5 text-[12px] leading-[1.5] {issue.related_origins ? 'text-seal' : 'text-muted'}"
                    >
                      {#if issue.related_origins}
                        {issue.related_origins}
                      {:else}
                        Optional. Other domains that share this site's passkeys, published at
                        <span class="serial">{wellKnownURL(site.rp_id ?? "")}</span>.
                      {/if}
                    </p>
                  </div>
                </div>
                <div class="mt-4 flex justify-end">
                  <button
                    type="button"
                    class="act act-quiet text-seal"
                    aria-label="Remove site {index + 1}{site.name ? ` (${site.name})` : ''}"
                    onclick={() => removePasskeySite(index)}
                  >
                    Remove site
                  </button>
                </div>
              </div>
            {/each}
          </div>
        {/if}
      </div>

      <div class="sm:col-span-2 mt-2 border-t border-rule pt-7">
        <h3 class="text-[15px] font-semibold text-ink">Post-login passkey suggestion</h3>
        <p class="mt-1 max-w-[70ch] text-[12px] leading-[1.6] text-muted">
          Offer an authenticated user a real WebAuthn registration before the login page continues to its destination. The user can always skip it.
        </p>

        <div class="mt-5 grid gap-6 sm:grid-cols-2">
          <Switch
            label="Suggest passkey enrollment"
            hint="Disabled by default. When enabled, the login page asks this Auth instance whether it should show the optional step."
            bind:checked={
              () => getSettingBool("passkey", ["enrollment", "enabled"]),
              (value: boolean) => setSettingBool("passkey", ["enrollment", "enabled"], value)
            }
          />

          <Switch
            label="Prompt users with a passkey"
            hint="Normally only users with no registered passkey are prompted. Enable this to encourage another device credential."
            bind:checked={
              () => getSettingBool("passkey", ["enrollment", "prompt_when_registered"]),
              (value: boolean) =>
                setSettingBool("passkey", ["enrollment", "prompt_when_registered"], value)
            }
          />

          <div class="min-w-0">
            <p class="stamp">Eligible login methods</p>
            <div class="mt-2 flex flex-wrap gap-x-5 gap-y-2">
              {#each [
                ["password", "Password"],
                ["code", "Provider / code"],
                ["passkey", "Passkey"],
              ] as method}
                <label class="flex cursor-pointer items-center gap-2 text-[13px] text-ink">
                  <input
                    type="checkbox"
                    checked={enrollmentMethodEnabled(method[0])}
                    onchange={(event) => setEnrollmentMethod(method[0], event.currentTarget.checked)}
                  />
                  {method[1]}
                </label>
              {/each}
            </div>
            <p class="mt-2 text-[12px] leading-[1.5] text-muted">
              These describe how the current session was created, not which upstream provider supplied the identity.
            </p>
          </div>

          <div class="min-w-0">
            <label class="stamp block" for="passkey-enrollment-snooze">Remind after</label>
            <input
              id="passkey-enrollment-snooze"
              class="entry serial mt-1.5"
              autocomplete="off"
              placeholder="720h"
              value={getSettingString("passkey", ["enrollment", "snooze_duration"])}
              oninput={(event) =>
                setSettingString("passkey", ["enrollment", "snooze_duration"], event.currentTarget.value)}
            />
            <p class="mt-1.5 text-[12px] leading-[1.5] text-muted">
              Browser-local delay after “Not now”. Empty means 720h (30 days). Use <span class="serial">0s</span> to ask again on the next login.
            </p>
          </div>
        </div>
      </div>
    </div>
  </Section>

  <Section
    title="Signing key"
    note="The RSA key every access and refresh token is signed with. It is stored encrypted in the jwt namespace, generated on first start, and its public half is published through JWKS."
  >
    <div class="flex flex-wrap items-end gap-x-12 gap-y-6 border-b border-rule pb-7">
      <div class="min-w-0">
        <Serial value={publishedKid} size="md" />
        <p class="stamp mt-2">Published key id</p>
      </div>
      <div class="min-w-0">
        <Serial value={publishedAlg || "RS256"} size="md" />
        <p class="stamp mt-2">Algorithm</p>
      </div>
      <div class="min-w-0">
        <Serial value={publishedKty || "RSA"} size="md" />
        <p class="stamp mt-2">Key type</p>
      </div>

      <p class="max-w-[52ch] flex-1 basis-72 text-[12.5px] leading-[1.6] text-muted">
        {#if published}
          Read from the live JWKS document, not from the stored setting — this is what a verifier
          sees today.
        {:else}
          No key is published yet, so nothing can verify a token issued by this instance. Commit a
          private key below or rotate to have one generated.
        {/if}
      </p>
    </div>

    <div class="mt-7 grid gap-6">
      <div class="min-w-0 max-w-md">
        <label class="stamp block" for="jwt-kid">Key id (kid)</label>
        <input
          id="jwt-kid"
          class="entry serial mt-1.5"
          autocomplete="off"
          aria-describedby="jwt-kid-hint"
          placeholder="turna-auth-…"
          value={getSettingString("jwt", ["kid"])}
          oninput={(event) => setSettingString("jwt", ["kid"], event.currentTarget.value)}
        />
        <p id="jwt-kid-hint" class="mt-1.5 max-w-[62ch] text-[12px] leading-[1.5] text-muted">
          The name this key is published under. Verifiers select a key by it, so changing it makes
          tokens signed under the old name unverifiable.
        </p>
      </div>

      <div class="min-w-0">
        <label class="stamp block" for="jwt-private-key">RSA private key</label>
        <textarea
          id="jwt-private-key"
          class="exhibit mt-1.5 min-h-44"
          spellcheck="false"
          autocomplete="off"
          aria-describedby="jwt-private-key-hint"
          placeholder={"-----BEGIN PRIVATE KEY-----\n…"}
          value={getSettingString("jwt", ["private_key"])}
          oninput={(event) => setSettingString("jwt", ["private_key"], event.currentTarget.value)}
        ></textarea>
        <p id="jwt-private-key-hint" class="mt-2 max-w-[70ch] text-[12px] leading-[1.55] text-muted">
          PEM, PKCS#8 or PKCS#1. The public key is derived from it and republished. Committing a
          different key has the same effect as rotating: every outstanding access and refresh token
          stops verifying. Changes apply without a restart.
        </p>
      </div>
    </div>

    <h3 class="stamp stamp-ink mt-10 border-b border-rule pb-1.5">Published for verifiers</h3>
    <ul>
      {#each references as item (item.href)}
        <li class="border-b border-rule last:border-b-0">
          <a
            class="flex items-baseline gap-3 py-2.5 no-underline hover:underline"
            href={item.href}
            target="_blank"
            rel="noreferrer"
          >
            <span class="shrink-0 text-[13.5px] text-carbon">{item.label}</span>
            <span class="serial min-w-0 flex-1 truncate text-right text-[12px] text-muted">
              {item.href.replace(/^https?:\/\/[^/]+/, "")}
            </span>
          </a>
        </li>
      {/each}
    </ul>

    <h3 class="stamp stamp-ink mt-10 border-b border-rule pb-1.5">Rotation</h3>
    <div class="mt-4">
      <BreakSeal
        consequence="Rotating generates a new RSA signing key and republishes JWKS. Every outstanding access and refresh token is invalidated the moment it lands — every signed-in session and every machine holding a token must obtain a new one, and there is no undo."
        action="Rotate signing key"
        disabled={session.busy}
        onconfirm={() => void registry.rotateSigningKey()}
      />
      {#if session.busy}
        <p class="stamp mt-3" role="status" aria-live="polite">Rotating and republishing JWKS…</p>
      {/if}
    </div>
  </Section>
</Instrument>
