<script lang="ts">
  import { onMount } from "svelte";
  import { isWebAuthnSupported, startRegistration } from "../../lib/webauthn";
  import type { ServerCreationOptions } from "../../lib/webauthn";

  export let apiBase = "/auth/v1";
  export let userID = "";

  type CredentialMeta = {
    id: string;
    user_id: string;
    name: string;
    sign_count: number;
    created_at: string;
    updated_at: string;
  };

  let credentials: CredentialMeta[] = [];
  let panelError = "";
  let panelNotice = "";
  let busyLocal = false;
  let label = "";

  async function load() {
    panelError = "";
    try {
      const res = await fetch(`${apiBase}/passkey/credentials?user_id=${encodeURIComponent(userID)}`);
      if (!res.ok) throw new Error(`list failed: ${res.status}`);
      const body = await res.json();
      credentials = body.payload ?? [];
    } catch (err) {
      panelError = err instanceof Error ? err.message : String(err);
    }
  }

  async function removeCredential(id: string) {
    if (!confirm("DELETE PASSKEY?")) return;

    busyLocal = true;
    panelError = "";
    try {
      const res = await fetch(`${apiBase}/passkey/credentials/${encodeURIComponent(id)}`, { method: "DELETE" });
      if (!res.ok) throw new Error(`delete failed: ${res.status}`);
      await load();
    } catch (err) {
      panelError = err instanceof Error ? err.message : String(err);
    } finally {
      busyLocal = false;
    }
  }

  async function register() {
    busyLocal = true;
    panelError = "";
    panelNotice = "";
    try {
      const beginRes = await fetch(`${apiBase}/passkey/register`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ user_id: userID }),
      });
      const beginBody = await beginRes.json();
      if (!beginRes.ok) throw new Error(beginBody?.message ?? `begin failed: ${beginRes.status}`);

      const options = beginBody.payload.options as ServerCreationOptions;
      const credential = await startRegistration(options);

      const finishRes = await fetch(`${apiBase}/passkey/register`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          user_id: userID,
          session_id: beginBody.payload.session_id,
          name: label.trim(),
          credential,
        }),
      });
      const finishBody = await finishRes.json();
      if (!finishRes.ok) throw new Error(finishBody?.message ?? `finish failed: ${finishRes.status}`);

      panelNotice = "PASSKEY REGISTERED";
      label = "";
      await load();
    } catch (err) {
      panelError = err instanceof Error ? err.message : String(err);
    } finally {
      busyLocal = false;
    }
  }

  onMount(load);
</script>

<div class="grid gap-3 bg-panel p-3 md:col-span-2 xl:col-span-3">
  <div class="flex flex-wrap items-center justify-between gap-2">
    <div>
      <span class="t-label text-fg">[ PASSKEYS ]</span>
      <p class="mt-1 text-[11px] leading-4 text-dim">
        WebAuthn credentials stored for <span class="text-fg">this user</span>. Registration binds the authenticator present in this browser to their account.
      </p>
    </div>
    <span class="t-label">{credentials.length} REGISTERED</span>
  </div>

  {#if panelError}
    <p class="text-[11px] font-bold uppercase tracking-[0.12em] text-alert">{panelError}</p>
  {/if}
  {#if panelNotice}
    <p class="text-[11px] font-bold uppercase tracking-[0.12em] text-fg">{panelNotice}</p>
  {/if}

  {#if credentials.length === 0}
    <p class="text-[11px] leading-4 text-dim">No passkeys registered for this user.</p>
  {:else}
    <div class="grid gap-px bg-line">
      {#each credentials as credential}
        <div class="flex flex-wrap items-center justify-between gap-2 bg-crt p-2 text-[11px] leading-4">
          <div class="grid gap-1">
            <span class="break-all font-bold text-fg">{credential.name || credential.id}</span>
            <span class="text-dim">CREATED {credential.created_at} / SIGN.COUNT {credential.sign_count}</span>
          </div>
          <button
            class="border border-line px-2.5 py-1 text-[10px] font-bold uppercase tracking-[0.1em] text-alert hover:bg-alert hover:text-white"
            disabled={busyLocal}
            on:click={() => removeCredential(credential.id)}
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
        <input bind:value={label} class="field-t" placeholder="my laptop" />
      </label>
      <div class="flex items-end bg-panel p-3">
        <button class="btn-t-solid w-full" disabled={busyLocal} on:click={register}>
          {busyLocal ? "WAITING FOR AUTHENTICATOR..." : "[ REGISTER PASSKEY ]"}
        </button>
      </div>
    </div>
  {:else}
    <p class="text-[11px] leading-4 text-dim">WebAuthn is not supported in this browser.</p>
  {/if}
</div>
