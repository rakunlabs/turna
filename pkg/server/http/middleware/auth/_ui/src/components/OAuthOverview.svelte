<script lang="ts">
  import Instrument from "./ui/Instrument.svelte";
  import Section from "./ui/Section.svelte";
  import Serial from "./ui/Serial.svelte";
  import Switch from "./ui/Switch.svelte";
  import BreakSeal from "./ui/BreakSeal.svelte";
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
    saveSetting,
  } from "../lib/state/settings.svelte";
  import { fieldText } from "../lib/records";

  /**
   * Everything that decides what a token is and who may ask for one. Each
   * section commits its own namespace, because a namespace is what the API
   * writes — a single page-wide Commit would hide four separate writes behind
   * one button.
   */
  const schema = $derived(getSettingString("oauth2", ["schema"]) || "https");
  const userVerification = $derived(getSettingString("passkey", ["user_verification"]) || "preferred");

  // The published key is what verifiers actually see, not what is stored.
  const publishedKid = $derived(fieldText(registry.signingKey.kid));
  const publishedAlg = $derived(fieldText(registry.signingKey.alg));
  const publishedKty = $derived(fieldText(registry.signingKey.kty));
  const published = $derived(registry.jwks.length > 0);

  const references = $derived([
    { label: "JWKS", href: `${session.oauthBase}/oauth2/certs` },
    {
      label: "OpenID configuration",
      href: `${session.oauthBase}/oauth2/.well-known/openid-configuration`,
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

{#snippet commit(namespace: "token" | "oauth2" | "authorize" | "registration" | "password" | "passkey" | "jwt")}
  <button
    type="button"
    class="act act-primary"
    disabled={session.busy}
    onclick={() => void saveSetting(namespace)}
  >
    {session.busy ? "Committing…" : "Commit"}
  </button>
{/snippet}

<Instrument
  title="OAuth2"
  note="Token lifetimes, the redirect surface for upstream code flows, which credentials the token endpoint accepts, and the key everything is signed with."
>
  {#snippet custody()}
    <span class="stamp">
      Namespaces <span class="serial stamp-raw">token · oauth2 · authorize · registration · password · passkey · jwt</span>
    </span>
    <span class="serial stamp-raw">{session.oauthBase}/oauth2/token</span>
  {/snippet}

  <Section title="Token lifetimes" note="How long an issued token stays good. Duration strings, e.g. 15m, 24h." first>
    {#snippet aside()}{@render commit("token")}{/snippet}

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
          Remembered sessions slide by this window whenever a refresh succeeds. Standard sessions keep a fixed window.
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
    {#snippet aside()}{@render commit("oauth2")}{/snippet}

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
          host, so tokens issued on one host can be refreshed through another.
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
    {#snippet aside()}{@render commit("authorize")}{/snippet}

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
    {#snippet aside()}{@render commit("registration")}{/snippet}

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
        <input
          id="registration-max-clients"
          class="entry serial mt-1.5"
          type="number"
          min="1"
          aria-describedby="registration-max-clients-hint"
          value={getSettingNumber("registration", ["max_clients"], 1000)}
          oninput={(event) =>
            setSettingNumber("registration", ["max_clients"], event.currentTarget.value)}
        />
        <p id="registration-max-clients-hint" class="mt-1.5 text-[12px] leading-[1.5] text-muted">
          Caps stored dynamic clients. Expired registrations are pruned before the limit is enforced.
        </p>
      </div>

      <div class="min-w-0 sm:col-span-2 xl:col-span-1">
        <label class="stamp block" for="registration-default-scope">Default scopes</label>
        <input
          id="registration-default-scope"
          class="entry serial mt-1.5"
          autocomplete="off"
          aria-describedby="registration-default-scope-hint"
          placeholder="openid, profile"
          value={getSettingList("registration", ["default_scope"])}
          oninput={(event) =>
            setSettingList("registration", ["default_scope"], event.currentTarget.value)}
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
    {#snippet aside()}{@render commit("password")}{/snippet}

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
    {#snippet aside()}{@render commit("passkey")}{/snippet}

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
        <input
          id="passkey-origins"
          class="entry serial mt-1.5"
          autocomplete="off"
          aria-describedby="passkey-origins-hint"
          placeholder="https://app.example.com"
          value={getSettingList("passkey", ["origins"])}
          oninput={(event) => setSettingList("passkey", ["origins"], event.currentTarget.value)}
        />
        <p id="passkey-origins-hint" class="mt-1.5 max-w-[70ch] text-[12px] leading-[1.5] text-muted">
          Comma separated. Empty means the origin is taken from the request.
        </p>
      </div>
    </div>
  </Section>

  <Section
    title="Signing key"
    note="The RSA key every access and refresh token is signed with. It is stored encrypted in the jwt namespace, generated on first start, and its public half is published through JWKS."
  >
    {#snippet aside()}{@render commit("jwt")}{/snippet}

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
