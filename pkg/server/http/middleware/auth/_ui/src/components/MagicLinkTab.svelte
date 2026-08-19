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
  export let saveSetting: (namespace: SettingNamespace) => void | Promise<void> = () => {};

  type EmailPreview = {
    subject: string;
    body: string;
    magic_link?: string;
    data?: AnyRecord;
  };

  const defaultMagicBodyTemplate = `Click the link to sign in:

{{.MagicLink}}

Or use this one-time code: {{.Code}}

The link expires in {{.ExpiresIn}}.`;

  let previewEmail = "user@example.com";
  let previewCode = "123456";
  let previewClientID = "ui";
  let previewRedirectURI = "https://app.example.com/login/";
  let preview: EmailPreview | null = null;
  let previewBusy = false;
  let previewError = "";

  $: magicLink = settingBool(settingsRevision, ["magic_link"], true);
  $: codeDisabled = settingBool(settingsRevision, ["disabled"]);
  $: smtpHost = settingString(settingsRevision, ["smtp", "host"]);
  $: magicSubject = settingString(settingsRevision, ["magic_link_subject"]);
  $: magicBodyTemplate = settingString(settingsRevision, ["magic_link_body_template"]);

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

  // preview the magic-link mail: force magic_link on and send a redirect_uri
  async function renderPreview() {
    previewBusy = true;
    previewError = "";

    const settings = { ...settingRecord("email"), magic_link: true };

    try {
      const res = await fetch(`${apiBase}/email/preview`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          settings,
          email: previewEmail.trim(),
          code: previewCode.trim(),
          client_id: previewClientID.trim(),
          redirect_uri: previewRedirectURI.trim(),
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
      previewError = err instanceof Error ? err.message : "Cannot render preview";
    } finally {
      previewBusy = false;
    }
  }
</script>

<div class="grid gap-px bg-line p-px">
  <div class="grid gap-3 bg-panel p-4">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div>
        <span class="t-label text-fg">Magic link</span>
        <h3 class="mt-2 font-display text-3xl leading-none tracking-tight md:text-4xl">Magic Link Login</h3>
      </div>
      <button class="btn-t-solid" disabled={busy} on:click={() => saveSetting("email")}>Save magic link</button>
    </div>
    <p class="max-w-3xl text-xs leading-5 text-dim">
      The magic link mail is sent during email login when the request carries a <span class="text-fg">redirect_uri</span> allowed by the OAuth client's whitelist; the link is <span class="text-fg">redirect_uri?code=...</span>. It shares the SMTP relay and code lifetime configured on the <span class="text-fg">Email</span> page.
    </p>
    {#if smtpHost === ""}
      <p class="text-xs leading-4 text-alert">SMTP host is not set — configure delivery on the Email page first.</p>
    {/if}
    {#if codeDisabled && magicLink}
      <p class="text-xs leading-4 text-dim">Code login is disabled; magic link mails are still sent when a redirect_uri is provided.</p>
    {/if}
  </div>

  <div class="grid gap-px bg-line xl:grid-cols-[minmax(0,1fr),minmax(0,1fr)]">
    <div class="grid content-start gap-px bg-line">
      <div class="bg-panel px-3 py-2">
        <span class="t-label text-fg">Magic link settings</span>
      </div>
      <label class="flex items-center gap-3 bg-panel p-3 text-xs font-bold">
        <input type="checkbox" checked={magicLink} class={checkboxClass(magicLink)} on:change={(event) => setSettingBool("email", ["magic_link"], checkedValue(event))} />
        <span class={magicLink ?"text-fg" :"text-alert"}>{magicLink ?"Magic link enabled" :"Magic link disabled"}</span>
      </label>
      <div class="bg-panel p-3 text-xs leading-5 text-dim">
        Turn this off to send only the one-time code during email login, even when a redirect_uri is provided. Useful when the SMTP relay is shared with signup and you don't want login magic links.
      </div>
    </div>

    <div class="grid content-start gap-px bg-line">
      <div class="bg-panel px-3 py-2">
        <span class="t-label text-fg">Magic link mail template</span>
      </div>
      <label class="grid gap-1 bg-panel p-3">
        <span class="t-label">Subject</span>
        <input class="field-t" value={magicSubject} placeholder="Your login link" disabled={!magicLink} on:input={(event) => setSettingString("email", ["magic_link_subject"], inputValue(event))} />
      </label>
      <label class="grid gap-1 bg-panel p-3">
        <span class="t-label">Body template</span>
        <textarea class="field-t min-h-48 text-xs leading-4" value={magicBodyTemplate} placeholder="empty = built-in magic-link body" disabled={!magicLink} on:input={(event) => setSettingString("email", ["magic_link_body_template"], inputValue(event))}></textarea>
      </label>
      <div class="flex flex-wrap gap-2 bg-panel p-3">
        <button class="btn-t" disabled={busy || !magicLink} on:click={() => setSettingString("email", ["magic_link_body_template"], defaultMagicBodyTemplate)}>Use default</button>
        <button class="btn-t" disabled={busy || !magicLink} on:click={() => setSettingString("email", ["magic_link_body_template"],"")}>Use built-in</button>
      </div>
      <div class="bg-panel p-3 text-xs leading-5 text-dim">
        Variables: <code class="text-fg">{"{{.Email}}"}</code>, <code class="text-fg">{"{{.Code}}"}</code>, <code class="text-fg">{"{{.MagicLink}}"}</code>, <code class="text-fg">{"{{.ExpiresIn}}"}</code>, <code class="text-fg">{"{{.ClientID}}"}</code>, <code class="text-fg">{"{{.RedirectURI}}"}</code>, <code class="text-fg">{"{{.UserID}}"}</code>, <code class="text-fg">{"{{.UserAlias}}"}</code>.
      </div>
    </div>
  </div>

  <div class="grid gap-px bg-line lg:grid-cols-[360px,minmax(0,1fr)]">
    <div class="grid content-start gap-px bg-line">
      <div class="bg-panel px-3 py-2">
        <span class="t-label text-fg">Magic link preview</span>
      </div>
      <label class="grid gap-1 bg-panel p-3">
        <span class="t-label">Email</span>
        <input bind:value={previewEmail} class="field-t" placeholder="user@example.com" />
      </label>
      <label class="grid gap-1 bg-panel p-3">
        <span class="t-label">Code</span>
        <input bind:value={previewCode} class="field-t" placeholder="123456" />
      </label>
      <label class="grid gap-1 bg-panel p-3">
        <span class="t-label">Client ID</span>
        <input bind:value={previewClientID} class="field-t" placeholder="ui" />
      </label>
      <label class="grid gap-1 bg-panel p-3">
        <span class="t-label">Redirect URI</span>
        <input bind:value={previewRedirectURI} class="field-t" placeholder="https://app.example.com/login/" />
      </label>
      <div class="bg-panel p-3">
        <button class="btn-t-solid w-full" disabled={previewBusy} on:click={renderPreview}>Render preview</button>
      </div>
    </div>

    <div class="grid content-start gap-px bg-line">
      <div class="bg-panel px-3 py-2">
        <span class="t-label text-fg">Rendered mail</span>
      </div>
      {#if previewError}
        <div class="bg-panel p-3 text-xs text-alert">{previewError}</div>
      {:else if preview}
        <div class="grid gap-3 bg-panel p-4">
          <p class="break-all text-xs"><span class="t-label">Subject</span> <span class="text-fg">{preview.subject}</span></p>
          {#if preview.magic_link}
            <p class="break-all text-xs leading-4 text-dim"><span class="t-label text-fg">Magic link</span> {preview.magic_link}</p>
          {/if}
          <pre class="overflow-auto whitespace-pre-wrap border border-line bg-crt p-3 text-xs leading-5 text-fg">{preview.body}</pre>
        </div>
      {:else}
        <div class="bg-panel p-4 text-xs leading-4 text-dim">Render a preview to validate the Go template before saving.</div>
      {/if}
    </div>
  </div>
</div>
