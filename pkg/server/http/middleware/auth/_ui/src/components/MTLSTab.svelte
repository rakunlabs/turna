<script lang="ts">
  import type { SettingNamespace } from "../lib/api";
  import type { Tab } from "../lib/navigation";

  export let apiBase = "/auth/v1";
  export let busy = false;
  export let settingsRevision = 0;
  export let getSettingBool: (namespace: SettingNamespace, path: string[], fallback?: boolean) => boolean = () => false;
  export let setSettingBool: (namespace: SettingNamespace, path: string[], value: boolean) => void = () => {};
  export let getSettingString: (namespace: SettingNamespace, path: string[]) => string = () => "";
  export let setSettingString: (namespace: SettingNamespace, path: string[], value: string) => void = () => {};
  export let saveSetting: (namespace: SettingNamespace) => void | Promise<void> = () => {};
  export let onSelect: (tab: Tab) => void = () => {};

  function inputValue(event: Event) {
    return (event.currentTarget as HTMLInputElement).value;
  }

  function checkedValue(event: Event) {
    return (event.currentTarget as HTMLInputElement).checked;
  }

  function settingBool(_revision: number, path: string[], fallback = false) {
    return getSettingBool("mtls", path, fallback);
  }

  function settingString(_revision: number, path: string[]) {
    return getSettingString("mtls", path);
  }

  function checkboxClass(checked: boolean) {
    const base = "h-3.5 w-3.5 appearance-none border bg-crt";
    return `${base} border-line checked:bg-fg ${checked ? "border-line" : "border-alert"}`;
  }

  $: enabled = settingBool(settingsRevision, ["enabled"]);
  $: certHeader = settingString(settingsRevision, ["cert_header"]);
  $: oauthBase = apiBase.replace(/\/v1$/, "");
</script>

<div class="grid gap-px bg-line p-px">
  <div class="grid gap-3 bg-panel p-4">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div>
        <span class="t-label text-fg">[ MTLS ]</span>
        <h3 class="mt-2 font-display text-3xl uppercase leading-none tracking-tight md:text-4xl">Client Certificates</h3>
      </div>
      <button class="btn-t-solid" disabled={busy} on:click={() => saveSetting("mtls")}>[ SAVE MTLS ]</button>
    </div>
    <p class="max-w-3xl text-xs leading-5 text-dim">
      mTLS authenticates <span class="text-fg">service accounts</span> for the OAuth2 <span class="text-fg">client_credentials</span> grant. The token endpoint matches the incoming certificate against that service account's certificate fingerprint or subject.
    </p>
  </div>

  <div class="grid gap-px bg-line lg:grid-cols-[minmax(0,1fr),minmax(0,1fr)]">
    <div class="grid content-start gap-px bg-line">
      <div class="bg-panel px-3 py-2">
        <span class="t-label text-fg">[ GLOBAL MTLS SETTINGS ]</span>
      </div>
      <label class="flex items-center gap-3 bg-panel p-3 text-xs font-bold uppercase tracking-[0.15em]">
        <input type="checkbox" checked={enabled} class={checkboxClass(enabled)} on:change={(event) => setSettingBool("mtls", ["enabled"], checkedValue(event))} />
        <span class={enabled ? "text-fg" : "text-alert"}>{enabled ? "MTLS ENABLED" : "MTLS DISABLED"}</span>
      </label>
      <label class="grid gap-1 bg-panel p-3">
        <span class="t-label">TRUSTED CERTIFICATE HEADER</span>
        <input class="field-t" value={certHeader} placeholder="ssl-client-cert" on:input={(event) => setSettingString("mtls", ["cert_header"], inputValue(event))} />
        <span class="text-[10px] leading-4 text-dim">Use only behind a trusted TLS-terminating proxy. Empty means use the TLS handshake peer certificate.</span>
      </label>
    </div>

    <div class="grid content-start gap-px bg-line">
      <div class="bg-panel px-3 py-2">
        <span class="t-label text-fg">[ WHERE CLIENT CERTS LIVE ]</span>
      </div>
      <div class="grid gap-3 bg-panel p-4 text-xs leading-5 text-dim">
        <p>
          Each mTLS client is a <span class="text-fg">service account</span>. Create/edit one and fill either <span class="text-fg">cert_fingerprint</span> or <span class="text-fg">cert_subject</span> in its certificate section.
        </p>
        <p>
          The service account alias is the OAuth2 <span class="text-fg">client_id</span>. For mTLS-only clients, <span class="text-fg">client_secret</span> may be empty when a certificate is configured.
        </p>
        <button class="btn-t-solid w-fit" on:click={() => onSelect("service-accounts")}>[ OPEN SERVICE ACCOUNTS ]</button>
      </div>
    </div>
  </div>

  <div class="grid gap-px bg-line lg:grid-cols-2">
    <div class="bg-panel p-4">
      <span class="t-label text-fg">[ TOKEN REQUEST ]</span>
      <pre class="mt-3 overflow-auto border border-line bg-crt p-3 text-[11px] leading-5 text-fg">curl --cert client.crt --key client.key \
  -X POST {oauthBase}/oauth2/token \
  -d grant_type=client_credentials \
  -d client_id=my-service</pre>
    </div>
    <div class="bg-panel p-4">
      <span class="t-label text-fg">[ SESSION INTEGRATION ]</span>
      <p class="mt-3 text-xs leading-5 text-dim">
        Session does not inspect the raw certificate. Auth issues a normal access token after mTLS validation; then session validates that token through <span class="text-fg">auth_middleware</span> or JWKS and forwards claims/X-User to the app.
      </p>
      <pre class="mt-3 overflow-auto border border-line bg-crt p-3 text-[11px] leading-5 text-fg">curl https://app.example.com/api \
  -H 'Authorization: Bearer &lt;access_token&gt;'</pre>
    </div>
  </div>
</div>
