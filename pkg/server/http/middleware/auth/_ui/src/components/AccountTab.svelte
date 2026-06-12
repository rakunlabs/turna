<script lang="ts">
  import { onMount } from "svelte";
  import { isWebAuthnSupported, startRegistration } from "../lib/webauthn";
  import type { ServerCreationOptions } from "../lib/webauthn";
  import type { AnyRecord } from "../lib/api";

  export let apiBase = "/auth/v1";

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

  let me: Me | null = null;
  let loadError = "";
  let busy = false;
  let error = "";
  let notice = "";

  // password
  let currentPassword = "";
  let newPassword = "";
  let confirmPassword = "";

  // totp
  let totpSecret = "";
  let totpURL = "";
  let totpCode = "";
  let recoveryCodes: string[] = [];

  // passkeys
  let passkeys: PasskeyMeta[] = [];
  let passkeyLabel = "";

  function flash(message: string) {
    notice = message;
    error = "";
    window.setTimeout(() => {
      if (notice === message) notice = "";
    }, 4000);
  }

  function fail(err: unknown, fallback: string) {
    error = err instanceof Error ? err.message : fallback;
    notice = "";
  }

  async function api<T>(path: string, init?: RequestInit): Promise<T> {
    const res = await fetch(`${apiBase}/${path}`, {
      headers: { "Content-Type": "application/json", ...(init?.headers ?? {}) },
      ...init,
    });

    let body: AnyRecord = {};
    try {
      body = await res.json();
    } catch {
      // ignore empty bodies
    }

    if (!res.ok) {
      throw new Error(String(body.message ?? body.error ?? res.statusText));
    }

    return body.payload as T;
  }

  async function loadMe() {
    loadError = "";
    try {
      me = await api<Me>("me");
    } catch (err) {
      me = null;
      loadError = err instanceof Error ? err.message : "CANNOT LOAD ACCOUNT";
    }
  }

  async function loadPasskeys() {
    try {
      passkeys = (await api<PasskeyMeta[]>("passkey/credentials")) ?? [];
    } catch {
      passkeys = [];
    }
  }

  async function loadAll() {
    await loadMe();
    if (me) await loadPasskeys();
  }

  // ////////////////////////////////////////////////////////////////
  // password

  async function changePassword() {
    if (newPassword !== confirmPassword) {
      error = "NEW PASSWORDS DO NOT MATCH";
      return;
    }

    busy = true;
    try {
      await api("me/password", {
        method: "POST",
        body: JSON.stringify({ current_password: currentPassword, new_password: newPassword }),
      });

      currentPassword = "";
      newPassword = "";
      confirmPassword = "";
      flash("PASSWORD UPDATED");
    } catch (err) {
      fail(err, "PASSWORD UPDATE FAILED");
    } finally {
      busy = false;
    }
  }

  // ////////////////////////////////////////////////////////////////
  // totp

  async function totpRegister() {
    busy = true;
    try {
      const payload = await api<{ secret: string; url: string }>("totp/register", { method: "POST", body: "{}" });
      totpSecret = payload.secret;
      totpURL = payload.url;
      recoveryCodes = [];
      flash("SCAN THE SECRET, THEN CONFIRM WITH A CODE");
    } catch (err) {
      fail(err, "TOTP REGISTER FAILED");
    } finally {
      busy = false;
    }
  }

  async function totpConfirm() {
    busy = true;
    try {
      const payload = await api<{ recovery_codes?: string[] }>("totp/confirm", {
        method: "POST",
        body: JSON.stringify({ code: totpCode.trim() }),
      });

      recoveryCodes = payload.recovery_codes ?? [];
      totpSecret = "";
      totpURL = "";
      totpCode = "";
      await loadMe();
      flash("TOTP ENABLED — SAVE YOUR RECOVERY CODES");
    } catch (err) {
      fail(err, "TOTP CONFIRM FAILED");
    } finally {
      busy = false;
    }
  }

  async function totpRecovery() {
    if (!confirm("REGENERATE RECOVERY CODES? The old set becomes invalid.")) return;

    busy = true;
    try {
      const payload = await api<{ recovery_codes?: string[] }>("totp/recovery", { method: "POST", body: "{}" });
      recoveryCodes = payload.recovery_codes ?? [];
      flash("RECOVERY CODES REGENERATED — SAVE THEM NOW");
    } catch (err) {
      fail(err, "RECOVERY REGENERATE FAILED");
    } finally {
      busy = false;
    }
  }

  async function totpDisable() {
    if (!confirm("DISABLE TOTP? Password logins will no longer require a second factor.")) return;

    busy = true;
    try {
      await api("totp", { method: "DELETE" });
      totpSecret = "";
      totpURL = "";
      recoveryCodes = [];
      await loadMe();
      flash("TOTP DISABLED");
    } catch (err) {
      fail(err, "TOTP DISABLE FAILED");
    } finally {
      busy = false;
    }
  }

  // ////////////////////////////////////////////////////////////////
  // passkeys

  async function passkeyRegister() {
    busy = true;
    try {
      const begin = await api<{ session_id: string; options: ServerCreationOptions }>("passkey/register", {
        method: "POST",
        body: "{}",
      });

      const credential = await startRegistration(begin.options);

      await api("passkey/register", {
        method: "POST",
        body: JSON.stringify({ session_id: begin.session_id, name: passkeyLabel.trim(), credential }),
      });

      passkeyLabel = "";
      await Promise.all([loadPasskeys(), loadMe()]);
      flash("PASSKEY REGISTERED");
    } catch (err) {
      fail(err, "PASSKEY REGISTER FAILED");
    } finally {
      busy = false;
    }
  }

  async function passkeyDelete(id: string) {
    if (!confirm("DELETE PASSKEY?")) return;

    busy = true;
    try {
      await api(`passkey/credentials/${encodeURIComponent(id)}`, { method: "DELETE" });
      await Promise.all([loadPasskeys(), loadMe()]);
      flash("PASSKEY DELETED");
    } catch (err) {
      fail(err, "PASSKEY DELETE FAILED");
    } finally {
      busy = false;
    }
  }

  async function copyText(value: string) {
    try {
      await navigator.clipboard.writeText(value);
      flash("COPIED TO CLIPBOARD");
    } catch {
      error = "CLIPBOARD UNAVAILABLE";
    }
  }

  onMount(() => {
    void loadAll();
  });
</script>

<div class="grid gap-px bg-line p-px">
  {#if error}
    <div class="flex items-center gap-3 bg-panel px-4 py-2">
      <span class="bg-alert px-2 py-0.5 text-[10px] font-bold uppercase tracking-[0.15em] text-white">FAULT</span>
      <span class="text-xs uppercase tracking-[0.05em] text-alert">{error}</span>
    </div>
  {/if}
  {#if notice}
    <div class="flex items-center gap-3 bg-panel px-4 py-2">
      <span class="bg-fg px-2 py-0.5 text-[10px] font-bold uppercase tracking-[0.15em] text-crt">OK</span>
      <span class="text-xs uppercase tracking-[0.05em]">{notice}</span>
    </div>
  {/if}

  {#if !me}
    <div class="grid gap-2 bg-panel p-4">
      <span class="t-label text-fg">[ MY ACCOUNT ]</span>
      <p class="text-[11px] leading-4 text-dim">
        {loadError ? loadError : "LOADING..."}
      </p>
      <p class="text-[11px] leading-4 text-dim">
        This page needs an authenticated session (the X-User header). Put a session middleware in front of the auth routes.
      </p>
    </div>
  {:else}
    <!-- identity -->
    <div class="grid gap-3 bg-panel p-4">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <span class="t-label text-fg">[ IDENTITY ]</span>
        <span class="t-label">{me.is_active ? "ACTIVE" : "DISABLED"} / {me.local ? "LOCAL" : "FEDERATED"}</span>
      </div>

      <div class="grid gap-1 text-[11px] leading-5">
        <p><span class="text-dim">ID</span> <span class="break-all font-bold text-fg">{me.id}</span></p>
        <p><span class="text-dim">ALIAS</span> <span class="break-all">{me.alias.join(", ")}</span></p>
        {#if me.details?.name}<p><span class="text-dim">NAME</span> {me.details.name}</p>{/if}
        {#if me.details?.email}<p><span class="text-dim">EMAIL</span> {me.details.email}</p>{/if}
      </div>

      <div class="grid gap-2 md:grid-cols-2">
        <div class="grid content-start gap-1 border border-line p-2">
          <span class="t-label">ROLES ({me.roles.length})</span>
          {#if me.roles.length === 0}
            <span class="text-[11px] text-dim">NO ROLES</span>
          {:else}
            <div class="flex flex-wrap gap-1">
              {#each me.roles as role}
                <span class="border border-line px-1.5 py-0.5 text-[10px] uppercase tracking-[0.08em]">{role}</span>
              {/each}
            </div>
          {/if}
        </div>
        <div class="grid content-start gap-1 border border-line p-2">
          <span class="t-label">PERMISSIONS ({me.permissions.length})</span>
          {#if me.permissions.length === 0}
            <span class="text-[11px] text-dim">NO PERMISSIONS</span>
          {:else}
            <div class="flex flex-wrap gap-1">
              {#each me.permissions as permission}
                <span class="border border-line px-1.5 py-0.5 text-[10px] uppercase tracking-[0.08em]">{permission}</span>
              {/each}
            </div>
          {/if}
        </div>
      </div>
    </div>

    <!-- password -->
    {#if me.local}
      <div class="grid gap-3 bg-panel p-4">
        <span class="t-label text-fg">[ CHANGE PASSWORD ]</span>
        <div class="grid gap-2 md:grid-cols-3">
          <label class="grid gap-1">
            <span class="t-label">CURRENT PASSWORD</span>
            <input type="password" bind:value={currentPassword} class="field-t" autocomplete="current-password" />
          </label>
          <label class="grid gap-1">
            <span class="t-label">NEW PASSWORD (MIN 8)</span>
            <input type="password" bind:value={newPassword} class="field-t" autocomplete="new-password" />
          </label>
          <label class="grid gap-1">
            <span class="t-label">CONFIRM NEW PASSWORD</span>
            <input type="password" bind:value={confirmPassword} class="field-t" autocomplete="new-password" />
          </label>
        </div>
        <div>
          <button
            class="btn-t-solid"
            disabled={busy || !currentPassword || newPassword.length < 8 || newPassword !== confirmPassword}
            on:click={changePassword}
          >
            [ UPDATE PASSWORD ]
          </button>
        </div>
      </div>
    {/if}

    <!-- totp -->
    <div class="grid gap-3 bg-panel p-4">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <span class="t-label text-fg">[ TWO-FACTOR / TOTP ]</span>
        <span class="t-label">{me.totp_enabled ? "ENABLED" : "DISABLED"}</span>
      </div>

      {#if recoveryCodes.length > 0}
        <div class="grid gap-2 border border-line p-3">
          <span class="t-label text-alert">RECOVERY CODES — SHOWN ONCE, STORE THEM SAFELY</span>
          <div class="grid grid-cols-2 gap-1 md:grid-cols-4">
            {#each recoveryCodes as code}
              <span class="break-all border border-line px-1.5 py-1 text-[11px]">{code}</span>
            {/each}
          </div>
          <div>
            <button class="btn-t-solid" on:click={() => copyText(recoveryCodes.join("\n"))}>[ COPY ALL ]</button>
          </div>
        </div>
      {/if}

      {#if totpSecret}
        <div class="grid gap-2 border border-line p-3">
          <span class="t-label">1. ADD THE SECRET TO YOUR AUTHENTICATOR APP</span>
          <p class="break-all text-[12px] font-bold text-fg">{totpSecret}</p>
          <p class="break-all text-[10px] leading-4 text-dim">{totpURL}</p>
          <div class="flex flex-wrap gap-2">
            <button class="btn-t-solid" on:click={() => copyText(totpSecret)}>[ COPY SECRET ]</button>
            <button class="btn-t-solid" on:click={() => copyText(totpURL)}>[ COPY OTPAUTH URL ]</button>
          </div>
          <label class="grid max-w-xs gap-1">
            <span class="t-label">2. CONFIRM WITH A 6-DIGIT CODE</span>
            <input bind:value={totpCode} class="field-t" placeholder="000000" maxlength="6" inputmode="numeric" />
          </label>
          <div>
            <button class="btn-t-solid" disabled={busy || totpCode.trim().length !== 6} on:click={totpConfirm}>
              [ CONFIRM & ENABLE ]
            </button>
          </div>
        </div>
      {:else if me.totp_enabled}
        <p class="text-[11px] leading-4 text-dim">
          Password logins require a TOTP code (or a single-use recovery code) from your authenticator app.
        </p>
        <div class="flex flex-wrap gap-2">
          <button class="btn-t-solid" disabled={busy} on:click={totpRecovery}>[ REGENERATE RECOVERY CODES ]</button>
          <button
            class="border border-line px-2.5 py-1 text-[10px] font-bold uppercase tracking-[0.1em] text-alert hover:bg-alert hover:text-white"
            disabled={busy}
            on:click={totpDisable}
          >
            DISABLE TOTP
          </button>
        </div>
      {:else}
        <p class="text-[11px] leading-4 text-dim">
          Add a second factor: scan a secret with Google Authenticator (or compatible) and confirm with a code.
        </p>
        <div>
          <button class="btn-t-solid" disabled={busy} on:click={totpRegister}>[ SET UP TOTP ]</button>
        </div>
      {/if}
    </div>

    <!-- passkeys -->
    <div class="grid gap-3 bg-panel p-4">
      <div class="flex flex-wrap items-center justify-between gap-2">
        <span class="t-label text-fg">[ PASSKEYS ]</span>
        <span class="t-label">{passkeys.length} REGISTERED</span>
      </div>

      {#if passkeys.length === 0}
        <p class="text-[11px] leading-4 text-dim">No passkeys registered. Passkeys enable passwordless login.</p>
      {:else}
        <div class="grid gap-px bg-line">
          {#each passkeys as passkey}
            <div class="flex flex-wrap items-center justify-between gap-2 bg-crt p-2 text-[11px] leading-4">
              <div class="grid gap-1">
                <span class="break-all font-bold text-fg">{passkey.name || passkey.id}</span>
                <span class="text-dim">CREATED {passkey.created_at}</span>
              </div>
              <button
                class="border border-line px-2.5 py-1 text-[10px] font-bold uppercase tracking-[0.1em] text-alert hover:bg-alert hover:text-white"
                disabled={busy}
                on:click={() => passkeyDelete(passkey.id)}
              >
                REMOVE
              </button>
            </div>
          {/each}
        </div>
      {/if}

      {#if isWebAuthnSupported()}
        <div class="grid gap-px bg-line md:grid-cols-[minmax(0,1fr),auto]">
          <label class="grid gap-1 bg-panel p-3">
            <span class="t-label">PASSKEY LABEL</span>
            <input bind:value={passkeyLabel} class="field-t" placeholder="my laptop" />
          </label>
          <div class="flex items-end bg-panel p-3">
            <button class="btn-t-solid w-full" disabled={busy} on:click={passkeyRegister}>
              {busy ? "WAITING FOR AUTHENTICATOR..." : "[ REGISTER PASSKEY ]"}
            </button>
          </div>
        </div>
      {:else}
        <p class="text-[11px] leading-4 text-dim">WebAuthn is not supported in this browser.</p>
      {/if}
    </div>

  {/if}
</div>
