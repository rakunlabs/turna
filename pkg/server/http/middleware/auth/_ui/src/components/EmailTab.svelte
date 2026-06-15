<script lang="ts">
  import type { AnyRecord, SettingNamespace } from "../lib/api";

  export let apiBase = "/auth/v1";
  export let busy = false;
  export let settingsRevision = 0;
  export let settingRecord: (namespace: SettingNamespace) => AnyRecord = () => ({});
  export let getSettingBool: (namespace: SettingNamespace, path: string[], fallback?: boolean) => boolean = () => false;
  export let setSettingBool: (namespace: SettingNamespace, path: string[], value: boolean) => void = () => {};
  export let getSettingString: (namespace: SettingNamespace, path: string[]) => string = () => "";
  export let setSettingString: (namespace: SettingNamespace, path: string[], value: string) => void = () => {};
  export let getSettingNumber: (namespace: SettingNamespace, path: string[], fallback?: number) => number = () => 0;
  export let setSettingNumber: (namespace: SettingNamespace, path: string[], value: string) => void = () => {};
  export let saveSetting: (namespace: SettingNamespace) => void | Promise<void> = () => {};

  type EmailPreview = {
    subject: string;
    body: string;
    magic_link?: string;
    data?: AnyRecord;
  };

  const defaultCodeBodyTemplate = `Your one-time login code is:

{{.Code}}

The code expires in {{.ExpiresIn}}.`;

  let previewEmail = "user@example.com";
  let previewCode = "123456";
  let previewClientID = "ui";
  let preview: EmailPreview | null = null;
  let previewBusy = false;
  let previewError = "";

  type Section = "connection" | "template";
  let section: Section = "connection";
  const sections: { id: Section; label: string }[] = [
    { id: "connection", label: "Connection" },
    { id: "template", label: "Template & preview" },
  ];

  $: disabled = settingBool(settingsRevision, ["disabled"]);
  $: magicLink = settingBool(settingsRevision, ["magic_link"], true);
  $: from = settingString(settingsRevision, ["from"]);
  $: subject = settingString(settingsRevision, ["subject"]);
  $: bodyTemplate = settingString(settingsRevision, ["body_template"]);
  $: codeLifetime = settingString(settingsRevision, ["code_lifetime"]);
  $: smtpHost = settingString(settingsRevision, ["smtp", "host"]);
  $: smtpPort = getSettingNumber("email", ["smtp", "port"], 587);
  $: smtpUsername = settingString(settingsRevision, ["smtp", "username"]);
  $: smtpPassword = settingString(settingsRevision, ["smtp", "password"]);
  $: smtpNoAuth = settingBool(settingsRevision, ["smtp", "no_auth"]);
  $: smtpStartTLS = settingBool(settingsRevision, ["smtp", "starttls"], true);
  $: smtpTLS = settingBool(settingsRevision, ["smtp", "tls"]);
  $: smtpInsecureSkipVerify = settingBool(settingsRevision, ["smtp", "insecure_skip_verify"]);

  function inputValue(event: Event) {
    return (event.currentTarget as HTMLInputElement | HTMLTextAreaElement).value;
  }

  function checkedValue(event: Event) {
    return (event.currentTarget as HTMLInputElement).checked;
  }

  function settingBool(_revision: number, path: string[], fallback = false) {
    return getSettingBool("email", path, fallback);
  }

  function settingString(_revision: number, path: string[]) {
    return getSettingString("email", path);
  }

  function checkboxClass(checked: boolean, danger = false) {
    const base = "h-3.5 w-3.5 appearance-none border bg-crt";
    if (danger) return `${base} border-line checked:bg-alert`;

    return `${base} border-line checked:bg-fg ${checked ? "" : "border-alert"}`;
  }

  // preview the one-time code mail (no redirect_uri so no magic link)
  async function renderPreview() {
    previewBusy = true;
    previewError = "";

    try {
      const res = await fetch(`${apiBase}/email/preview`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          settings: settingRecord("email"),
          email: previewEmail.trim(),
          code: previewCode.trim(),
          client_id: previewClientID.trim(),
        }),
      });

      let body: AnyRecord = {};
      try {
        body = await res.json();
      } catch {
        // keep status text
      }
      if (!res.ok) {
        throw new Error(String(body.message ?? body.error ?? res.statusText));
      }

      preview = body.payload as EmailPreview;
    } catch (err) {
      preview = null;
      previewError = err instanceof Error ? err.message : "CANNOT RENDER PREVIEW";
    } finally {
      previewBusy = false;
    }
  }
</script>

<div class="grid gap-px bg-line p-px">
  <div class="grid gap-3 bg-panel p-4">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div>
        <span class="t-label text-fg">[ EMAIL ]</span>
        <h3 class="mt-2 font-display text-3xl uppercase leading-none tracking-tight md:text-4xl">Email Code Login</h3>
      </div>
      <button class="btn-t-solid" disabled={busy} on:click={() => saveSetting("email")}>[ SAVE EMAIL ]</button>
    </div>
    <p class="max-w-3xl text-xs leading-5 text-dim">
      Passwordless one-time code login over SMTP. The magic link mail is configured on its own <span class="text-fg">MAGIC LINK</span> page; the same SMTP relay here is also used by magic link, signup verification and password reset.
    </p>
    {#if magicLink}
      <p class="text-[11px] leading-4 text-dim">MAGIC LINK LOGIN IS <span class="text-fg">ENABLED</span> — manage it on the MAGIC LINK page.</p>
    {/if}
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

  {#if section === "connection"}
    <div class="grid content-start gap-px bg-line">
      <div class="bg-panel px-3 py-2">
        <span class="t-label text-fg">[ DELIVERY SETTINGS ]</span>
      </div>
      <label class="flex items-center gap-3 bg-panel p-3 text-xs font-bold uppercase tracking-[0.15em]">
        <input type="checkbox" checked={disabled} class={checkboxClass(disabled, true)} on:change={(event) => setSettingBool("email", ["disabled"], checkedValue(event))} />
        <span class={disabled ? "text-alert" : "text-dim"}>DISABLE CODE LOGIN</span>
      </label>
      <div class="grid gap-px bg-line md:grid-cols-2">
        <label class="grid gap-1 bg-panel p-3">
          <span class="t-label">FROM</span>
          <input class="field-t" value={from} placeholder="auth@example.com" on:input={(event) => setSettingString("email", ["from"], inputValue(event))} />
        </label>
        <label class="grid gap-1 bg-panel p-3">
          <span class="t-label">CODE LIFETIME</span>
          <input class="field-t" value={codeLifetime} placeholder="15m" on:input={(event) => setSettingString("email", ["code_lifetime"], inputValue(event))} />
        </label>
        <label class="grid gap-1 bg-panel p-3">
          <span class="t-label">SMTP HOST</span>
          <input class="field-t" value={smtpHost} placeholder="smtp.example.com" on:input={(event) => setSettingString("email", ["smtp", "host"], inputValue(event))} />
        </label>
        <label class="grid gap-1 bg-panel p-3">
          <span class="t-label">SMTP PORT</span>
          <input class="field-t" type="number" min="1" value={smtpPort} on:input={(event) => setSettingNumber("email", ["smtp", "port"], inputValue(event))} />
        </label>
        <label class="grid gap-1 bg-panel p-3">
          <span class="t-label">SMTP USERNAME</span>
          <input class="field-t" value={smtpUsername} placeholder="optional" on:input={(event) => setSettingString("email", ["smtp", "username"], inputValue(event))} />
        </label>
        <label class="grid gap-1 bg-panel p-3">
          <span class="t-label">SMTP PASSWORD</span>
          <input class="field-t" value={smtpPassword} placeholder={smtpNoAuth ? "not used while no pass is enabled" : "optional"} disabled={smtpNoAuth} on:input={(event) => setSettingString("email", ["smtp", "password"], inputValue(event))} />
        </label>
      </div>
      <div class="grid gap-px bg-line md:grid-cols-3">
        <label class="flex items-center gap-3 bg-panel p-3 text-xs font-bold uppercase tracking-[0.15em]">
          <input type="checkbox" checked={smtpNoAuth} class={checkboxClass(smtpNoAuth)} on:change={(event) => setSettingBool("email", ["smtp", "no_auth"], checkedValue(event))} />
          <span class={smtpNoAuth ? "text-fg" : "text-dim"}>NO PASS / SKIP SMTP AUTH</span>
        </label>
        <label class="flex items-center gap-3 bg-panel p-3 text-xs font-bold uppercase tracking-[0.15em]">
          <input type="checkbox" checked={smtpStartTLS} class={checkboxClass(smtpStartTLS)} on:change={(event) => setSettingBool("email", ["smtp", "starttls"], checkedValue(event))} />
          <span class={smtpStartTLS ? "text-fg" : "text-dim"}>STARTTLS</span>
        </label>
        <label class="flex items-center gap-3 bg-panel p-3 text-xs font-bold uppercase tracking-[0.15em]">
          <input type="checkbox" checked={smtpTLS} class={checkboxClass(smtpTLS)} on:change={(event) => setSettingBool("email", ["smtp", "tls"], checkedValue(event))} />
          <span class={smtpTLS ? "text-fg" : "text-dim"}>IMPLICIT TLS</span>
        </label>
        <label class="flex items-center gap-3 bg-panel p-3 text-xs font-bold uppercase tracking-[0.15em] md:col-span-3">
          <input type="checkbox" checked={smtpInsecureSkipVerify} class={checkboxClass(smtpInsecureSkipVerify, true)} on:change={(event) => setSettingBool("email", ["smtp", "insecure_skip_verify"], checkedValue(event))} />
          <span class={smtpInsecureSkipVerify ? "text-alert" : "text-dim"}>INSECURE TLS SKIP VERIFY</span>
        </label>
      </div>
    </div>
  {:else if section === "template"}
    <div class="grid content-start gap-px bg-line">
      <div class="bg-panel px-3 py-2">
        <span class="t-label text-fg">[ CODE MAIL TEMPLATE ]</span>
      </div>
      <label class="grid gap-1 bg-panel p-3">
        <span class="t-label">CODE SUBJECT</span>
        <input class="field-t" value={subject} placeholder="Your login code" on:input={(event) => setSettingString("email", ["subject"], inputValue(event))} />
      </label>
      <label class="grid gap-1 bg-panel p-3">
        <span class="t-label">CODE BODY TEMPLATE</span>
        <textarea class="field-t min-h-48 text-[11px] leading-4" value={bodyTemplate} placeholder="empty = built-in code body" on:input={(event) => setSettingString("email", ["body_template"], inputValue(event))}></textarea>
      </label>
      <div class="flex flex-wrap gap-2 bg-panel p-3">
        <button class="btn-t" disabled={busy} on:click={() => setSettingString("email", ["body_template"], defaultCodeBodyTemplate)}>USE DEFAULT</button>
        <button class="btn-t" disabled={busy} on:click={() => setSettingString("email", ["body_template"], "")}>USE BUILT-IN</button>
      </div>
      <div class="bg-panel p-3 text-[11px] leading-5 text-dim">
        Variables: <code class="text-fg">{"{{.Email}}"}</code>, <code class="text-fg">{"{{.Code}}"}</code>, <code class="text-fg">{"{{.ExpiresIn}}"}</code>, <code class="text-fg">{"{{.ClientID}}"}</code>, <code class="text-fg">{"{{.RedirectURI}}"}</code>, <code class="text-fg">{"{{.UserID}}"}</code>, <code class="text-fg">{"{{.UserAlias}}"}</code>.
      </div>
    </div>

    <div class="grid gap-px bg-line lg:grid-cols-[360px,minmax(0,1fr)]">
      <div class="grid content-start gap-px bg-line">
        <div class="bg-panel px-3 py-2">
          <span class="t-label text-fg">[ CODE MAIL PREVIEW ]</span>
        </div>
        <label class="grid gap-1 bg-panel p-3">
          <span class="t-label">EMAIL</span>
          <input bind:value={previewEmail} class="field-t" placeholder="user@example.com" />
        </label>
        <label class="grid gap-1 bg-panel p-3">
          <span class="t-label">CODE</span>
          <input bind:value={previewCode} class="field-t" placeholder="123456" />
        </label>
        <label class="grid gap-1 bg-panel p-3">
          <span class="t-label">CLIENT ID</span>
          <input bind:value={previewClientID} class="field-t" placeholder="ui" />
        </label>
        <div class="bg-panel p-3">
          <button class="btn-t-solid w-full" disabled={previewBusy} on:click={renderPreview}>[ RENDER PREVIEW ]</button>
        </div>
      </div>

      <div class="grid content-start gap-px bg-line">
        <div class="bg-panel px-3 py-2">
          <span class="t-label text-fg">[ RENDERED MAIL ]</span>
        </div>
        {#if previewError}
          <div class="bg-panel p-3 text-xs text-alert">{previewError}</div>
        {:else if preview}
          <div class="grid gap-3 bg-panel p-4">
            <p class="break-all text-xs"><span class="t-label">SUBJECT</span> <span class="text-fg">{preview.subject}</span></p>
            <pre class="overflow-auto whitespace-pre-wrap border border-line bg-crt p-3 text-[11px] leading-5 text-fg">{preview.body}</pre>
          </div>
        {:else}
          <div class="bg-panel p-4 text-[11px] leading-4 text-dim">Render a preview to validate the Go template before saving.</div>
        {/if}
      </div>
    </div>
  {/if}
</div>
