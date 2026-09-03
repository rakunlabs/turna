<script lang="ts">
  import { onMount } from "svelte";

  import Instrument from "./ui/Instrument.svelte";
  import Section from "./ui/Section.svelte";
  import Seal from "./ui/Seal.svelte";
  import Entry from "./ui/Entry.svelte";
  import BreakSeal from "./ui/BreakSeal.svelte";
  import { isWebAuthnSupported, startRegistration } from "../lib/webauthn";
  import type { ServerCreationOptions } from "../lib/webauthn";
  import type { AnyRecord } from "../lib/api";
  import { fieldText, formatStamp } from "../lib/records";
  import { docket, messageOf, session } from "../lib/state/session.svelte";

  /**
   * The only page a person who is not an operator ever opens. It is written for
   * them: every control says what it does to their own sign-in, and the three
   * acts that cannot be taken back — dropping a passkey, dropping two-step
   * codes, replacing recovery codes — are sealed rather than confirmed.
   */

  type Me = {
    id: string;
    alias: string[];
    details: AnyRecord;
    roles: string[];
    permissions: string[];
    is_active: boolean;
    local: boolean;
    totp_enabled: boolean;
    passkey_count: number;
  };

  type PasskeyMeta = { id: string; name: string; created_at: string; sign_count: number };

  let me = $state<Me | null>(null);
  let loadError = $state("");

  // roles / permissions lists: long grants collapse behind a count so the page
  // stays readable for an account that carries hundreds of permissions.
  const grantCap = 15;
  let showAllRoles = $state(false);
  let showAllPermissions = $state(false);
  const visibleRoles = $derived(
    showAllRoles ? (me?.roles ?? []) : (me?.roles ?? []).slice(0, grantCap),
  );
  const visiblePermissions = $derived(
    showAllPermissions ? (me?.permissions ?? []) : (me?.permissions ?? []).slice(0, grantCap),
  );

  // self access check
  let checkHost = $state("");
  let checkPath = $state("/");
  let checkMethod = $state("GET");
  let checkRunning = $state(false);
  let checkVerdict = $state<{
    allowed: boolean;
    host: string;
    path: string;
    method: string;
  } | null>(null);

  const checkMethodOptions = ["GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"];

  // password
  let currentPassword = $state("");
  let newPassword = $state("");
  let confirmPassword = $state("");

  // totp
  let totpSecret = $state("");
  let totpURL = $state("");
  let totpCode = $state("");
  let recoveryCodes = $state<string[]>([]);

  // passkeys
  let passkeys = $state<PasskeyMeta[]>([]);
  let passkeyLabel = $state("");
  let pendingPasskey = $state("");

  // personal access keys (only when the instance allows self-service issuing)
  type OwnKeyMeta = {
    id: string;
    name: string;
    description: string;
    disabled: boolean;
    expires_at?: string;
    created_at: string;
    last_used_at?: string;
  };

  let ownKeys = $state<OwnKeyMeta[]>([]);
  let ownKeyName = $state("");
  let ownKeyDescription = $state("");
  let ownKeyEmail = $state("");
  let ownKeyExpires = $state("720h");
  let createdOwnKey = $state("");
  let pendingKeyRevoke = $state("");

  const ownKeyPresets = [
    { label: "24 hours", value: "24h" },
    { label: "7 days", value: "168h" },
    { label: "30 days", value: "720h" },
    { label: "90 days", value: "2160h" },
    { label: "No expiry", value: "" },
  ];

  const patEnabled = $derived(session.capabilities?.api_key_self_service === true);

  const webauthnReady = isWebAuthnSupported();

  const profileName = $derived(fieldText(me?.details?.name));
  const profileEmail = $derived(fieldText(me?.details?.email));
  const aliases = $derived((me?.alias ?? []).filter(Boolean).join(", "));
  const passwordMismatch = $derived(confirmPassword.length > 0 && newPassword !== confirmPassword);
  const totpOn = $derived(me?.totp_enabled === true);

  async function copyText(value: string, what: string) {
    try {
      await navigator.clipboard.writeText(value);
      docket.commit(`${what} copied to the clipboard.`);
    } catch {
      docket.reject(
        "This browser did not allow the page to use the clipboard. Select the text and copy it by hand.",
      );
    }
  }

  async function loadMe() {
    loadError = "";
    try {
      const res = await session.request<Me>("me");
      me = res.payload;
    } catch (err) {
      me = null;
      loadError = messageOf(err, "Cannot load account");
    }
  }

  async function loadPasskeys() {
    try {
      const res = await session.request<PasskeyMeta[]>("passkey/credentials");
      passkeys = res.payload ?? [];
    } catch {
      passkeys = [];
    }
  }

  async function loadAll() {
    await loadMe();
    if (me) {
      await loadPasskeys();
      if (patEnabled) await loadOwnKeys();
    }
  }

  // ////////////////////////////////////////////////////////////////
  // personal access keys

  async function loadOwnKeys() {
    try {
      const res = await session.request<OwnKeyMeta[]>("api-keys");
      ownKeys = res.payload ?? [];
    } catch {
      ownKeys = [];
    }
  }

  async function createOwnKey() {
    const ok = await session.run(async () => {
      const res = await session.request<{ id: string; key: string; expires_at?: string }>(
        "api-keys",
        {
          method: "POST",
          body: JSON.stringify({
            name: ownKeyName.trim(),
            description: ownKeyDescription.trim(),
            details: { email: ownKeyEmail.trim() },
            expires_in: ownKeyExpires.trim(),
          }),
        },
      );

      createdOwnKey = res.payload.key;
      ownKeyName = "";
      ownKeyDescription = "";
      ownKeyEmail = "";
      await loadOwnKeys();
    }, "Access key create failed");

    if (ok) {
      docket.commit("Access key issued. Copy it now — it is not stored and cannot be shown again.");
    }
  }

  async function revokeOwnKey(id: string) {
    pendingKeyRevoke = "";

    const ok = await session.run(async () => {
      await session.request(`api-keys/${encodeURIComponent(id)}`, { method: "DELETE" });
      await loadOwnKeys();
    }, "Access key revoke failed");

    if (ok) {
      docket.commit("Access key revoked. Anything still sending it is refused from now on.");
    }
  }

  function ownKeyLabel(key: OwnKeyMeta) {
    return key.name || key.id;
  }

  function ownKeyStanding(key: OwnKeyMeta): { label: string; state: "endorsed" | "broken" | "held" } {
    if (key.disabled) return { label: "Suspended", state: "held" };
    if (key.expires_at && new Date(key.expires_at).getTime() < Date.now()) {
      return { label: "Expired", state: "broken" };
    }

    return { label: "Active", state: "endorsed" };
  }

  // ////////////////////////////////////////////////////////////////
  // password

  async function changePassword() {
    if (newPassword !== confirmPassword) {
      docket.reject("The two new-password boxes do not match. Retype both and try again.");
      return;
    }

    const ok = await session.run(async () => {
      await session.request("me/password", {
        method: "POST",
        body: JSON.stringify({ current_password: currentPassword, new_password: newPassword }),
      });
    }, "Password update failed");

    if (!ok) return;

    currentPassword = "";
    newPassword = "";
    confirmPassword = "";
    docket.commit("Password changed. Use the new one the next time you sign in.");
  }

  // ////////////////////////////////////////////////////////////////
  // totp

  async function totpRegister() {
    const ok = await session.run(async () => {
      const res = await session.request<{ secret: string; url: string }>("totp/register", {
        method: "POST",
        body: "{}",
      });

      totpSecret = res.payload.secret;
      totpURL = res.payload.url;
      recoveryCodes = [];
    }, "TOTP register failed");

    if (ok) docket.commit("Add the secret to your authenticator app, then enter a code to finish.");
  }

  async function totpConfirm() {
    const ok = await session.run(async () => {
      const res = await session.request<{ recovery_codes?: string[] }>("totp/confirm", {
        method: "POST",
        body: JSON.stringify({ code: totpCode.trim() }),
      });

      recoveryCodes = res.payload.recovery_codes ?? [];
      totpSecret = "";
      totpURL = "";
      totpCode = "";
      await loadMe();
    }, "TOTP confirm failed");

    if (ok) docket.commit("Two-step codes are on. Write down the recovery codes before you leave.");
  }

  async function totpRecovery() {
    const ok = await session.run(async () => {
      const res = await session.request<{ recovery_codes?: string[] }>("totp/recovery", {
        method: "POST",
        body: "{}",
      });

      recoveryCodes = res.payload.recovery_codes ?? [];
    }, "Recovery regenerate failed");

    if (ok) docket.commit("New recovery codes issued. The previous set no longer works.");
  }

  async function totpDisable() {
    const ok = await session.run(async () => {
      await session.request("totp", { method: "DELETE" });
      totpSecret = "";
      totpURL = "";
      recoveryCodes = [];
      await loadMe();
    }, "TOTP disable failed");

    if (ok) docket.commit("Two-step codes are off. Signing in now needs your password only.");
  }

  // ////////////////////////////////////////////////////////////////
  // passkeys

  async function passkeyRegister() {
    const ok = await session.run(async () => {
      const begin = await session.request<{ session_id: string; options: ServerCreationOptions }>(
        "passkey/register",
        { method: "POST", body: "{}" },
      );

      const credential = await startRegistration(begin.payload.options);

      await session.request("passkey/register", {
        method: "POST",
        body: JSON.stringify({
          session_id: begin.payload.session_id,
          name: passkeyLabel.trim(),
          credential,
        }),
      });

      passkeyLabel = "";
      await Promise.all([loadPasskeys(), loadMe()]);
    }, "Passkey register failed");

    if (ok) docket.commit("Passkey registered. You can now sign in with this device.");
  }

  async function passkeyDelete(id: string) {
    pendingPasskey = "";

    const ok = await session.run(async () => {
      await session.request(`passkey/credentials/${encodeURIComponent(id)}`, { method: "DELETE" });
      await Promise.all([loadPasskeys(), loadMe()]);
    }, "Passkey delete failed");

    if (ok) docket.commit("Passkey removed. That device can no longer sign you in.");
  }

  function passkeyName(passkey: PasskeyMeta) {
    return passkey.name || passkey.id;
  }

  // ////////////////////////////////////////////////////////////////
  // self access check

  async function runSelfCheck() {
    checkRunning = true;
    docket.clearRejections();

    const terms = { host: checkHost.trim(), path: checkPath.trim(), method: checkMethod.trim() };

    try {
      const body = await session.raw<{ allowed: boolean }>("me/check", {
        method: "POST",
        body: JSON.stringify(terms),
      });

      checkVerdict = { allowed: Boolean(body.allowed), ...terms };
    } catch (err) {
      checkVerdict = null;
      docket.reject(messageOf(err, "Access check failed"));
    } finally {
      checkRunning = false;
    }
  }

  onMount(() => {
    void loadAll();
  });
</script>

{#snippet copyGlyph()}
  <svg width="13" height="13" viewBox="0 0 16 16" fill="none" aria-hidden="true">
    <rect x="5.6" y="5.6" width="8.6" height="8.6" stroke="currentColor" stroke-width="1.4" />
    <path
      d="M10.9 5.6V2.6a.8.8 0 0 0-.8-.8H2.6a.8.8 0 0 0-.8.8v7.5a.8.8 0 0 0 .8.8h3"
      stroke="currentColor"
      stroke-width="1.4"
      stroke-linecap="square"
    />
  </svg>
{/snippet}

<Instrument
  title="Your account"
  note="Everything here belongs to you alone: how you sign in, and what this system currently knows about you. Changes take effect immediately."
>
  {#snippet custody()}
    {#if me}
      <span class="stamp">Signed in as <span class="serial stamp-raw">{me.id}</span></span>
      <Seal
        state={me.is_active ? "endorsed" : "broken"}
        label={me.is_active ? "Active" : "Suspended"}
      />
      <span class="stamp">{me.local ? "Local account" : "Directory account"}</span>
    {:else}
      <span class="stamp">Reading your record…</span>
    {/if}
  {/snippet}

  {#if !me}
    <div class="border border-dashed border-rule px-6 py-14 text-center">
      <p class="text-[15px] font-semibold text-ink">
        {loadError ? "Your account could not be read" : "Reading your account…"}
      </p>
      {#if loadError}
        <p class="mx-auto mt-2 max-w-[62ch] text-[13px] leading-[1.6] text-muted">{loadError}</p>
        <p class="mx-auto mt-3 max-w-[62ch] text-[13px] leading-[1.6] text-muted">
          This page needs a signed-in session. The request reached the server without an
          <span class="serial">X-User</span> header, which usually means the session middleware is not
          in front of these routes. Ask whoever runs this service to check that.
        </p>
        <button type="button" class="act mt-6" disabled={session.busy} onclick={() => void loadAll()}>
          Try again
        </button>
      {/if}
    </div>
  {:else}
    <Section title="Who you are" first>
      <dl class="divide-y divide-rule border-y border-rule">
        <div class="grid gap-x-4 gap-y-0.5 py-2.5 sm:grid-cols-[11rem_minmax(0,1fr)]">
          <dt class="stamp sm:pt-[3px]">Account ID</dt>
          <dd class="serial min-w-0 break-all text-[13px] text-ink">{me.id}</dd>
        </div>

        <div class="grid gap-x-4 gap-y-0.5 py-2.5 sm:grid-cols-[11rem_minmax(0,1fr)]">
          <dt class="stamp sm:pt-[3px]">Sign-in names</dt>
          <dd class="serial min-w-0 break-all text-[13px] text-ink">{aliases || "—"}</dd>
        </div>

        {#if profileName}
          <div class="grid gap-x-4 gap-y-0.5 py-2.5 sm:grid-cols-[11rem_minmax(0,1fr)]">
            <dt class="stamp sm:pt-[3px]">Name</dt>
            <dd class="min-w-0 break-words text-[13px] text-ink">{profileName}</dd>
          </div>
        {/if}

        {#if profileEmail}
          <div class="grid gap-x-4 gap-y-0.5 py-2.5 sm:grid-cols-[11rem_minmax(0,1fr)]">
            <dt class="stamp sm:pt-[3px]">Email</dt>
            <dd class="min-w-0 break-all text-[13px] text-ink">{profileEmail}</dd>
          </div>
        {/if}

        <div class="grid gap-x-4 gap-y-0.5 py-2.5 sm:grid-cols-[11rem_minmax(0,1fr)]">
          <dt class="stamp sm:pt-[3px]">Where it lives</dt>
          <dd class="min-w-0 text-[13px] leading-[1.55] text-muted">
            {me.local
              ? "This account is stored here, so you set your own password."
              : "This account comes from your organisation's directory. Your password is changed there, not here."}
          </dd>
        </div>
      </dl>
    </Section>

    <Section
      title="What you are allowed to do"
      note="Set for you by an administrator. Nothing on this page can change them — they are shown so you know what your sign-in carries."
    >
      <div class="grid gap-x-10 gap-y-8 sm:grid-cols-2">
        <div class="min-w-0">
          <p class="stamp stamp-ink">Roles · {me.roles.length}</p>
          {#if me.roles.length === 0}
            <p class="mt-2 text-[13px] text-muted">No roles granted.</p>
          {:else}
            <ul class="mt-2 divide-y divide-rule border-t border-rule">
              {#each visibleRoles as role (role)}
                <li class="serial break-all py-1.5 text-[12.5px] text-ink">{role}</li>
              {/each}
            </ul>
            {#if me.roles.length > grantCap}
              <button
                type="button"
                class="act act-quiet mt-2"
                aria-expanded={showAllRoles}
                onclick={() => (showAllRoles = !showAllRoles)}
              >
                {showAllRoles ? "Show fewer" : `Show all ${me.roles.length} roles`}
              </button>
            {/if}
          {/if}
        </div>

        <div class="min-w-0">
          <p class="stamp stamp-ink">Permissions · {me.permissions.length}</p>
          {#if me.permissions.length === 0}
            <p class="mt-2 text-[13px] text-muted">No permissions granted.</p>
          {:else}
            <ul class="mt-2 divide-y divide-rule border-t border-rule">
              {#each visiblePermissions as permission (permission)}
                <li class="serial break-all py-1.5 text-[12.5px] text-ink">{permission}</li>
              {/each}
            </ul>
            {#if me.permissions.length > grantCap}
              <button
                type="button"
                class="act act-quiet mt-2"
                aria-expanded={showAllPermissions}
                onclick={() => (showAllPermissions = !showAllPermissions)}
              >
                {showAllPermissions ? "Show fewer" : `Show all ${me.permissions.length} permissions`}
              </button>
            {/if}
          {/if}
        </div>
      </div>
    </Section>

    <Section
      title="Check your access"
      note="Ask whether your sign-in would be allowed to make a given request. Nothing is written — this asks the same permission graph the middleware asks on every request."
    >
      <div class="grid gap-5 sm:grid-cols-3">
        <Entry
          label="Host"
          bind:value={checkHost}
          placeholder="api.example.com"
          mono
          hint="Left empty, only permissions that match an empty host can pass."
        />

        <Entry
          label="Path"
          bind:value={checkPath}
          placeholder="/path/to/check"
          mono
          hint="The request path, as the service would see it."
        />

        <div class="min-w-0">
          <label class="stamp block" for="self-check-method">Method</label>
          <input
            id="self-check-method"
            class="entry serial mt-1.5"
            list="self-check-methods"
            autocomplete="off"
            placeholder="GET or custom method"
            bind:value={checkMethod}
          />
          <datalist id="self-check-methods">
            {#each checkMethodOptions as option (option)}
              <option value={option}></option>
            {/each}
          </datalist>
          <p class="mt-1.5 text-[12px] leading-[1.5] text-muted">
            Compared case-insensitively; a custom verb is accepted.
          </p>
        </div>
      </div>

      <div class="mt-6 flex flex-wrap items-center gap-3">
        <button
          type="button"
          class="act act-primary"
          disabled={checkRunning || !checkPath.trim()}
          onclick={() => void runSelfCheck()}
        >
          {checkRunning ? "Checking…" : "Run check"}
        </button>

        {#if checkVerdict}
          <button
            type="button"
            class="act act-quiet"
            disabled={checkRunning}
            onclick={() => (checkVerdict = null)}
          >
            Clear verdict
          </button>
        {/if}
      </div>

      <div role="status" aria-live="polite">
        {#if checkVerdict}
          <div class="mt-6 border border-rule bg-sheet px-5 py-5">
            <div class="flex items-center gap-3">
              <Seal
                state={checkVerdict.allowed ? "endorsed" : "broken"}
                label={checkVerdict.allowed ? "Allowed" : "Denied"}
              />
              <p class="text-[13.5px] leading-[1.6] text-ink">
                {#if checkVerdict.allowed}
                  Your sign-in holds a permission that covers
                  <span class="serial">{checkVerdict.method || "—"}</span>
                  <span class="serial">{checkVerdict.path || "—"}</span>{checkVerdict.host
                    ? " on "
                    : ""}{#if checkVerdict.host}<span class="serial">{checkVerdict.host}</span
                    >{/if}.
                {:else}
                  Nothing granted to you covers
                  <span class="serial">{checkVerdict.method || "—"}</span>
                  <span class="serial">{checkVerdict.path || "—"}</span>{checkVerdict.host
                    ? " on "
                    : ""}{#if checkVerdict.host}<span class="serial">{checkVerdict.host}</span
                    >{/if}.
                {/if}
              </p>
            </div>
          </div>
        {/if}
      </div>
    </Section>

    {#if me.local}
      <Section
        title="Password"
        note="Changing it here replaces the password you type when you sign in. Sessions you already have stay open."
      >
        <div class="grid gap-5 sm:grid-cols-3">
          <Entry
            label="Current password"
            type="password"
            autocomplete="current-password"
            bind:value={currentPassword}
          />
          <Entry
            label="New password"
            type="password"
            autocomplete="new-password"
            hint="At least 8 characters."
            invalid={newPassword.length > 0 && newPassword.length < 8}
            bind:value={newPassword}
          />
          <Entry
            label="Repeat new password"
            type="password"
            autocomplete="new-password"
            hint={passwordMismatch ? "These two do not match yet." : ""}
            invalid={passwordMismatch}
            bind:value={confirmPassword}
          />
        </div>

        <div class="mt-6">
          <button
            type="button"
            class="act act-primary"
            disabled={session.busy ||
              !currentPassword ||
              newPassword.length < 8 ||
              newPassword !== confirmPassword}
            onclick={() => void changePassword()}
          >
            {session.busy ? "Changing…" : "Change password"}
          </button>
        </div>
      </Section>
    {/if}

    <Section
      title="Two-step codes"
      note="A six-digit code from an authenticator app, asked for after your password. It means a stolen password on its own is not enough to sign in as you."
    >
      {#snippet aside()}
        <Seal state={totpOn ? "endorsed" : "void"} label={totpOn ? "On" : "Off"} />
      {/snippet}

      {#if recoveryCodes.length > 0}
        <div class="guilloche stamp-in mb-8 border border-seal bg-sheet">
          <div class="hatch flex flex-wrap items-center gap-x-4 gap-y-1 border-b border-seal/40 px-5 py-3">
            <span class="stamp text-seal">Shown once</span>
            <span class="text-[13px] leading-[1.5] text-ink">
              These recovery codes will never be displayed again.
            </span>
          </div>

          <div class="px-5 py-6">
            <p class="max-w-[68ch] text-[13px] leading-[1.6] text-muted">
              Each code signs you in once if you lose your authenticator app. Keep them somewhere you
              can reach without your phone — printed, or in a password manager.
            </p>

            <ul class="mt-5 grid grid-cols-2 gap-x-6 gap-y-1 sm:grid-cols-3 lg:grid-cols-4">
              {#each recoveryCodes as code (code)}
                <li class="serial break-all border-b border-rule py-1.5 text-[13.5px] font-medium text-ink">
                  {code}
                </li>
              {/each}
            </ul>

            <div class="mt-6 flex flex-wrap gap-2">
              <button
                type="button"
                class="act act-primary"
                onclick={() => void copyText(recoveryCodes.join("\n"), "Recovery codes")}
              >
                {@render copyGlyph()}
                Copy all codes
              </button>
              <button type="button" class="act" onclick={() => (recoveryCodes = [])}>
                I have stored them
              </button>
            </div>
          </div>
        </div>
      {/if}

      {#if totpSecret}
        <div class="border border-rule bg-sheet">
          <div class="border-b border-rule px-5 py-4">
            <p class="stamp stamp-ink">Step 1 — add the secret to your app</p>
            <p class="mt-2 max-w-[68ch] text-[13px] leading-[1.6] text-muted">
              Open your authenticator app, choose to add an account by entering a key, and paste the
              secret below. The long link does the same thing if your app accepts one.
            </p>

            <p class="serial mt-4 break-all text-[15px] font-semibold text-ink">{totpSecret}</p>
            <p class="serial mt-2 break-all text-[12px] leading-[1.55] text-muted">{totpURL}</p>

            <div class="mt-4 flex flex-wrap gap-2">
              <button type="button" class="act" onclick={() => void copyText(totpSecret, "Secret")}>
                {@render copyGlyph()}
                Copy secret
              </button>
              <button type="button" class="act" onclick={() => void copyText(totpURL, "Setup link")}>
                {@render copyGlyph()}
                Copy setup link
              </button>
            </div>
          </div>

          <div class="px-5 py-4">
            <p class="stamp stamp-ink">Step 2 — prove it works</p>
            <div class="mt-3 flex flex-wrap items-end gap-4">
              <div class="w-40">
                <label class="stamp block" for="totp-confirm-code">Code from the app</label>
                <input
                  id="totp-confirm-code"
                  class="entry serial mt-1.5 text-[15px]"
                  placeholder="000000"
                  maxlength="6"
                  inputmode="numeric"
                  autocomplete="one-time-code"
                  bind:value={totpCode}
                />
              </div>
              <button
                type="button"
                class="act act-primary"
                disabled={session.busy || totpCode.trim().length !== 6}
                onclick={() => void totpConfirm()}
              >
                {session.busy ? "Checking…" : "Turn on two-step codes"}
              </button>
            </div>
            <p class="mt-3 max-w-[68ch] text-[12px] leading-[1.55] text-muted">
              Nothing changes about your sign-in until this code is accepted.
            </p>
          </div>
        </div>
      {:else if me.totp_enabled}
        <p class="max-w-[70ch] text-[13px] leading-[1.6] text-ink">
          Two-step codes are on. When you sign in with your password you will also be asked for the
          current code from your authenticator app, or for one of your recovery codes.
        </p>

        <div class="mt-8 grid gap-8">
          <div>
            <p class="stamp stamp-ink">Replace your recovery codes</p>
            <p class="mt-2 max-w-[70ch] text-[13px] leading-[1.6] text-muted">
              Do this if you think someone else has seen them, or you no longer know where they are.
            </p>
            <div class="mt-3">
              <BreakSeal
                consequence="Issuing a new set invalidates every recovery code you were given before. Any copy you printed or saved stops working the moment the new set appears, and the old codes cannot be brought back."
                action="Issue new recovery codes"
                disabled={session.busy}
                onconfirm={() => void totpRecovery()}
              />
            </div>
          </div>

          <div>
            <p class="stamp stamp-ink">Turn two-step codes off</p>
            <p class="mt-2 max-w-[70ch] text-[13px] leading-[1.6] text-muted">
              Your password alone would then be enough to sign in as you.
            </p>
            <div class="mt-3">
              <BreakSeal
                consequence="Turning this off deletes your authenticator secret and every unused recovery code. Sign-in will need only your password from the next attempt onward, and this cannot be undone — setting it up again gives you a new secret to scan and a new set of codes."
                action="Turn off two-step codes"
                disabled={session.busy}
                onconfirm={() => void totpDisable()}
              />
            </div>
          </div>
        </div>
      {:else}
        <p class="max-w-[70ch] text-[13px] leading-[1.6] text-ink">
          Two-step codes are off. Setting them up takes a minute: you add a secret to an
          authenticator app on your phone, type back the code it shows, and you are given recovery
          codes in case you lose the phone.
        </p>
        <div class="mt-5">
          <button
            type="button"
            class="act act-primary"
            disabled={session.busy}
            onclick={() => void totpRegister()}
          >
            {session.busy ? "Preparing…" : "Set up two-step codes"}
          </button>
        </div>
      {/if}
    </Section>

    <Section
      title="Passkeys"
      note="A passkey lets you sign in with the fingerprint, face or PIN of a device you already trust — no password to type or remember. Register one per device."
    >
      {#snippet aside()}
        <span class="stamp">{passkeys.length} registered</span>
      {/snippet}

      {#if passkeys.length === 0}
        <div class="border border-dashed border-rule px-6 py-12 text-center">
          <p class="text-[15px] font-semibold text-ink">No passkeys registered</p>
          <p class="mx-auto mt-2 max-w-[58ch] text-[13px] leading-[1.6] text-muted">
            You sign in with your password only. Registering a passkey on this device adds a second
            way in — and a faster one.
          </p>
        </div>
      {:else}
        <ul class="border-y border-rule">
          {#each passkeys as passkey (passkey.id)}
            <li class="border-b border-rule last:border-b-0">
              <div class="flex flex-wrap items-center gap-x-6 gap-y-2 py-3">
                <div class="min-w-0 flex-1 basis-64">
                  <p class="truncate text-[13.5px] font-medium text-ink">{passkeyName(passkey)}</p>
                  <p class="serial mt-0.5 truncate text-[12px] text-muted">
                    Registered {formatStamp(passkey.created_at) || passkey.created_at || "—"}
                  </p>
                </div>

                <button
                  type="button"
                  class="act act-quiet shrink-0 text-seal hover:bg-seal/10 hover:text-seal"
                  aria-expanded={pendingPasskey === passkey.id}
                  onclick={() => (pendingPasskey = pendingPasskey === passkey.id ? "" : passkey.id)}
                >
                  Remove
                </button>
              </div>

              {#if pendingPasskey === passkey.id}
                <div class="pb-4">
                  <BreakSeal
                    consequence={`Removing “${passkeyName(passkey)}” deletes this passkey from your account. The device or security key it is stored on can no longer sign you in, and it cannot be restored — you would have to register that device again from the start.`}
                    action="Remove this passkey"
                    disabled={session.busy}
                    onconfirm={() => void passkeyDelete(passkey.id)}
                  />
                </div>
              {/if}
            </li>
          {/each}
        </ul>
      {/if}

      {#if webauthnReady}
        <div class="mt-8 flex flex-wrap items-end gap-4">
          <div class="min-w-0 flex-1 basis-72">
            <label class="stamp block" for="passkey-label">Name for this device</label>
            <input
              id="passkey-label"
              class="entry mt-1.5"
              placeholder="Work laptop"
              autocomplete="off"
              bind:value={passkeyLabel}
            />
            <p class="mt-1.5 text-[12px] leading-[1.5] text-muted">
              So you can tell your passkeys apart later.
            </p>
          </div>

          <button
            type="button"
            class="act act-primary shrink-0"
            disabled={session.busy}
            onclick={() => void passkeyRegister()}
          >
            {session.busy ? "Waiting for your device…" : "Register a passkey"}
          </button>
        </div>
        <p class="mt-4 max-w-[70ch] text-[12.5px] leading-[1.55] text-muted">
          Your browser will ask you to confirm with the fingerprint reader, face scan, PIN or
          security key you already use on this device. Turna never sees any of it.
        </p>
      {:else}
        <p class="mt-8 max-w-[70ch] text-[13px] leading-[1.6] text-muted">
          This browser cannot create passkeys. Open this page in a current version of Chrome, Edge,
          Firefox or Safari to register one.
        </p>
      {/if}
    </Section>

    {#if patEnabled}
      <Section
        title="Personal access keys"
        note="A static key for your scripts and tools. It acts as you, carries at most your own access, and is checked against the database on every request — revoking it stops access immediately."
      >
        {#snippet aside()}
          <span class="stamp">{ownKeys.length} on record</span>
        {/snippet}

        {#if createdOwnKey}
          <div class="guilloche stamp-in mb-8 border border-seal bg-sheet">
            <div class="hatch flex flex-wrap items-center gap-x-4 gap-y-1 border-b border-seal/40 px-5 py-3">
              <span class="stamp text-seal">Issued once</span>
              <span class="text-[13px] leading-[1.5] text-ink">
                This is the only time this key will ever be shown.
              </span>
            </div>

            <div class="px-5 py-6">
              <p class="serial break-all text-[15px] font-semibold text-ink">{createdOwnKey}</p>

              <div class="mt-5 flex flex-wrap gap-2">
                <button
                  type="button"
                  class="act act-primary"
                  onclick={() => void copyText(createdOwnKey, "Access key")}
                >
                  {@render copyGlyph()}
                  Copy key
                </button>
                <button type="button" class="act" onclick={() => (createdOwnKey = "")}>
                  I have stored it
                </button>
              </div>

              <p class="mt-4 max-w-[68ch] text-[12.5px] leading-[1.55] text-muted">
                Only a hash of this key is kept, so it cannot be recovered later. Send it as an
                <span class="serial">X-API-Key</span> header.
              </p>
            </div>
          </div>
        {/if}

        {#if ownKeys.length === 0}
          <div class="border border-dashed border-rule px-6 py-10 text-center">
            <p class="text-[15px] font-semibold text-ink">No access keys issued</p>
            <p class="mx-auto mt-2 max-w-[58ch] text-[13px] leading-[1.6] text-muted">
              An access key lets a script or tool call the API as you, without an interactive
              sign-in.
            </p>
          </div>
        {:else}
          <ul class="border-y border-rule">
            {#each ownKeys as key (key.id)}
              {@const standing = ownKeyStanding(key)}
              <li class="border-b border-rule last:border-b-0">
                <div class="flex flex-wrap items-center gap-x-6 gap-y-2 py-3">
                  <div class="min-w-0 flex-1 basis-64">
                    <p class="truncate text-[13.5px] font-medium text-ink">{ownKeyLabel(key)}</p>
                    {#if key.description}
                      <p class="mt-0.5 truncate text-[12px] text-muted">{key.description}</p>
                    {/if}
                    <p class="serial mt-0.5 truncate text-[12px] text-muted">
                      Issued {formatStamp(key.created_at) || key.created_at || "—"} · Expires
                      {key.expires_at ? formatStamp(key.expires_at) : "never"} ·
                      {key.last_used_at ? `Last used ${formatStamp(key.last_used_at)}` : "Never used"}
                    </p>
                  </div>

                  <Seal state={standing.state} label={standing.label} />

                  <button
                    type="button"
                    class="act act-quiet shrink-0 text-seal hover:bg-seal/10 hover:text-seal"
                    aria-expanded={pendingKeyRevoke === key.id}
                    onclick={() => (pendingKeyRevoke = pendingKeyRevoke === key.id ? "" : key.id)}
                  >
                    Revoke
                  </button>
                </div>

                {#if pendingKeyRevoke === key.id}
                  <div class="pb-4">
                    <BreakSeal
                      consequence={`Revoking “${ownKeyLabel(key)}” deletes it permanently. Every script or tool still sending this key is refused on its very next request, and the key cannot be restored — you would have to issue a new one.`}
                      action="Revoke this key"
                      disabled={session.busy}
                      onconfirm={() => void revokeOwnKey(key.id)}
                    />
                  </div>
                {/if}
              </li>
            {/each}
          </ul>
        {/if}

        <div class="mt-8 flex flex-wrap items-end gap-4">
          <div class="min-w-0 flex-1 basis-64">
            <label class="stamp block" for="own-key-name">Name for this key</label>
            <input
              id="own-key-name"
              class="entry mt-1.5"
              placeholder="my-script"
              autocomplete="off"
              bind:value={ownKeyName}
            />
            <p class="mt-1.5 text-[12px] leading-[1.5] text-muted">
              Name it after what will hold it, so you can tell your keys apart later.
            </p>
          </div>

          <div class="min-w-0 flex-1 basis-64">
            <label class="stamp block" for="own-key-email">Email for X-User</label>
            <input
              id="own-key-email"
              class="entry mt-1.5"
              type="email"
              placeholder={profileEmail || "you@example.com"}
              autocomplete="off"
              bind:value={ownKeyEmail}
            />
            <p class="mt-1.5 text-[12px] leading-[1.5] text-muted">
              Optional override. Empty uses your account email, then the key name.
            </p>
          </div>

          <div class="min-w-0 flex-1 basis-64">
            <label class="stamp block" for="own-key-description">Description</label>
            <input
              id="own-key-description"
              class="entry mt-1.5"
              placeholder="Used by my deployment script"
              autocomplete="off"
              bind:value={ownKeyDescription}
            />
            <p class="mt-1.5 text-[12px] leading-[1.5] text-muted">
              Optional note about where this key is used.
            </p>
          </div>

          <div class="min-w-0 basis-48">
            <label class="stamp block" for="own-key-expires">Expires in</label>
            <input
              id="own-key-expires"
              class="entry serial mt-1.5"
              placeholder="720h"
              autocomplete="off"
              bind:value={ownKeyExpires}
            />
          </div>

          <button
            type="button"
            class="act act-primary shrink-0"
            disabled={session.busy}
            onclick={() => void createOwnKey()}
          >
            {session.busy ? "Issuing…" : "Issue a key"}
          </button>
        </div>

        <div class="mt-3 flex flex-wrap gap-2">
          {#each ownKeyPresets as preset (preset.label)}
            <button
              type="button"
              class="act {ownKeyExpires === preset.value ? 'act-primary' : ''}"
              aria-pressed={ownKeyExpires === preset.value}
              onclick={() => (ownKeyExpires = preset.value)}
            >
              {preset.label}
            </button>
          {/each}
        </div>
      </Section>
    {/if}
  {/if}
</Instrument>
