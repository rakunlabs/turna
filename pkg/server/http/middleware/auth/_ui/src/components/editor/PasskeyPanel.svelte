<script lang="ts">
  import { onMount } from "svelte";

  import Section from "../ui/Section.svelte";
  import Entry from "../ui/Entry.svelte";
  import BreakSeal from "../ui/BreakSeal.svelte";
  import Seal from "../ui/Seal.svelte";
  import { isWebAuthnSupported, startRegistration } from "../../lib/webauthn";
  import type { ServerCreationOptions } from "../../lib/webauthn";
  import { docket, errorTextOf, messageOf, session } from "../../lib/state/session.svelte";
  import { formatStamp } from "../../lib/records";

  /**
   * Passkeys live beside the user record but are written through their own
   * endpoints, so this panel keeps its own busy flag rather than the session's.
   */
  let { userID }: { userID: string } = $props();

  type CredentialMeta = {
    id: string;
    user_id: string;
    name: string;
    rp_id?: string;
    sign_count: number;
    created_at: string;
    updated_at: string;
  };

  let credentials = $state<CredentialMeta[]>([]);
  let working = $state(false);
  let surveyed = $state(false);
  let loadFailed = $state(false);
  let label = $state("");
  let pendingRemoval = $state("");

  const supported = isWebAuthnSupported();

  async function load() {
    try {
      const res = await fetch(
        `${session.apiBase}/passkey/credentials?user_id=${encodeURIComponent(userID)}`,
      );
      if (!res.ok) throw new Error(await errorTextOf(res));
      const body = await res.json();
      credentials = body.payload ?? [];
      loadFailed = false;
      surveyed = true;
    } catch (err) {
      loadFailed = true;
      docket.reject(messageOf(err, "Cannot read this user's passkeys"));
    }
  }

  async function removeCredential(id: string) {
    pendingRemoval = "";
    working = true;
    try {
      const res = await fetch(`${session.apiBase}/passkey/credentials/${encodeURIComponent(id)}`, {
        method: "DELETE",
      });
      if (!res.ok) throw new Error(await errorTextOf(res));
      await load();
      docket.commit("Passkey removed");
    } catch (err) {
      docket.reject(messageOf(err, "Cannot remove this passkey"));
    } finally {
      working = false;
    }
  }

  async function register() {
    working = true;
    try {
      const beginRes = await fetch(`${session.apiBase}/passkey/register`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ user_id: userID }),
      });
      if (!beginRes.ok) throw new Error(await errorTextOf(beginRes));
      const beginBody = await beginRes.json();

      const options = beginBody.payload.options as ServerCreationOptions;
      const credential = await startRegistration(options);

      const finishRes = await fetch(`${session.apiBase}/passkey/register`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          user_id: userID,
          session_id: beginBody.payload.session_id,
          name: label.trim(),
          credential,
        }),
      });
      if (!finishRes.ok) throw new Error(await errorTextOf(finishRes));

      label = "";
      await load();
      docket.commit("Passkey registered");
    } catch (err) {
      docket.reject(messageOf(err, "Passkey registration did not complete"));
    } finally {
      working = false;
    }
  }

  onMount(() => {
    void load();
  });
</script>

<Section
  title="Passkeys"
  note="WebAuthn credentials held for this user. Registering binds the authenticator present in this browser to their account — do it at the machine that will be logging in."
>
  {#snippet aside()}
    <span class="stamp">
      {credentials.length}
      {credentials.length === 1 ? "credential" : "credentials"}
    </span>
  {/snippet}

  {#if loadFailed && !surveyed}
    <div class="border border-dashed border-seal/60 px-6 py-8 text-center" role="alert">
      <p class="text-[13px] leading-[1.6] text-ink">This user's passkeys could not be read.</p>
      <button type="button" class="act mt-4" disabled={working} onclick={() => void load()}>Try again</button>
    </div>
  {:else if !surveyed}
    <p class="text-[13px] text-muted">Reading credentials…</p>
  {:else if credentials.length === 0}
    <p class="border border-dashed border-rule px-6 py-10 text-center text-[13px] leading-[1.6] text-muted">
      No passkey registered for this user — they sign in with a password or an upstream provider only.
    </p>
  {:else}
    <ul class="border border-rule bg-sheet">
      {#each credentials as credential (credential.id)}
        <li class="border-b border-rule last:border-b-0">
          <div class="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 px-4 py-3">
            <div class="min-w-0">
              <p class="truncate text-[13.5px] font-medium text-ink">
                {credential.name || credential.id}
              </p>
              <p class="serial mt-0.5 truncate text-[12px] text-muted">
                {credential.rp_id || "Default relying party"} · Registered {formatStamp(credential.created_at) || "—"} · signature count
                {credential.sign_count}
              </p>
            </div>

            <div class="flex items-center gap-3">
              <Seal state="endorsed" label="Bound" />
              <button
                type="button"
                class="act act-quiet text-seal hover:bg-seal/10 hover:text-seal"
                aria-expanded={pendingRemoval === credential.id}
                disabled={working}
                onclick={() =>
                  (pendingRemoval = pendingRemoval === credential.id ? "" : credential.id)}
              >
                Remove
              </button>
            </div>
          </div>

          {#if pendingRemoval === credential.id}
            <div class="px-4 pb-4">
              <BreakSeal
                consequence={`Removing ${credential.name || credential.id} deletes the credential from this instance. The authenticator holding it can no longer sign this user in, the key cannot be re-registered from its stored form, and there is no undo.`}
                action="Remove this passkey"
                disabled={working}
                onconfirm={() => void removeCredential(credential.id)}
              />
            </div>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}

  {#if supported}
    <div class="mt-8 flex flex-wrap items-end gap-4">
      <div class="min-w-0 flex-1 basis-72">
        <Entry
          label="Passkey label"
          bind:value={label}
          placeholder="my laptop"
          hint="Names the authenticator so it can be told apart later."
        />
      </div>
      <button type="button" class="act act-primary" disabled={working} onclick={() => void register()}>
        {working ? "Waiting for the authenticator…" : "Register passkey"}
      </button>
    </div>
  {:else}
    <p class="mt-8 max-w-[70ch] text-[13px] leading-[1.6] text-muted">
      This browser does not support WebAuthn, so a passkey cannot be registered here. Open the console
      in a browser with platform authenticator support, or register from the user's own device.
    </p>
  {/if}
</Section>
