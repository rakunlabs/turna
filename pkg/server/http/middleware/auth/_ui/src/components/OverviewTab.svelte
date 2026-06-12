<script lang="ts">
  import Metric from "./Metric.svelte";
  import type { Dashboard } from "../lib/api";

  export let dashboard: Dashboard | null = null;
  export let apiBase = "/auth/v1";
  export let busy = false;
  export let onLdapSync: () => void = () => {};

  $: oauthBase = apiBase.replace(/\/v1$/, "");
</script>

<div class="border-b border-line p-6 md:p-8">
  <p class="t-label">[ AUTH CONTROL PLANE ]</p>
  <h2 class="mt-3 font-display uppercase leading-[0.9] tracking-[-0.03em]" style="font-size: clamp(2.5rem, 6vw, 6.5rem);">
    AUTH<span class="text-alert">.</span>
  </h2>
  <p class="mt-4 max-w-2xl text-xs leading-5 text-dim">
    Manage IAM records, LDAP sync, OAuth2 clients, providers, and runtime settings from PostgreSQL.
  </p>
</div>

<div class="grid grid-cols-2 gap-px border-b border-line bg-line md:grid-cols-4">
  <Metric index="01" label="USERS" value={dashboard?.total_users ?? 0} />
  <Metric index="02" label="SERVICE ACCTS" value={dashboard?.total_service_accounts ?? 0} />
  <Metric index="03" label="ROLES" value={dashboard?.total_roles ?? 0} />
  <Metric index="04" label="PERMISSIONS" value={dashboard?.total_permissions ?? 0} />
</div>

<div class="grid gap-px bg-line">
  <div class="bg-crt">
    <div class="flex items-center justify-between border-b border-line px-4 py-2">
      <span class="t-label text-fg">[ QUICK ACTIONS ]</span>
      <span class="t-label">READY</span>
    </div>
    <div class="flex flex-wrap gap-px bg-line p-px">
      <button class="btn-t flex-1 border-0 bg-crt" disabled={busy} on:click={onLdapSync}>
        RUN LDAP SYNC
      </button>
      <a class="btn-t flex-1 border-0 bg-crt" href={`${oauthBase}/oauth2/.well-known/openid-configuration`} target="_blank" rel="noreferrer">
        OPENID CONFIG
      </a>
      <a class="btn-t flex-1 border-0 bg-crt" href={`${oauthBase}/oauth2/certs`} target="_blank" rel="noreferrer">
        JWKS
      </a>
      <a class="btn-t flex-1 border-0 bg-crt" href={`${oauthBase}/swagger/index.html`} target="_blank" rel="noreferrer">
        API DOCS
      </a>
    </div>
  </div>
</div>
