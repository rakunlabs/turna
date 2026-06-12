<script lang="ts">
  import { onMount } from "svelte";

  export let apiBase = "/auth/v1";

  type FlowID = "browser" | "provider" | "password" | "client" | "api";
  type View = FlowID | "endpoints";

  let selected: View = "browser";
  let origin = "";
  let providerID = "keycloak";
  let clientID = "web-app";
  let loginBase = "/login/";

  $: authBase = apiBase.replace(/\/v1$/, "");
  $: publicAuthBase = `${origin}${authBase}`;
  $: swaggerURL = `${authBase}/swagger/index.html`;
  $: openapiURL = `${authBase}/swagger/swagger.json`;
  $: loginProviderName = "turna";
  $: loginCallback = `${origin}${loginBase.replace(/\/?$/, "/")}auth/code/${loginProviderName}`;
  $: providerCallback = `${publicAuthBase}/oauth2/code/${providerID || "{provider}"}`;
  $: authStart = `${publicAuthBase}/oauth2/auth/${providerID || "{provider}"}`;
  $: tokenURL = `${publicAuthBase}/oauth2/token`;
  $: certURL = `${publicAuthBase}/oauth2/certs`;

  const flows: { id: FlowID; label: string; summary: string }[] = [
    {
      id: "browser",
      label: "Browser + session",
      summary: "Use Turna Auth as an OIDC provider; login middleware stores the cookie/session.",
    },
    {
      id: "provider",
      label: "Upstream provider",
      summary: "Connect Keycloak, GitHub, or another identity provider to Auth.",
    },
    {
      id: "password",
      label: "Password flow",
      summary: "Issue tokens by checking a local user password or LDAP credentials.",
    },
    {
      id: "client",
      label: "Service account",
      summary: "Use a service account secret for machine-to-machine tokens.",
    },
    {
      id: "api",
      label: "Protect API",
      summary: "Validate bearer tokens or session cookies with the session middleware.",
    },
  ];

  const reference = {
    id: "endpoints" as const,
    label: "API endpoints",
    summary: "OAuth2 and IAM endpoint reference for this instance — the same for every flow.",
  };

  $: selectedFlow = flows.find((flow) => flow.id === selected) ?? flows[0];
  $: browserSnippet = `server:
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
        middlewares: [session, app]`;

  $: passwordCurl = `curl -X POST ${tokenURL} \\
  -H 'Content-Type: application/x-www-form-urlencoded' \\
  -d 'grant_type=password' \\
  -d 'client_id=${clientID || "web-app"}' \\
  -d 'client_secret=change-me' \\
  -d 'username=user@example.com' \\
  -d 'password=secret' \\
  -d 'scope=openid profile'`;

  $: clientCurl = `curl -X POST ${tokenURL} \\
  -H 'Content-Type: application/x-www-form-urlencoded' \\
  -d 'grant_type=client_credentials' \\
  -d 'client_id=my-service' \\
  -d 'client_secret=change-me'`;

  $: codeCurl = `curl -X POST ${tokenURL} \\
  -H 'Content-Type: application/x-www-form-urlencoded' \\
  -d 'grant_type=authorization_code' \\
  -d 'client_id=${clientID || "web-app"}' \\
  -d 'client_secret=change-me' \\
  -d 'code=<code-from-redirect>'`;

  $: apiSnippet = `server:
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
        middlewares: [token_api_mode, session, api]`;

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

  onMount(() => {
    origin = window.location.origin;
  });
</script>

<div class="bg-panel">
  <div class="flex flex-col gap-3 border-b border-line p-4 md:flex-row md:items-end md:justify-between">
    <div>
      <p class="t-label text-fg">[ AUTH FLOW GUIDE ]</p>
      <h3 class="mt-2 font-display text-3xl uppercase leading-none tracking-tight md:text-4xl">Token & Session Paths</h3>
      <p class="mt-3 max-w-3xl text-xs leading-5 text-dim">
        OAuth2, LDAP password check, local users and service accounts all end at the same Turna token surface.
        Use the tabs below to choose the integration path.
      </p>
    </div>
    <div class="grid gap-2 text-[11px] uppercase tracking-[0.1em] text-dim md:min-w-[360px]">
      <label class="grid gap-1">
        <span class="t-label">Provider ID</span>
        <input class="field-t" bind:value={providerID} placeholder="keycloak" />
      </label>
      <label class="grid gap-1">
        <span class="t-label">OAuth client ID</span>
        <input class="field-t" bind:value={clientID} placeholder="web-app" />
      </label>
    </div>
  </div>

  <div class="grid gap-px bg-line p-px xl:grid-cols-[300px,minmax(0,1fr)]">
    <div class="grid content-start gap-px bg-line">
      <button
        class={`grid gap-1 p-3 text-left uppercase ${selected === reference.id ? "bg-alert text-white" : "bg-panel text-dim hover:text-fg"}`}
        on:click={() => (selected = reference.id)}
      >
        <span class="text-xs font-bold tracking-[0.12em]">{reference.label}</span>
        <span class="text-[10px] leading-4 tracking-[0.08em]">{reference.summary}</span>
      </button>

      <div class="bg-panel px-3 py-2">
        <span class="t-label">[ INTEGRATION FLOWS ]</span>
      </div>

      {#each flows as flow}
        <button
          class={`grid gap-1 p-3 text-left uppercase ${selected === flow.id ? "bg-alert text-white" : "bg-panel text-dim hover:text-fg"}`}
          on:click={() => (selected = flow.id)}
        >
          <span class="text-xs font-bold tracking-[0.12em]">{flow.label}</span>
          <span class="text-[10px] leading-4 tracking-[0.08em]">{flow.summary}</span>
        </button>
      {/each}
    </div>

    <div class="grid gap-px bg-line">
      {#if selected === "endpoints"}
        <div class="bg-panel p-4">
          <p class="t-label text-fg">{reference.label}</p>
          <p class="mt-2 max-w-3xl text-xs leading-5 text-dim">{reference.summary}</p>
        </div>

        <div class="flex flex-wrap items-center justify-between gap-3 bg-panel p-4">
          <div>
            <p class="t-label text-fg">[ OPENAPI / SWAGGER ]</p>
            <p class="mt-2 max-w-3xl text-xs leading-5 text-dim">Interactive API reference for every admin endpoint (admin access required).</p>
          </div>
          <div class="flex flex-wrap gap-px">
            <a class="btn-t-solid" href={swaggerURL} target="_blank" rel="noreferrer">[ OPEN SWAGGER UI ]</a>
            <a class="btn-t border-0 bg-crt" href={openapiURL} target="_blank" rel="noreferrer">OPENAPI JSON</a>
          </div>
        </div>

        <div class="bg-panel px-4 py-2">
          <span class="t-label text-fg">[ OAUTH2 ENDPOINTS ]</span>
        </div>
        <div class="grid gap-px bg-line md:grid-cols-2 xl:grid-cols-3">
          {#each endpoints as endpoint}
            <div class="bg-panel p-3">
              <p class="t-label text-fg">{endpoint.label}</p>
              <p class="mt-2 break-all text-[11px] leading-4 text-dim">{publicAuthBase}{endpoint.value}</p>
            </div>
          {/each}
        </div>

        <div class="bg-panel p-4">
          <p class="t-label text-fg">IAM compatibility surface</p>
          <p class="mt-2 max-w-3xl text-xs leading-5 text-dim">
            Auth keeps the legacy IAM shapes for users, service accounts, roles, permissions, LDAP maps, access checks, temporary grants, exports, role relations, and bulk permission workflows. Backup/restore is intentionally not listed here because it was tied to the old Badger store; Auth uses PostgreSQL migrations and version polling instead.
          </p>
        </div>

        <div class="bg-panel px-4 py-2">
          <span class="t-label text-fg">[ IAM ENDPOINTS ]</span>
        </div>
        <div class="grid gap-px bg-line md:grid-cols-2 xl:grid-cols-3">
          {#each iamEndpoints as endpoint}
            <div class="bg-panel p-3">
              <p class="t-label text-fg">{endpoint.label}</p>
              <p class="mt-2 break-all text-[11px] leading-4 text-dim">{publicAuthBase}{endpoint.value}</p>
            </div>
          {/each}
        </div>
      {:else}
        <div class="bg-panel p-4">
          <p class="t-label text-fg">{selectedFlow.label}</p>
          <p class="mt-2 text-xs leading-5 text-dim">{selectedFlow.summary}</p>
        </div>

        {#if selected === "browser"}
        <div class="grid gap-px bg-line md:grid-cols-2">
          <div class="bg-panel p-4">
            <p class="t-label text-fg">Required records</p>
            <p class="mt-3 text-xs leading-5 text-dim">Create an OAuth Provider named <span class="text-fg">{providerID}</span>, then create an OAuth Server Client named <span class="text-fg">{clientID}</span>.</p>
            <p class="mt-3 break-all text-xs leading-5 text-dim">Add this login callback to the client whitelist: <span class="text-fg">{loginCallback}</span></p>
          </div>
          <div class="bg-panel p-4">
            <p class="t-label text-fg">Session/login config</p>
            <pre class="mt-3 overflow-auto border border-line bg-crt p-3 text-[11px] leading-5 text-fg">{browserSnippet}</pre>
          </div>
        </div>
      {:else if selected === "provider"}
        <div class="grid gap-px bg-line md:grid-cols-2">
          <div class="bg-panel p-4">
            <p class="t-label text-fg">Provider setup</p>
            <p class="mt-3 break-all text-xs leading-5 text-dim">At the upstream IdP, set redirect/callback URL to <span class="text-fg">{providerCallback}</span>.</p>
            <p class="mt-3 break-all text-xs leading-5 text-dim">Turna starts the upstream flow at <span class="text-fg">{authStart}</span>.</p>
          </div>
          <div class="bg-panel p-4">
            <p class="t-label text-fg">Authorization code token exchange</p>
            <pre class="mt-3 overflow-auto border border-line bg-crt p-3 text-[11px] leading-5 text-fg">{codeCurl}</pre>
          </div>
        </div>
      {:else if selected === "password"}
        <div class="grid gap-px bg-line md:grid-cols-2">
          <div class="bg-panel p-4">
            <p class="t-label text-fg">How password flow resolves users</p>
            <p class="mt-3 text-xs leading-5 text-dim">If the user is local, Turna checks the stored bcrypt password in encrypted details. If the user is not local, Turna checks the active LDAP config.</p>
            <p class="mt-3 text-xs leading-5 text-dim">The OAuth Server Client validates <span class="text-fg">client_id</span> and <span class="text-fg">client_secret</span> before user password check.</p>
          </div>
          <div class="bg-panel p-4">
            <p class="t-label text-fg">Password grant request</p>
            <pre class="mt-3 overflow-auto border border-line bg-crt p-3 text-[11px] leading-5 text-fg">{passwordCurl}</pre>
          </div>
        </div>
      {:else if selected === "client"}
        <div class="grid gap-px bg-line md:grid-cols-2">
          <div class="bg-panel p-4">
            <p class="t-label text-fg">Service account setup</p>
            <p class="mt-3 text-xs leading-5 text-dim">Create a Service Account. Its alias is used as <span class="text-fg">client_id</span>, and <span class="text-fg">details.secret</span> is used as <span class="text-fg">client_secret</span>.</p>
            <p class="mt-3 text-xs leading-5 text-dim">Optional default scopes come from <span class="text-fg">details.scope</span>.</p>
          </div>
          <div class="bg-panel p-4">
            <p class="t-label text-fg">Client credentials request</p>
            <pre class="mt-3 overflow-auto border border-line bg-crt p-3 text-[11px] leading-5 text-fg">{clientCurl}</pre>
          </div>
        </div>
      {:else if selected === "api"}
        <div class="grid gap-px bg-line md:grid-cols-2">
          <div class="bg-panel p-4">
            <p class="t-label text-fg">API protection</p>
            <p class="mt-3 text-xs leading-5 text-dim">Put <span class="text-fg">session</span> before protected services. It validates bearer tokens or session cookies, then sets identity headers like <span class="text-fg">X-User</span>.</p>
            <p class="mt-3 text-xs leading-5 text-dim">API keys are static credentials: session validates <span class="text-fg">X-API-Key</span> against the database on every request and forwards <span class="text-fg">X-User: api-key:&lt;id&gt;</span>. mTLS still authenticates at <span class="text-fg">/oauth2/token</span>.</p>
            <p class="mt-3 text-xs leading-5 text-dim">For API routes, add <span class="text-fg">disable_redirect</span> so unauthenticated requests return 407 instead of browser redirect.</p>
          </div>
          <div class="bg-panel p-4">
            <p class="t-label text-fg">Protected API config</p>
            <pre class="mt-3 overflow-auto border border-line bg-crt p-3 text-[11px] leading-5 text-fg">{apiSnippet}</pre>
          </div>
        </div>
        {/if}
      {/if}
    </div>
  </div>
</div>
