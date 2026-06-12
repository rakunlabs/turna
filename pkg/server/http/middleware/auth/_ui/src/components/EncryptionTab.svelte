<script lang="ts">
  import type { AnyRecord } from "../lib/api";

  export let apiBase = "/auth/v1";
  export let busy = false;

  let newKey = "";
  let confirmUpdate = false;
  let apiBusy = false;
  let error = "";
  let notice = "";
  let rotatedKey = "";

  $: working = busy || apiBusy;
  $: canRotate = !working && newKey.trim().length > 0 && confirmUpdate;

  function flash(message: string) {
    notice = message;
    error = "";
    window.setTimeout(() => {
      if (notice === message) notice = "";
    }, 6000);
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

    return (body as { payload: T }).payload;
  }

  async function rotate() {
    const key = newKey.trim();
    if (!key) {
      error = "NEW KEY IS REQUIRED";
      return;
    }

    if (!confirm("ROTATE THE RECORD ENCRYPTION KEY? All encrypted rows are re-encrypted now. You MUST set this key in the config before the next restart, or startup will fail.")) {
      return;
    }

    apiBusy = true;
    error = "";
    try {
      const payload = await api<{ message?: string; rotated?: boolean }>("encryption/rotate", {
        method: "POST",
        body: JSON.stringify({ new_key: key }),
      });

      rotatedKey = key;
      newKey = "";
      confirmUpdate = false;
      flash(payload?.message ?? "ENCRYPTION KEY ROTATED");
    } catch (err) {
      error = err instanceof Error ? err.message : "ENCRYPTION KEY ROTATION FAILED";
    } finally {
      apiBusy = false;
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

  <div class="bg-panel p-4">
    <p class="t-label text-fg">[ ENCRYPTION ]</p>
    <h3 class="mt-2 font-display text-3xl uppercase leading-none tracking-tight md:text-4xl">Record Encryption Key</h3>
    <p class="mt-3 max-w-3xl text-xs leading-5 text-dim">
      User details, runtime settings, OAuth/SAML/LDAP configs and TOTP secrets are sealed at rest with AES-256-GCM
      using the static <span class="text-fg">encryption.key</span>. Rotating re-encrypts every encrypted row with a new
      key in one transaction and hot-swaps the running cipher. On boot a canary value verifies the configured key, so a
      wrong key fails fast instead of corrupting reads.
    </p>
  </div>

  {#if rotatedKey}
    <div class="grid gap-2 border-l-2 border-alert bg-panel p-4">
      <span class="t-label text-alert">ROTATED — UPDATE CONFIG BEFORE RESTART</span>
      <p class="max-w-3xl text-[11px] leading-5 text-dim">
        Set <span class="text-fg">encryption.key</span> to the value below in your static config. The next restart will
        FAIL the startup canary check until you do. Other running instances must also be restarted with the new key.
      </p>
      <p class="break-all border border-line bg-crt p-3 text-[12px] font-bold text-fg">{rotatedKey}</p>
      <div class="flex flex-wrap gap-2">
        <button class="btn-t-solid" on:click={() => copyText(rotatedKey)}>[ COPY KEY ]</button>
        <button class="btn-t" on:click={() => (rotatedKey = "")}>DISMISS</button>
      </div>
    </div>
  {/if}

  <div class="border border-line bg-panel">
    <div class="flex flex-wrap items-center justify-between gap-3 border-b border-line px-4 py-2">
      <span class="t-label text-fg">[ ROTATE ENCRYPTION KEY ]</span>
      <button class="btn-t-solid" disabled={!canRotate} on:click={rotate}>[ ROTATE ]</button>
    </div>

    <div class="grid gap-px bg-line p-px">
      <label class="grid gap-1 bg-panel p-3">
        <span class="t-label">NEW ENCRYPTION KEY</span>
        <input
          bind:value={newKey}
          class="field-t"
          autocomplete="off"
          spellcheck="false"
          placeholder="any text; base64 16/24/32-byte values are used as-is, anything else is SHA-256 derived"
        />
        <span class="text-[10px] leading-4 text-dim">
          Pick a strong secret you can store in your config/secret manager. This value is what you must put in
          <span class="text-fg">encryption.key</span>.
        </span>
      </label>

      <label class="flex items-center gap-3 bg-panel p-3 text-xs font-bold uppercase tracking-[0.15em]">
        <input
          bind:checked={confirmUpdate}
          type="checkbox"
          class={`h-3.5 w-3.5 appearance-none border bg-crt checked:bg-alert ${confirmUpdate ? "border-line" : "border-alert"}`}
        />
        <span class={confirmUpdate ? "text-fg" : "text-alert"}>I WILL UPDATE encryption.key IN THE CONFIG BEFORE RESTART</span>
      </label>

      <p class="bg-panel p-3 text-[11px] leading-4 text-dim">
        Rotation runs in a single transaction; if any row fails to re-encrypt it rolls back and the current key stays
        active. A no-op is reported when the new key matches the current one.
      </p>
    </div>
  </div>
</div>
