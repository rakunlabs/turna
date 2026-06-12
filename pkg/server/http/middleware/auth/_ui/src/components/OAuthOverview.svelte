<script lang="ts">
  import type { AnyRecord, SettingNamespace } from "../lib/api";

  export let busy = false;
  export let settingsRevision = 0;
  export let oauthBase = "/auth";
  export let jwksKey: AnyRecord = {};
  export let getSettingString: (namespace: SettingNamespace, path: string[]) => string = () => "";
  export let setSettingString: (namespace: SettingNamespace, path: string[], value: string) => void = () => {};
  export let getSettingBool: (namespace: SettingNamespace, path: string[], fallback?: boolean) => boolean = () => false;
  export let setSettingBool: (namespace: SettingNamespace, path: string[], value: boolean) => void = () => {};
  export let getSettingList: (namespace: SettingNamespace, path: string[]) => string = () => "";
  export let setSettingList: (namespace: SettingNamespace, path: string[], value: string) => void = () => {};
  export let saveSetting: (namespace: SettingNamespace) => void | Promise<void> = () => {};
  export let rotateJWT: () => void | Promise<void> = () => {};

  function inputValue(event: Event) {
    return (event.currentTarget as HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement).value;
  }

  function checkedValue(event: Event) {
    return (event.currentTarget as HTMLInputElement).checked;
  }

  function checkboxClass(checked: boolean, variant: "danger" | "neutral" = "neutral") {
    const base = "h-3.5 w-3.5 appearance-none border bg-crt";
    if (variant === "danger") return `${base} border-line checked:bg-alert`;
    return `${base} border-line checked:bg-fg`;
  }

  function fieldText(value: unknown) {
    if (value === undefined || value === null) return "";
    return typeof value === "string" ? value.trim() : String(value).trim();
  }

  function settingString(_revision: number, namespace: SettingNamespace, path: string[], fallback = "") {
    return getSettingString(namespace, path) || fallback;
  }

  function setSchema(event: Event) {
    schema = inputValue(event);
    setSettingString("oauth2", ["schema"], schema);
  }

  function setPasskeyUserVerification(event: Event) {
    passkeyUserVerification = inputValue(event);
    setSettingString("passkey", ["user_verification"], passkeyUserVerification);
  }

  let schema = "https";
  let passkeyUserVerification = "preferred";

  $: schema = settingString(settingsRevision, "oauth2", ["schema"], "https");
  $: passkeyUserVerification = settingString(settingsRevision, "passkey", ["user_verification"], "preferred");

  type Section = "tokens" | "login" | "jwt";
  let section: Section = "tokens";
  const sections: { id: Section; label: string }[] = [
    { id: "tokens", label: "Tokens & redirects" },
    { id: "login", label: "Login methods" },
    { id: "jwt", label: "JWT & certs" },
  ];
</script>

<div class="grid gap-px bg-line p-px">
  <div class="bg-panel p-4">
    <p class="t-label text-fg">[ OAUTH2 CONTROL ]</p>
    <h3 class="mt-2 font-display text-3xl uppercase leading-none tracking-tight md:text-4xl">Token Minting</h3>
    <p class="mt-3 max-w-3xl text-xs leading-5 text-dim">
      Configure token lifetimes, upstream code-flow redirects, login methods and JWT signing. The OAuth code/state store moved to the CACHE page.
    </p>
  </div>

  <div class="flex flex-wrap gap-px bg-line">
    {#each sections as item}
      <button
        class={`px-4 py-2 text-[11px] font-bold uppercase tracking-[0.15em] ${section === item.id ? "bg-alert text-white" : "bg-panel text-dim hover:text-fg"}`}
        on:click={() => (section = item.id)}
      >
        {item.label}
      </button>
    {/each}
  </div>

  {#if section === "tokens"}
  <div class="grid gap-px bg-line xl:grid-cols-2">
    <div class="grid gap-px bg-line p-px">
      <div class="flex items-center justify-between bg-panel px-3 py-2">
        <span class="t-label text-fg">[ TOKEN LIFETIMES ]</span>
        <button class="btn-t-solid" disabled={busy} on:click={() => saveSetting("token")}>SAVE TOKEN</button>
      </div>
      <div class="grid gap-px bg-line md:grid-cols-2">
        <label class="grid gap-1 bg-panel p-3">
          <span class="t-label">ACCESS TOKEN LIFETIME</span>
          <input class="field-t" value={getSettingString("token", ["token_lifetime"])} placeholder="15m" on:input={(event) => setSettingString("token", ["token_lifetime"], inputValue(event))} />
        </label>
        <label class="grid gap-1 bg-panel p-3">
          <span class="t-label">REFRESH TOKEN LIFETIME</span>
          <input class="field-t" value={getSettingString("token", ["refresh_lifetime"])} placeholder="24h" on:input={(event) => setSettingString("token", ["refresh_lifetime"], inputValue(event))} />
        </label>
      </div>
    </div>

    <div class="grid gap-px bg-line p-px">
      <div class="flex items-center justify-between bg-panel px-3 py-2">
        <span class="t-label text-fg">[ CODE FLOW REDIRECTS ]</span>
        <button class="btn-t-solid" disabled={busy} on:click={() => saveSetting("oauth2")}>SAVE OAUTH2</button>
      </div>
      <div class="grid gap-px bg-line md:grid-cols-3">
        <label class="grid gap-1 bg-panel p-3 md:col-span-2">
          <span class="t-label">BASE URL</span>
          <input class="field-t" value={getSettingString("oauth2", ["base_url"])} placeholder="https://auth.example.com" on:input={(event) => setSettingString("oauth2", ["base_url"], inputValue(event))} />
        </label>
        <label class="grid gap-1 bg-panel p-3">
          <span class="t-label">SCHEMA</span>
          <select class="field-t uppercase" on:change={setSchema}>
            <option value="https" selected={schema === "https"}>https</option>
            <option value="http" selected={schema === "http"}>http</option>
          </select>
        </label>
        <label class="flex items-center gap-3 bg-panel p-3 text-xs font-bold uppercase tracking-[0.15em] md:col-span-3">
          <input type="checkbox" checked={getSettingBool("oauth2", ["insecure_skip_verify"])} class={checkboxClass(getSettingBool("oauth2", ["insecure_skip_verify"]), "danger")} on:change={(event) => setSettingBool("oauth2", ["insecure_skip_verify"], checkedValue(event))} />
          <span class={getSettingBool("oauth2", ["insecure_skip_verify"]) ? "text-alert" : "text-dim"}>INSECURE TLS SKIP VERIFY</span>
        </label>
      </div>
    </div>
  </div>
  {:else if section === "login"}
  <div class="grid gap-px bg-line xl:grid-cols-2">
    <div class="grid gap-px bg-line p-px">
      <div class="flex items-center justify-between bg-panel px-3 py-2">
        <span class="t-label text-fg">[ PASSWORD LOGIN ]</span>
        <button class="btn-t-solid" disabled={busy} on:click={() => saveSetting("password")}>SAVE PASSWORD</button>
      </div>
      <div class="grid gap-px bg-line md:grid-cols-2">
        <label class="flex items-center gap-3 bg-panel p-3 text-xs font-bold uppercase tracking-[0.15em]">
          <input type="checkbox" checked={getSettingBool("password", ["disabled"])} class={checkboxClass(getSettingBool("password", ["disabled"]), "danger")} on:change={(event) => setSettingBool("password", ["disabled"], checkedValue(event))} />
          <span class={getSettingBool("password", ["disabled"]) ? "text-alert" : "text-dim"}>DISABLE PASSWORD GRANT</span>
        </label>
        <label class="flex items-center gap-3 bg-panel p-3 text-xs font-bold uppercase tracking-[0.15em]">
          <input type="checkbox" checked={getSettingBool("password", ["local_disabled"])} class={checkboxClass(getSettingBool("password", ["local_disabled"]), "danger")} on:change={(event) => setSettingBool("password", ["local_disabled"], checkedValue(event))} />
          <span class={getSettingBool("password", ["local_disabled"]) ? "text-alert" : "text-dim"}>DISABLE LOCAL USER PASSWORDS</span>
        </label>
        <label class="flex items-center gap-3 bg-panel p-3 text-xs font-bold uppercase tracking-[0.15em]">
          <input type="checkbox" checked={getSettingBool("password", ["ldap_disabled"])} class={checkboxClass(getSettingBool("password", ["ldap_disabled"]), "danger")} on:change={(event) => setSettingBool("password", ["ldap_disabled"], checkedValue(event))} />
          <span class={getSettingBool("password", ["ldap_disabled"]) ? "text-alert" : "text-dim"}>DISABLE LDAP PASSWORDS</span>
        </label>
        <label class="flex items-center gap-3 bg-panel p-3 text-xs font-bold uppercase tracking-[0.15em]">
          <input type="checkbox" checked={getSettingBool("password", ["ldap_register_disabled"])} class={checkboxClass(getSettingBool("password", ["ldap_register_disabled"]), "danger")} on:change={(event) => setSettingBool("password", ["ldap_register_disabled"], checkedValue(event))} />
          <span class={getSettingBool("password", ["ldap_register_disabled"]) ? "text-alert" : "text-dim"}>DISABLE LDAP AUTO-REGISTER</span>
        </label>
        <p class="bg-panel p-3 text-[11px] leading-4 text-dim md:col-span-2">
          Local users verify against the stored bcrypt password; non-local users bind against LDAP. Unknown aliases are created from LDAP on first login unless auto-register is disabled.
        </p>
      </div>
    </div>

    <div class="grid gap-px bg-line p-px">
      <div class="flex items-center justify-between bg-panel px-3 py-2">
        <span class="t-label text-fg">[ PASSKEY / WEBAUTHN ]</span>
        <button class="btn-t-solid" disabled={busy} on:click={() => saveSetting("passkey")}>SAVE PASSKEY</button>
      </div>
      <div class="grid gap-px bg-line md:grid-cols-2">
        <label class="flex items-center gap-3 bg-panel p-3 text-xs font-bold uppercase tracking-[0.15em]">
          <input type="checkbox" checked={getSettingBool("passkey", ["disabled"])} class={checkboxClass(getSettingBool("passkey", ["disabled"]), "danger")} on:change={(event) => setSettingBool("passkey", ["disabled"], checkedValue(event))} />
          <span class={getSettingBool("passkey", ["disabled"]) ? "text-alert" : "text-dim"}>DISABLE PASSKEY</span>
        </label>
        <label class="grid gap-1 bg-panel p-3">
          <span class="t-label">USER VERIFICATION</span>
          <select class="field-t" on:change={setPasskeyUserVerification}>
            <option value="preferred" selected={passkeyUserVerification === "preferred"}>preferred</option>
            <option value="required" selected={passkeyUserVerification === "required"}>required</option>
            <option value="discouraged" selected={passkeyUserVerification === "discouraged"}>discouraged</option>
          </select>
        </label>
        <label class="grid gap-1 bg-panel p-3">
          <span class="t-label">RP ID</span>
          <input class="field-t" value={getSettingString("passkey", ["rp_id"])} placeholder="derived from request host" on:input={(event) => setSettingString("passkey", ["rp_id"], inputValue(event))} />
        </label>
        <label class="grid gap-1 bg-panel p-3">
          <span class="t-label">RP DISPLAY NAME</span>
          <input class="field-t" value={getSettingString("passkey", ["rp_display_name"])} placeholder="Turna Auth" on:input={(event) => setSettingString("passkey", ["rp_display_name"], inputValue(event))} />
        </label>
        <label class="grid gap-1 bg-panel p-3 md:col-span-2">
          <span class="t-label">ORIGINS</span>
          <input class="field-t" value={getSettingList("passkey", ["origins"])} placeholder="derived from request, e.g. https://app.example.com" on:input={(event) => setSettingList("passkey", ["origins"], inputValue(event))} />
        </label>
        <p class="bg-panel p-3 text-[11px] leading-4 text-dim md:col-span-2">
          Set RP ID and origins explicitly when login pages are served from a different domain than this auth host.
        </p>
      </div>
    </div>
  </div>
  {:else if section === "jwt"}
  <div class="grid gap-px bg-line p-px">
      <div class="flex items-center justify-between bg-panel px-3 py-2">
        <span class="t-label text-fg">[ JWT / CERTS ]</span>
        <div class="flex gap-px">
          <a class="btn-t border-0 bg-crt" href={`${oauthBase}/oauth2/certs`} target="_blank" rel="noreferrer">JWKS</a>
          <a class="btn-t border-0 bg-crt" href={`${oauthBase}/oauth2/.well-known/openid-configuration`} target="_blank" rel="noreferrer">OPENID</a>
          <button class="btn-t border-0 bg-crt text-alert" disabled={busy} on:click={() => rotateJWT()}>ROTATE KEY</button>
          <button class="btn-t-solid" disabled={busy} on:click={() => saveSetting("jwt")}>SAVE JWT</button>
        </div>
      </div>
      <div class="grid gap-px bg-line md:grid-cols-2">
        <label class="grid gap-1 bg-panel p-3">
          <span class="t-label">KEY ID (KID)</span>
          <input class="field-t" value={getSettingString("jwt", ["kid"])} placeholder="turna-auth-..." on:input={(event) => setSettingString("jwt", ["kid"], inputValue(event))} />
        </label>
        <div class="grid gap-1 bg-panel p-3">
          <span class="t-label">SIGNING ALG</span>
          <p class="text-sm font-bold text-fg">{fieldText(jwksKey.alg) || "RS256"}</p>
        </div>
        <label class="grid gap-1 bg-panel p-3 md:col-span-2">
          <span class="t-label">RSA PRIVATE KEY (PEM, PKCS#8 OR PKCS#1)</span>
          <textarea class="field-t min-h-40 text-[11px] leading-4" spellcheck="false" value={getSettingString("jwt", ["private_key"])} placeholder={"-----BEGIN PRIVATE KEY-----\n..."} on:input={(event) => setSettingString("jwt", ["private_key"], inputValue(event))}></textarea>
        </label>
        <p class="bg-panel p-3 text-[11px] leading-4 text-dim md:col-span-2">
          Stored encrypted in the <span class="text-fg">jwt</span> namespace; auto-generated on first start. The public key is derived from the private key and published through JWKS. Saving a different key or rotating <span class="text-alert">invalidates all outstanding access and refresh tokens</span>; changes apply without restart.
        </p>
      </div>
    </div>
  {/if}
</div>
