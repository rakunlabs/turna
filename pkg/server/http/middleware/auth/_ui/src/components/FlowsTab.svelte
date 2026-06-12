<script lang="ts">
  import type { SettingNamespace } from "../lib/api";

  export let settingsRevision = 0;
  export let getSettingBool: (namespace: SettingNamespace, path: string[], fallback?: boolean) => boolean = () => false;
  export let ldapActive = false;
  export let providerCount = 0;
  export let samlCount = 0;

  function sBool(_rev: number, ns: SettingNamespace, path: string[], fallback = false) {
    return getSettingBool(ns, path, fallback);
  }

  $: passwordDisabled = sBool(settingsRevision, "password", ["disabled"]);
  $: localDisabled = sBool(settingsRevision, "password", ["local_disabled"]);
  $: ldapDisabled = sBool(settingsRevision, "password", ["ldap_disabled"]);
  $: ldapRegisterDisabled = sBool(settingsRevision, "password", ["ldap_register_disabled"]);
  $: passkeyDisabled = sBool(settingsRevision, "passkey", ["disabled"]);
  $: totpDisabled = sBool(settingsRevision, "totp", ["disabled"]);
  $: signupEnabled = sBool(settingsRevision, "signup", ["enabled"]);
  $: emailVerification = sBool(settingsRevision, "signup", ["email_verification"], true);
  $: passwordReset = sBool(settingsRevision, "signup", ["password_reset"]);
  $: apiKeyDisabled = sBool(settingsRevision, "api_key", ["disabled"]);
  $: deviceDisabled = sBool(settingsRevision, "device", ["disabled"]);
  $: mtlsEnabled = sBool(settingsRevision, "mtls", ["enabled"]);
  $: tokenExchangeDisabled = sBool(settingsRevision, "token_exchange", ["disabled"]);

  // password grant outcome for an unknown (not-yet-stored) alias
  $: passwordUnknown = (() => {
    if (ldapDisabled || !ldapActive) return "off";
    if (ldapRegisterDisabled) return "known-only";
    return "auto-register";
  })();
</script>

<div class="grid gap-px bg-line p-px">
  <div class="bg-panel p-4">
    <p class="t-label text-fg">[ FLOWS ]</p>
    <h3 class="mt-2 font-display text-3xl uppercase leading-none tracking-tight md:text-4xl">What Happens Now</h3>
    <p class="mt-3 max-w-3xl text-xs leading-5 text-dim">
      Live walkthrough of each authentication path based on the <span class="text-fg">current</span> settings. Change a
      toggle on its page and this view updates after the next refresh.
    </p>
  </div>

  <div class="grid gap-px bg-line lg:grid-cols-2">
    <!-- PASSWORD LOGIN -->
    <div class="grid content-start gap-px bg-line">
      <div class="flex items-center justify-between bg-panel px-3 py-2">
        <span class="t-label text-fg">[ PASSWORD LOGIN ]</span>
        {#if passwordDisabled}
          <span class="text-[10px] font-bold uppercase tracking-[0.15em] text-alert">OFF</span>
        {:else}
          <span class="text-[10px] font-bold uppercase tracking-[0.15em] text-fg">ON</span>
        {/if}
      </div>
      <div class="bg-panel p-4 text-[11px] leading-5 text-dim">
        {#if passwordDisabled}
          <p class="text-alert">The <span class="text-fg">password</span> grant is disabled; username/password logins are rejected.</p>
        {:else}
          <ol class="grid list-decimal gap-1 pl-4">
            <li>Client <span class="text-fg">client_id/secret</span> is validated (registered OAuth client or service account).</li>
            <li>The alias is resolved to a user:</li>
            <ul class="grid list-disc gap-1 pl-4">
              <li>
                <span class="text-fg">Local user</span>:
                {#if localDisabled}<span class="text-alert">local passwords are disabled</span>{:else}bcrypt password is checked{/if}.
              </li>
              <li>
                <span class="text-fg">Unknown alias</span>:
                {#if passwordUnknown === "auto-register"}
                  found in LDAP &rarr; account is <span class="text-fg">auto-created (non-local)</span> and logged in.
                {:else if passwordUnknown === "known-only"}
                  <span class="text-alert">LDAP auto-register is off</span> &rarr; only already-stored users can log in.
                {:else}
                  {#if ldapDisabled}<span class="text-alert">LDAP passwords disabled</span>{:else}<span class="text-alert">no enabled LDAP config</span>{/if} &rarr; unknown aliases are rejected.
                {/if}
              </li>
            </ul>
            {#if !totpDisabled}
              <li>If the user enrolled <span class="text-fg">TOTP</span>, a valid code is required to finish.</li>
            {/if}
          </ol>
        {/if}
      </div>
    </div>

    <!-- SELF REGISTRATION -->
    <div class="grid content-start gap-px bg-line">
      <div class="flex items-center justify-between bg-panel px-3 py-2">
        <span class="t-label text-fg">[ SELF REGISTRATION ]</span>
        {#if signupEnabled}
          <span class="text-[10px] font-bold uppercase tracking-[0.15em] text-fg">ON</span>
        {:else}
          <span class="text-[10px] font-bold uppercase tracking-[0.15em] text-alert">OFF</span>
        {/if}
      </div>
      <div class="bg-panel p-4 text-[11px] leading-5 text-dim">
        {#if !signupEnabled}
          <p class="text-alert">Self-registration is disabled; <span class="text-fg">Create account</span> is hidden on the login page.</p>
        {:else}
          <ol class="grid list-decimal gap-1 pl-4">
            <li>User submits email + password at <span class="text-fg">/oauth2/signup</span> (valid client required).</li>
            {#if emailVerification}
              <li>A verification code/magic link is emailed; the <span class="text-fg">local</span> account is created only after confirmation.</li>
            {:else}
              <li>The <span class="text-fg">local</span> account is created immediately; duplicate addresses answer 409.</li>
            {/if}
            <li>Forgot-password over email is <span class={passwordReset ? "text-fg" : "text-alert"}>{passwordReset ? "enabled" : "disabled"}</span>.</li>
          </ol>
          <p class="mt-2">New accounts are <span class="text-fg">local</span> users and verify against the stored bcrypt password.</p>
        {/if}
      </div>
    </div>

    <!-- OAUTH2 / OIDC -->
    <div class="grid content-start gap-px bg-line">
      <div class="flex items-center justify-between bg-panel px-3 py-2">
        <span class="t-label text-fg">[ OAUTH2 / OIDC PROVIDER ]</span>
        <span class="text-[10px] font-bold uppercase tracking-[0.15em] {providerCount ? 'text-fg' : 'text-dim'}">{providerCount} CONFIGURED</span>
      </div>
      <div class="bg-panel p-4 text-[11px] leading-5 text-dim">
        {#if providerCount === 0}
          <p>No upstream OAuth providers configured. Add one under <span class="text-fg">OAUTH PROVIDERS</span>.</p>
        {:else}
          <ol class="grid list-decimal gap-1 pl-4">
            <li>User is redirected to the provider at <span class="text-fg">/oauth2/auth/&lbrace;provider&rbrace;</span> and returns with a code.</li>
            <li>Claims are read; if the provider's <span class="text-fg">claim_mapping.register</span> is on, an unknown user is <span class="text-fg">auto-created (non-local)</span> from the claims.</li>
            <li>Roles from <span class="text-fg">roles_claim</span> are synced into the user's sync roles (optionally via LDAP group maps).</li>
          </ol>
          <p class="mt-2">Register + role mapping is configured <span class="text-fg">per provider</span>.</p>
        {/if}
      </div>
    </div>

    <!-- METHODS STATUS -->
    <div class="grid content-start gap-px bg-line">
      <div class="bg-panel px-3 py-2">
        <span class="t-label text-fg">[ METHODS &amp; GRANTS ]</span>
      </div>
      <div class="grid grid-cols-2 gap-px bg-line">
        {#each [
          { label: "Passkey / WebAuthn", on: !passkeyDisabled },
          { label: "TOTP 2FA", on: !totpDisabled },
          { label: "API keys", on: !apiKeyDisabled },
          { label: "Device flow", on: !deviceDisabled },
          { label: "mTLS client certs", on: mtlsEnabled },
          { label: "Token exchange", on: !tokenExchangeDisabled },
          { label: "SAML providers", on: samlCount > 0 },
          { label: "LDAP", on: ldapActive },
        ] as item}
          <div class="flex items-center justify-between gap-2 bg-panel p-3">
            <span class="text-[11px] text-dim">{item.label}</span>
            {#if item.on}
              <span class="text-[10px] font-bold uppercase tracking-[0.12em] text-fg">[ ON ]</span>
            {:else}
              <span class="text-[10px] font-bold uppercase tracking-[0.12em] text-alert">[ OFF ]</span>
            {/if}
          </div>
        {/each}
      </div>
    </div>
  </div>
</div>
