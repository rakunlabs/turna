<script lang="ts">
  import { onMount } from "svelte";

  import Instrument from "./ui/Instrument.svelte";
  import Entry from "./ui/Entry.svelte";
  import { docket, messageOf, session } from "../lib/state/session.svelte";

  /**
   * The one reading surface in this console. Everything else is a register or a
   * control; this is a document, so it gets a prose measure, a heading
   * hierarchy carried by weight, and every path set as a serial. The two
   * identifiers at the top are substituted into every snippet below, so what an
   * operator copies is already addressed to this instance.
   */
  let origin = $state("");
  let providerID = $state("keycloak");
  let clientID = $state("web-app");

  const loginProviderName = "turna";
  const loginBase = "/login/";

  const authBase = $derived(session.oauthBase);
  const publicAuthBase = $derived(`${origin}${authBase}`);
  const swaggerURL = $derived(`${authBase}/swagger/index.html`);
  const openapiURL = $derived(`${authBase}/swagger/swagger.json`);
  const loginCallback = $derived(
    `${origin}${loginBase.replace(/\/?$/, "/")}auth/code/${loginProviderName}`,
  );
  const providerCallback = $derived(`${publicAuthBase}/oauth2/code/${providerID || "{provider}"}`);
  const authStart = $derived(`${publicAuthBase}/oauth2/auth/${providerID || "{provider}"}`);
  const tokenURL = $derived(`${publicAuthBase}/oauth2/token`);
  const certURL = $derived(`${publicAuthBase}/oauth2/certs`);

  const contents = [
    { id: "guide-browser", label: "Browser session", summary: "Turna as the OIDC provider for your own app." },
    { id: "guide-provider", label: "Upstream provider", summary: "Delegate login to Keycloak, GitHub or another IdP." },
    { id: "guide-password", label: "Password grant", summary: "Trade a username and password for a token." },
    { id: "guide-client", label: "Service account", summary: "Machine-to-machine tokens from a client secret." },
    { id: "guide-api", label: "Protecting an API", summary: "Validate bearer tokens and cookies at the edge." },
    { id: "guide-oauth-endpoints", label: "OAuth2 endpoints", summary: "The published surface, same for every flow." },
    { id: "guide-iam-endpoints", label: "IAM endpoints", summary: "The administrative and compatibility surface." },
    { id: "guide-reference", label: "Interactive reference", summary: "Swagger UI and the OpenAPI document." },
  ];

  const browserSnippet = $derived(`server:
  http:
    middlewares:
      session:
        session:
          cookie_name: turna_auth
          store:
            active: file
            file:
              session_key: change-me
          provider:
            ${loginProviderName}:
              name: Turna Auth
              password_flow: true
              oauth2:
                client_id: ${clientID || "web-app"}
                client_secret: change-me
                scopes: [openid, profile, email]
                cert_url: ${certURL}
                token_url: ${tokenURL}
                auth_url: ${authStart}
          action:
            token:
              login_path: ${loginBase}
      login:
        login:
          session_middleware: session
          path:
            base: ${loginBase}
    routers:
      login:
        path: ${loginBase}*
        middlewares: [login]
      app:
        path: /*
        middlewares: [session, app]`);

  const passwordCurl = $derived(`curl -X POST ${tokenURL} \\
  -H 'Content-Type: application/x-www-form-urlencoded' \\
  -d 'grant_type=password' \\
  -d 'client_id=${clientID || "web-app"}' \\
  -d 'client_secret=change-me' \\
  -d 'username=user@example.com' \\
  -d 'password=secret' \\
  -d 'scope=openid profile'`);

  const clientCurl = $derived(`curl -X POST ${tokenURL} \\
  -H 'Content-Type: application/x-www-form-urlencoded' \\
  -d 'grant_type=client_credentials' \\
  -d 'client_id=my-service' \\
  -d 'client_secret=change-me'`);

  const codeCurl = $derived(`curl -X POST ${tokenURL} \\
  -H 'Content-Type: application/x-www-form-urlencoded' \\
  -d 'grant_type=authorization_code' \\
  -d 'client_id=${clientID || "web-app"}' \\
  -d 'client_secret=change-me' \\
  -d 'code=<code-from-redirect>'`);

  const apiSnippet = $derived(`server:
  http:
    middlewares:
      token_api_mode:
        set:
          values: [token_header, disable_redirect]
      session:
        session:
          provider:
            ${loginProviderName}:
              oauth2:
                client_id: ${clientID || "web-app"}
                cert_url: ${certURL}
                token_url: ${tokenURL}
    routers:
      api:
        path: /api/*
        middlewares: [token_api_mode, session, api]`);

  const endpoints = [
    { label: "Discovery", value: "/oauth2/.well-known/openid-configuration" },
    { label: "JWKS", value: "/oauth2/certs" },
    { label: "Token", value: "/oauth2/token" },
    { label: "Userinfo", value: "/oauth2/userinfo" },
    { label: "Start code flow", value: "/oauth2/auth/{provider}" },
    { label: "Provider callback", value: "/oauth2/code/{provider}" },
  ];

  const iamEndpoints = [
    { label: "Users", value: "/v1/users" },
    { label: "User export", value: "/v1/users/export" },
    { label: "User temporary access", value: "/v1/users/{id}/access" },
    { label: "Service accounts", value: "/v1/service-accounts" },
    { label: "Service account export", value: "/v1/service-accounts/export" },
    { label: "Roles", value: "/v1/roles" },
    { label: "Role relation dump", value: "/v1/roles/relation" },
    { label: "Role export", value: "/v1/roles/export" },
    { label: "Permissions", value: "/v1/permissions" },
    { label: "Permission bulk create", value: "/v1/permissions/bulk" },
    { label: "Permission keep sync", value: "/v1/permissions/keep" },
    { label: "Permission export", value: "/v1/permissions/export" },
    { label: "LDAP groups", value: "/v1/ldap/groups" },
    { label: "LDAP user", value: "/v1/ldap/users/{uid}" },
    { label: "LDAP sync", value: "/v1/ldap/sync" },
    { label: "LDAP maps", value: "/v1/ldap/maps" },
    { label: "Access check", value: "/v1/check" },
    { label: "Version", value: "/v1/version" },
    { label: "Reload sync", value: "/v1/sync" },
  ];

  /** In-page jumps, never anchors: the hash belongs to the router. */
  function jump(id: string) {
    document.getElementById(id)?.scrollIntoView({ block: "start", behavior: "smooth" });
  }

  async function copy(text: string) {
    try {
      await navigator.clipboard.writeText(text);
      docket.commit("Copied to the clipboard");
    } catch (err) {
      docket.reject(
        `${messageOf(err, "The clipboard is not available here")} — select the block and copy it manually.`,
      );
    }
  }

  onMount(() => {
    origin = window.location.origin;
  });
</script>

{#snippet code(caption: string, text: string)}
  <div class="mt-5">
    <div class="flex items-baseline justify-between gap-4 border-b border-rule pb-1.5">
      <h4 class="stamp stamp-ink">{caption}</h4>
      <button type="button" class="act act-quiet shrink-0" onclick={() => void copy(text)}>Copy</button>
    </div>
    <pre class="exhibit mt-3 overflow-x-auto">{text}</pre>
  </div>
{/snippet}

{#snippet fact(label: string, value: string)}
  <li class="grid gap-x-6 gap-y-1 border-b border-rule py-2.5 last:border-b-0 sm:grid-cols-[minmax(0,15rem)_minmax(0,1fr)] sm:items-baseline">
    <span class="text-[13px] text-ink">{label}</span>
    <span class="serial min-w-0 break-all text-[12.5px] text-muted">{value}</span>
  </li>
{/snippet}

<Instrument
  title="Integration guide"
  note="Every path into this instance — browser sessions, upstream providers, passwords, service accounts — ends at the same token surface. This is that surface, written out."
>
  {#snippet custody()}
    <span class="stamp">Addressed to <span class="serial stamp-raw">{publicAuthBase || authBase}</span></span>
    <span class="stamp">Reference, not configuration</span>
  {/snippet}

  <section>
    <h2 class="border-b border-rule pb-2 text-[1.05rem] font-bold tracking-[-0.015em] text-ink">
      How the pieces fit
    </h2>
    <p class="mt-4 max-w-[70ch] text-[13.5px] leading-[1.7] text-ink">
      Turna Auth is an OAuth2 and OpenID Connect provider with an IAM store behind it. Whoever the
      caller is — a person in a browser, a service holding a secret, a directory account binding
      against LDAP — the result is the same: a signed token from
      <span class="serial">/oauth2/token</span>, verifiable against
      <span class="serial">/oauth2/certs</span>.
    </p>
    <p class="mt-3 max-w-[70ch] text-[13.5px] leading-[1.7] text-muted">
      Authorisation is separate from authentication. A token says who the caller is; the permission
      graph decides what that identity may reach, and the access check page asks it directly.
    </p>

    <h3 class="mt-8 text-[13.5px] font-semibold text-ink">Contents</h3>
    <ul class="mt-2">
      {#each contents as item (item.id)}
        <li class="border-b border-rule last:border-b-0">
          <button
            type="button"
            class="group grid w-full gap-x-6 gap-y-0.5 py-2.5 text-left transition-colors sm:grid-cols-[minmax(0,15rem)_minmax(0,1fr)] sm:items-baseline"
            onclick={() => jump(item.id)}
          >
            <span class="text-[13.5px] text-carbon group-hover:underline">{item.label}</span>
            <span class="text-[12.5px] leading-[1.55] text-muted">{item.summary}</span>
          </button>
        </li>
      {/each}
    </ul>
  </section>

  <section class="mt-14">
    <h2 class="border-b border-rule pb-2 text-[1.05rem] font-bold tracking-[-0.015em] text-ink">
      Your identifiers
    </h2>
    <p class="mt-4 max-w-[70ch] text-[13.5px] leading-[1.7] text-muted">
      These two names are substituted into every snippet below. They are not stored — nothing on this
      page writes anything.
    </p>

    <div class="mt-6 grid max-w-2xl gap-6 sm:grid-cols-2">
      <Entry
        label="Provider ID"
        bind:value={providerID}
        placeholder="keycloak"
        mono
        hint="The record you create under OAuth providers."
      />
      <Entry
        label="OAuth client ID"
        bind:value={clientID}
        placeholder="web-app"
        mono
        hint="The record you create under Server clients."
      />
    </div>
  </section>

  <section id="guide-browser" class="mt-14 scroll-mt-6">
    <h2 class="border-b border-rule pb-2 text-[1.05rem] font-bold tracking-[-0.015em] text-ink">
      Browser session
    </h2>
    <p class="mt-4 max-w-[70ch] text-[13.5px] leading-[1.7] text-ink">
      Use Turna Auth as the OIDC provider for your own application. The login middleware serves the
      sign-in page, the session middleware holds the cookie and refreshes the token.
    </p>

    <h3 class="mt-8 text-[13.5px] font-semibold text-ink">Records this needs</h3>
    <p class="mt-2 max-w-[70ch] text-[13px] leading-[1.65] text-muted">
      An OAuth provider named <span class="serial">{providerID || "{provider}"}</span>, and an OAuth
      server client named <span class="serial">{clientID || "web-app"}</span>. Add the login callback
      below to that client's whitelist, or the redirect back from sign-in is refused.
    </p>
    <ul class="mt-4">
      {@render fact("Login callback to whitelist", loginCallback)}
      {@render fact("Token endpoint", tokenURL)}
      {@render fact("JWKS", certURL)}
    </ul>

    {@render code("Session and login middleware", browserSnippet)}
  </section>

  <section id="guide-provider" class="mt-14 scroll-mt-6">
    <h2 class="border-b border-rule pb-2 text-[1.05rem] font-bold tracking-[-0.015em] text-ink">
      Upstream provider
    </h2>
    <p class="mt-4 max-w-[70ch] text-[13.5px] leading-[1.7] text-ink">
      Delegate the actual sign-in to an existing identity provider. Turna starts the upstream flow,
      receives the code at its own callback, reads the claims and issues its own token from them.
    </p>
    <p class="mt-3 max-w-[70ch] text-[13px] leading-[1.65] text-muted">
      If the provider's <span class="serial">claim_mapping.register</span> is on, an unknown user is
      created as a non-local account from the claims. Roles named in
      <span class="serial">roles_claim</span> are synced into that user's sync roles, optionally
      through the LDAP group maps. Both are configured per provider.
    </p>

    <ul class="mt-6">
      {@render fact("Redirect URI to set at the IdP", providerCallback)}
      {@render fact("Where Turna starts the flow", authStart)}
    </ul>

    {@render code("Authorization code exchange", codeCurl)}
  </section>

  <section id="guide-password" class="mt-14 scroll-mt-6">
    <h2 class="border-b border-rule pb-2 text-[1.05rem] font-bold tracking-[-0.015em] text-ink">
      Password grant
    </h2>
    <p class="mt-4 max-w-[70ch] text-[13.5px] leading-[1.7] text-ink">
      The client is authenticated first: <span class="serial">client_id</span> and
      <span class="serial">client_secret</span> must belong to a registered server client or a
      service account. Only then is the user's password checked.
    </p>
    <p class="mt-3 max-w-[70ch] text-[13px] leading-[1.65] text-muted">
      A local user verifies against the bcrypt password held in their encrypted details. A non-local
      user binds against the active LDAP config. An alias that is not stored here at all is looked up
      in LDAP and created on first login, unless auto-register is off. If the user enrolled TOTP, a
      valid code is required to finish.
    </p>

    {@render code("Password grant request", passwordCurl)}
  </section>

  <section id="guide-client" class="mt-14 scroll-mt-6">
    <h2 class="border-b border-rule pb-2 text-[1.05rem] font-bold tracking-[-0.015em] text-ink">
      Service account
    </h2>
    <p class="mt-4 max-w-[70ch] text-[13.5px] leading-[1.7] text-ink">
      For machine-to-machine calls, create a service account. Its alias is the
      <span class="serial">client_id</span>; <span class="serial">details.secret</span> is the
      <span class="serial">client_secret</span>. Default scopes come from
      <span class="serial">details.scope</span>.
    </p>

    {@render code("Client credentials request", clientCurl)}
  </section>

  <section id="guide-api" class="mt-14 scroll-mt-6">
    <h2 class="border-b border-rule pb-2 text-[1.05rem] font-bold tracking-[-0.015em] text-ink">
      Protecting an API
    </h2>
    <p class="mt-4 max-w-[70ch] text-[13.5px] leading-[1.7] text-ink">
      Put the <span class="serial">session</span> middleware in front of anything protected. It
      validates a bearer token or a session cookie and forwards identity headers such as
      <span class="serial">X-User</span> to the service behind it.
    </p>
    <p class="mt-3 max-w-[70ch] text-[13px] leading-[1.65] text-muted">
      API keys are static credentials: session validates <span class="serial">X-API-Key</span>
      against the database on every request and forwards
      <span class="serial">X-User: api-key:&lt;id&gt;</span>. mTLS authenticates at
      <span class="serial">/oauth2/token</span> instead.
    </p>
    <p class="mt-3 max-w-[70ch] text-[13px] leading-[1.65] text-muted">
      On API routes add <span class="serial">disable_redirect</span>, so an unauthenticated request
      answers 407 rather than being redirected to a sign-in page a program cannot read.
    </p>

    {@render code("Protected API configuration", apiSnippet)}
  </section>

  <section id="guide-oauth-endpoints" class="mt-14 scroll-mt-6">
    <h2 class="border-b border-rule pb-2 text-[1.05rem] font-bold tracking-[-0.015em] text-ink">
      OAuth2 endpoints
    </h2>
    <p class="mt-4 max-w-[70ch] text-[13.5px] leading-[1.7] text-muted">
      Published by this instance and safe to hand to a client. The same for every flow above.
    </p>
    <ul class="mt-5">
      {#each endpoints as endpoint (endpoint.value)}
        {@render fact(endpoint.label, `${publicAuthBase}${endpoint.value}`)}
      {/each}
    </ul>
  </section>

  <section id="guide-iam-endpoints" class="mt-14 scroll-mt-6">
    <h2 class="border-b border-rule pb-2 text-[1.05rem] font-bold tracking-[-0.015em] text-ink">
      IAM endpoints
    </h2>
    <p class="mt-4 max-w-[70ch] text-[13.5px] leading-[1.7] text-muted">
      Auth keeps the legacy IAM shapes for users, service accounts, roles, permissions, LDAP maps,
      access checks, temporary grants, exports, role relations and bulk permission workflows. These
      require admin access.
    </p>
    <p class="mt-3 max-w-[70ch] text-[13px] leading-[1.65] text-muted">
      Backup and restore are deliberately absent: they were tied to the old Badger store. This build
      uses PostgreSQL migrations and version polling instead.
    </p>
    <ul class="mt-5">
      {#each iamEndpoints as endpoint (endpoint.value)}
        {@render fact(endpoint.label, `${publicAuthBase}${endpoint.value}`)}
      {/each}
    </ul>
  </section>

  <section id="guide-reference" class="mt-14 scroll-mt-6">
    <h2 class="border-b border-rule pb-2 text-[1.05rem] font-bold tracking-[-0.015em] text-ink">
      Interactive reference
    </h2>
    <p class="mt-4 max-w-[70ch] text-[13.5px] leading-[1.7] text-muted">
      Every admin endpoint with its request and response shapes, generated from this build. Admin
      access is required to call anything from it.
    </p>
    <div class="mt-5 flex flex-wrap gap-2">
      <a class="act act-primary no-underline" href={swaggerURL} target="_blank" rel="noreferrer">
        Open Swagger UI
      </a>
      <a class="act no-underline" href={openapiURL} target="_blank" rel="noreferrer">
        OpenAPI document
      </a>
    </div>
  </section>
</Instrument>
