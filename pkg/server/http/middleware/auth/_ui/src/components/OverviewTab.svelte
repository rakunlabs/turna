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
  <h2 class="mt-3 font-mono font-bold uppercase leading-[0.9] tracking-[-0.02em]" style="font-size: clamp(2.5rem, 6vw, 6.5rem);">
    <span class="crt-text">AUTH</span>
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

<style>
  /* CRT monitor scanline text — horizontal raster lines cut across the glyphs */
  .crt-text {
    position: relative;
    display: inline-block;
    text-shadow:
      0 0 1px rgb(var(--color-fg) / 0.35),
      0 0 18px rgb(var(--color-primary) / 0.2);
  }

  .crt-text::after {
    content: "";
    position: absolute;
    inset: -0.04em 0;
    pointer-events: none;
    background: repeating-linear-gradient(
      to bottom,
      rgb(var(--color-crt) / 0) 0,
      rgb(var(--color-crt) / 0) 2px,
      rgb(var(--color-crt) / 0.55) 2px,
      rgb(var(--color-crt) / 0.55) 4px
    );
    animation: crt-flicker 4s ease-in-out infinite;
  }

  @keyframes crt-flicker {
    0%,
    100% {
      opacity: 0.82;
    }
    50% {
      opacity: 1;
    }
  }

  @media (prefers-reduced-motion: reduce) {
    .crt-text::after {
      animation: none;
    }
  }
</style>
