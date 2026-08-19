<script lang="ts">
  import Metric from "./Metric.svelte";
  import type { Dashboard } from "../lib/api";

  export let dashboard: Dashboard | null = null;
  export let apiBase = "/auth/v1";
  export let busy = false;
  export let onLdapSync: () => void = () => {};

  $: oauthBase = apiBase.replace(/\/v1$/, "");
</script>

<div class="p-6 md:p-8">
  <h2 class="font-display text-2xl tracking-tight text-fg md:text-3xl">Overview</h2>
  <p class="mt-2 max-w-2xl text-sm leading-6 text-dim">
    Manage IAM records, LDAP sync, OAuth2 clients, providers, and runtime settings from PostgreSQL.
  </p>
</div>

<div class="grid grid-cols-2 gap-3 px-6 pb-6 md:grid-cols-4 md:px-8">
  <Metric label="Users" value={dashboard?.total_users ?? 0} />
  <Metric label="Service accounts" value={dashboard?.total_service_accounts ?? 0} />
  <Metric label="Roles" value={dashboard?.total_roles ?? 0} />
  <Metric label="Permissions" value={dashboard?.total_permissions ?? 0} />
</div>

<div class="px-6 pb-8 md:px-8">
  <div class="rounded-lg border border-line bg-surface p-4 shadow-sm">
    <span class="t-label">Quick actions</span>
    <div class="mt-3 flex flex-wrap gap-2">
      <button class="btn-t" disabled={busy} on:click={onLdapSync}>
        Run LDAP sync
      </button>
      <a class="btn-t" href={`${oauthBase}/oauth2/.well-known/openid-configuration`} target="_blank" rel="noreferrer">
        OpenID configuration
      </a>
      <a class="btn-t" href={`${oauthBase}/oauth2/certs`} target="_blank" rel="noreferrer">
        JWKS
      </a>
      <a class="btn-t" href={`${oauthBase}/swagger/index.html`} target="_blank" rel="noreferrer">
        API docs
      </a>
    </div>
  </div>
</div>
