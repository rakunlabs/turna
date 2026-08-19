<script lang="ts">
  import Instrument from "./ui/Instrument.svelte";
  import Section from "./ui/Section.svelte";
  import Entry from "./ui/Entry.svelte";
  import Seal from "./ui/Seal.svelte";
  import Serial from "./ui/Serial.svelte";
  import BreakSeal from "./ui/BreakSeal.svelte";
  import { docket, messageOf, session } from "../lib/state/session.svelte";

  /**
   * This page reports what the running instance already is — the cipher, the
   * canary, the columns under seal — and offers exactly one act: rotating the
   * key. Nothing here is a stored setting, so nothing here is a form field
   * except the new key itself.
   */
  let newKey = $state("");
  let rotatedKey = $state("");

  const canRotate = $derived(!session.busy && newKey.trim().length > 0);

  /** The columns re-encrypted by a rotation, in the order the server walks them. */
  const sealed = [
    { label: "User details", detail: "auth_users.details_encrypted" },
    { label: "Runtime settings", detail: "auth_settings.value_encrypted" },
    { label: "OAuth server clients", detail: "auth_oauth_clients.config_encrypted" },
    { label: "OAuth providers", detail: "auth_oauth_providers.config_encrypted" },
    { label: "SAML providers", detail: "auth_saml_providers.config_encrypted" },
    { label: "LDAP configs", detail: "auth_ldap_configs.config_encrypted" },
    { label: "TOTP secrets", detail: "auth_totp_secrets.secret_encrypted" },
  ];

  async function rotate() {
    const key = newKey.trim();
    if (!key) {
      docket.reject("Name the new key before rotating — the field above is empty.");
      return;
    }

    let message = "";

    const ok = await session.run(async () => {
      const res = await session.request<{ message?: string; rotated?: boolean }>(
        "encryption/rotate",
        { method: "POST", body: JSON.stringify({ new_key: key }) },
      );

      message = res.payload?.message ?? "Encryption key rotated";
    });

    if (!ok) return;

    rotatedKey = key;
    newKey = "";
    docket.commit(message);
  }

  async function copyKey() {
    try {
      await navigator.clipboard.writeText(rotatedKey);
      docket.commit("New key copied to the clipboard");
    } catch (err) {
      docket.reject(
        `${messageOf(err, "Clipboard unavailable")} — select the key below and copy it by hand.`,
      );
    }
  }
</script>

<Instrument
  title="Record encryption"
  note="Everything sensitive in PostgreSQL is sealed at rest with AES-256-GCM under the static encryption.key. This page reports that standing and rotates the key; it holds no settings of its own."
>
  {#snippet custody()}
    <span class="stamp">Static config <span class="serial stamp-raw">encryption.key</span></span>
    <span class="serial stamp-raw">{session.apiBase}/encryption/rotate</span>
  {/snippet}

  {#if rotatedKey}
    <div class="mb-10 border border-seal/45">
      <p class="hatch border-b border-seal/30 px-4 py-3 text-[13px] leading-[1.55] text-ink">
        <span class="stamp text-seal">Config out of date</span>
        <span class="mt-1.5 block">
          The stored rows are now sealed under the key below, but the static config still names the
          old one. Set <span class="serial">encryption.key</span> to this value everywhere before any
          instance restarts — the startup canary check fails until you do, and every other running
          replica must be restarted with it too.
        </span>
      </p>

      <div class="px-4 py-4">
        <p class="serial break-all border border-rule bg-raised px-3 py-2.5 text-[13px] text-ink">
          {rotatedKey}
        </p>
        <div class="mt-3 flex flex-wrap gap-2">
          <button type="button" class="act act-primary" onclick={() => void copyKey()}>
            Copy key
          </button>
          <button type="button" class="act" onclick={() => (rotatedKey = "")}>
            I have stored it
          </button>
        </div>
      </div>
    </div>
  {/if}

  <Section title="Standing" first>
    <div class="flex flex-wrap items-end gap-x-12 gap-y-6">
      <div>
        <Serial value="AES-256-GCM" size="md" />
        <p class="stamp mt-2">Cipher</p>
      </div>

      <div>
        <span class="inline-block py-1"><Seal state="endorsed" label="Canary verified" size={13} /></span>
        <p class="stamp mt-2">Startup check</p>
      </div>

      <p class="max-w-[52ch] flex-1 basis-72 text-[13px] leading-[1.6] text-muted">
        On boot this instance decrypts a known canary value with the configured key and refuses to
        start if it cannot. You are reading this page, so the key it holds is the key the stored rows
        were sealed with.
      </p>
    </div>
  </Section>

  <Section title="Under seal" note="The columns a rotation walks, and the only ones it rewrites.">
    <ul class="max-w-[70ch]">
      {#each sealed as item (item.detail)}
        <li class="flex flex-wrap items-baseline gap-x-4 gap-y-1 border-b border-rule py-2.5 last:border-b-0">
          <span class="flex shrink-0 items-center gap-2.5">
            <Seal state="endorsed" />
            <span class="text-[13.5px] text-ink">{item.label}</span>
          </span>
          <span class="serial min-w-0 flex-1 basis-56 truncate text-right text-[12px] text-muted">
            {item.detail}
          </span>
        </li>
      {/each}
    </ul>

    <p class="mt-5 max-w-[70ch] text-[12.5px] leading-[1.55] text-muted">
      Short-lived flow state and public passkey material are deliberately absent: neither is a secret
      at rest, and neither survives a rotation long enough to matter.
    </p>
  </Section>

  <Section
    title="Rotate the key"
    note="Re-encrypts every column above with a new key in one transaction and hot-swaps the running cipher. If any row fails, the whole rotation rolls back and the current key stays active."
  >
    <div class="max-w-[62ch]">
      <Entry
        label="New encryption key"
        mono
        bind:value={newKey}
        placeholder="any text — base64 16/24/32-byte values are used as-is"
        hint="Anything that is not a base64 key of 16, 24 or 32 bytes is SHA-256 derived. Store it in your secret manager before you rotate: this console shows it once and never again."
      />
    </div>

    <div class="mt-6 max-w-[70ch]">
      <BreakSeal
        consequence="Rotating re-encrypts every sealed row under the new key and swaps the live cipher immediately. The old key stops decrypting anything, and unless you set encryption.key to the new value in the static config before the next restart, this instance will not start. There is no undo."
        action="Rotate the encryption key"
        disabled={!canRotate}
        onconfirm={() => void rotate()}
      />
    </div>

    {#if session.busy}
      <p class="stamp mt-3 text-caution" role="status" aria-live="polite">
        Re-encrypting every sealed row…
      </p>
    {:else if !newKey.trim()}
      <p class="mt-3 text-[12px] text-muted">
        The seal stays inert until a new key is named above.
      </p>
    {/if}
  </Section>
</Instrument>
