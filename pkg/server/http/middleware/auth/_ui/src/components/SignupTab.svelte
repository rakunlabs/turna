<script lang="ts">
  import type { AnyRecord, SettingNamespace } from "../lib/api";

  export let apiBase = "/auth/v1";
  export let busy = false;
  export let settingsRevision = 0;
  export let getSettingBool: (namespace: SettingNamespace, path: string[], fallback?: boolean) => boolean = () => false;
  export let setSettingBool: (namespace: SettingNamespace, path: string[], value: boolean) => void = () => {};
  export let getSettingString: (namespace: SettingNamespace, path: string[]) => string = () => "";
  export let setSettingString: (namespace: SettingNamespace, path: string[], value: string) => void = () => {};
  export let getSettingList: (namespace: SettingNamespace, path: string[]) => string = () => "";
  export let setSettingList: (namespace: SettingNamespace, path: string[], value: string) => void = () => {};
  export let getSettingNumber: (namespace: SettingNamespace, path: string[], fallback?: number) => number = () => 0;
  export let setSettingNumber: (namespace: SettingNamespace, path: string[], value: string) => void = () => {};
  export let saveSetting: (namespace: SettingNamespace) => void | Promise<void> = () => {};

  type EmailPreview = {
    subject: string;
    body: string;
    magic_link?: string;
  };

  let previewKind: "verify" | "reset" = "verify";
  let previewEmail = "user@example.com";
  let previewCode = "123456";
  let previewRedirectURI = "https://app.example.com/login/?flow=verify";
  let preview: EmailPreview | null = null;
  let previewBusy = false;
  let previewError = "";

  $: enabled = settingBool(settingsRevision, ["enabled"]);
  $: emailVerification = settingBool(settingsRevision, ["email_verification"], true);
  $: passwordReset = settingBool(settingsRevision, ["password_reset"]);
  $: passwordMinLength = settingNumber(settingsRevision, ["password_min_length"], 8);
  $: codeLifetime = settingString(settingsRevision, ["code_lifetime"]);
  $: defaultRoleIDs = settingList(settingsRevision, ["default_role_ids"]);
  $: verifySubject = settingString(settingsRevision, ["verify_subject"]);
  $: verifyBody = settingString(settingsRevision, ["verify_body_template"]);
  $: resetSubject = settingString(settingsRevision, ["reset_subject"]);
  $: resetBody = settingString(settingsRevision, ["reset_body_template"]);

  function inputValue(event: Event) {
    return (event.currentTarget as HTMLInputElement | HTMLTextAreaElement).value;
  }

  function checkedValue(event: Event) {
    return (event.currentTarget as HTMLInputElement).checked;
  }

  function settingBool(_revision: number, path: string[], fallback = false) {
    return getSettingBool("signup", path, fallback);
  }

  function settingString(_revision: number, path: string[]) {
    return getSettingString("signup", path);
  }

  function settingList(_revision: number, path: string[]) {
    return getSettingList("signup", path);
  }

  function settingNumber(_revision: number, path: string[], fallback = 0) {
    return getSettingNumber("signup", path, fallback);
  }

  function checkboxClass(checked: boolean, danger = false) {
    const base = "h-3.5 w-3.5 appearance-none border bg-crt";
    if (danger) return `${base} border-line checked:bg-alert`;

    return `${base} border-line checked:bg-fg ${checked ? "" : "border-alert"}`;
  }

  const defaultVerifyBody = `{{if .MagicLink}}Click the link to verify your email:

{{.MagicLink}}

Or use this verification code: {{.Code}}{{else}}Your verification code is:

{{.Code}}{{end}}

The code expires in {{.ExpiresIn}}.`;

  const defaultResetBody = `{{if .MagicLink}}Click the link to reset your password:

{{.MagicLink}}

Or use this reset code: {{.Code}}{{else}}Your password reset code is:

{{.Code}}{{end}}

The code expires in {{.ExpiresIn}}.`;

  // signup verify/reset mails reuse the email preview endpoint: the selected
  // subject/body templates are passed as a synthetic email settings payload.
  async function renderPreview() {
    previewBusy = true;
    previewError = "";

    const subject = previewKind === "verify" ? verifySubject : resetSubject;
    const body = previewKind === "verify" ? verifyBody : resetBody;
    const fallbackSubject = previewKind === "verify" ? "Verify your email" : "Reset your password";
    const fallbackBody = previewKind === "verify" ? defaultVerifyBody : defaultResetBody;

    try {
      const res = await fetch(`${apiBase}/email/preview`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          settings: {
            subject: subject || fallbackSubject,
            body_template: body || fallbackBody,
            code_lifetime: codeLifetime || "1h",
          },
          email: previewEmail.trim(),
          code: previewCode.trim(),
          redirect_uri: previewRedirectURI.trim(),
        }),
      });

      let payload: AnyRecord = {};
      try {
        payload = await res.json();
      } catch {
        // keep status text
      }
      if (!res.ok) {
        throw new Error(String(payload.message ?? payload.error ?? res.statusText));
      }

      preview = payload.payload as EmailPreview;
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
        <span class="t-label text-fg">Signup</span>
        <h3 class="mt-2 font-display text-3xl leading-none tracking-tight md:text-4xl">Self Registration</h3>
      </div>
      <button class="btn-t-solid" disabled={busy} on:click={() => saveSetting("signup")}>Save signup</button>
    </div>
    <p class="max-w-3xl text-xs leading-5 text-dim">
      Optional self-registration and forgot-password flows for the login page. Signup creates <span class="text-fg">local</span> users; email verification and password reset deliver one-time codes/magic links over the SMTP relay configured on the <span class="text-fg">Email</span> page.
    </p>
  </div>

  <div class="grid gap-px bg-line xl:grid-cols-[minmax(0,1fr),minmax(0,1fr)]">
    <div class="grid content-start gap-px bg-line">
      <div class="bg-panel px-3 py-2">
        <span class="t-label text-fg">Flow settings</span>
      </div>
      <label class="flex items-center gap-3 bg-panel p-3 text-xs font-bold">
        <input type="checkbox" checked={enabled} class={checkboxClass(enabled)} on:change={(event) => setSettingBool("signup", ["enabled"], checkedValue(event))} />
        <span class={enabled ?"text-fg" :"text-dim"}>{enabled ?"Signup enabled" :"Signup disabled"}</span>
      </label>
      <label class="flex items-center gap-3 bg-panel p-3 text-xs font-bold">
        <input type="checkbox" checked={emailVerification} class={checkboxClass(emailVerification)} on:change={(event) => setSettingBool("signup", ["email_verification"], checkedValue(event))} />
        <span class={emailVerification ?"text-fg" :"text-alert"}>Email verification required</span>
      </label>
      <label class="flex items-center gap-3 bg-panel p-3 text-xs font-bold">
        <input type="checkbox" checked={passwordReset} class={checkboxClass(passwordReset)} on:change={(event) => setSettingBool("signup", ["password_reset"], checkedValue(event))} />
        <span class={passwordReset ?"text-fg" :"text-dim"}>Password reset over email</span>
      </label>
      <label class="grid gap-1 bg-panel p-3">
        <span class="t-label">Password min length</span>
        <input type="number" min="1" class="field-t" value={passwordMinLength} placeholder="8" on:input={(event) => setSettingNumber("signup", ["password_min_length"], inputValue(event))} />
        <span class="text-xs leading-4 text-dim">Enforced on signup, password reset and self-service password change; the login page reflects it live. Default 8.</span>
      </label>
      <label class="grid gap-1 bg-panel p-3">
        <span class="t-label">Default role IDs</span>
        <input class="field-t" value={defaultRoleIDs} placeholder="role-id-a, role-id-b" on:input={(event) => setSettingList("signup", ["default_role_ids"], inputValue(event))} />
        <span class="text-xs leading-4 text-dim">Granted to every user created through signup.</span>
      </label>
      <label class="grid gap-1 bg-panel p-3">
        <span class="t-label">Code lifetime</span>
        <input class="field-t" value={codeLifetime} placeholder="1h" on:input={(event) => setSettingString("signup", ["code_lifetime"], inputValue(event))} />
        <span class="text-xs leading-4 text-dim">Verification and reset codes are single use and expire after this duration.</span>
      </label>
      <div class="bg-panel p-3 text-xs leading-5 text-dim">
        Without email verification, signup creates active accounts immediately and duplicate addresses answer 409. With verification (recommended), the account exists only after the code is confirmed and responses never reveal whether an address is registered.
      </div>
    </div>

    <div class="grid content-start gap-px bg-line">
      <div class="bg-panel px-3 py-2">
        <span class="t-label text-fg">Mail templates</span>
      </div>
      <label class="grid gap-1 bg-panel p-3">
        <span class="t-label">Verify subject</span>
        <input class="field-t" value={verifySubject} placeholder="Verify your email" on:input={(event) => setSettingString("signup", ["verify_subject"], inputValue(event))} />
      </label>
      <label class="grid gap-1 bg-panel p-3">
        <span class="t-label">Verify body template</span>
        <textarea class="field-t min-h-32 text-xs leading-4" value={verifyBody} placeholder="empty = built-in template" on:input={(event) => setSettingString("signup", ["verify_body_template"], inputValue(event))}></textarea>
      </label>
      <label class="grid gap-1 bg-panel p-3">
        <span class="t-label">Reset subject</span>
        <input class="field-t" value={resetSubject} placeholder="Reset your password" on:input={(event) => setSettingString("signup", ["reset_subject"], inputValue(event))} />
      </label>
      <label class="grid gap-1 bg-panel p-3">
        <span class="t-label">Reset body template</span>
        <textarea class="field-t min-h-32 text-xs leading-4" value={resetBody} placeholder="empty = built-in template" on:input={(event) => setSettingString("signup", ["reset_body_template"], inputValue(event))}></textarea>
      </label>
      <div class="bg-panel p-3 text-xs leading-5 text-dim">
        Variables: <code class="text-fg">{"{{.Email}}"}</code>, <code class="text-fg">{"{{.Name}}"}</code>, <code class="text-fg">{"{{.Code}}"}</code>, <code class="text-fg">{"{{.MagicLink}}"}</code>, <code class="text-fg">{"{{.ExpiresIn}}"}</code>, <code class="text-fg">{"{{.ClientID}}"}</code>, <code class="text-fg">{"{{.RedirectURI}}"}</code>.
      </div>
    </div>
  </div>

  <div class="grid gap-px bg-line lg:grid-cols-[360px,minmax(0,1fr)]">
    <div class="grid content-start gap-px bg-line">
      <div class="bg-panel px-3 py-2">
        <span class="t-label text-fg">Preview input</span>
      </div>
      <div class="flex flex-wrap gap-px bg-panel p-3">
        <button
          class={`border px-2.5 py-1 text-xs font-bold ${previewKind ==="verify" ?"border-alert bg-alert text-white" :"border-line text-dim hover:text-fg"}`}
          on:click={() => (previewKind = "verify")}
        >
          Verify mail
        </button>
        <button
          class={`border px-2.5 py-1 text-xs font-bold ${previewKind ==="reset" ?"border-alert bg-alert text-white" :"border-line text-dim hover:text-fg"}`}
          on:click={() => (previewKind = "reset")}
        >
          Reset mail
        </button>
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
        <span class="t-label">Redirect URI / magic link target</span>
        <input bind:value={previewRedirectURI} class="field-t" placeholder="https://app.example.com/login/?flow=verify" />
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

  <div class="grid gap-px bg-line lg:grid-cols-2">
    <div class="bg-panel p-4">
      <span class="t-label text-fg">Login page integration</span>
      <p class="mt-3 text-xs leading-5 text-dim">
        The <span class="text-fg">login</span> middleware detects these settings live and shows <span class="text-fg">Create account</span> / <span class="text-fg">Forgot password?</span> links automatically for password providers backed by this auth middleware. No login configuration is needed for in-process providers.
      </p>
      <pre class="mt-3 overflow-auto border border-line bg-crt p-3 text-xs leading-5 text-fg"># remote auth instance only:
# oauth2:
#   signup_url: https://auth.example.com/auth/oauth2/signup
#   password_reset_url: https://auth.example.com/auth/oauth2/password-reset</pre>
    </div>
    <div class="bg-panel p-4">
      <span class="t-label text-fg">Endpoints</span>
      <pre class="mt-3 overflow-auto border border-line bg-crt p-3 text-xs leading-5 text-fg">POST /auth/oauth2/signup
POST /auth/oauth2/signup/verify
POST /auth/oauth2/password-reset
POST /auth/oauth2/password-reset/confirm</pre>
      <p class="mt-3 text-xs leading-4 text-dim">
        Public endpoints requiring valid client credentials; responses never reveal whether an address is registered when verification is on.
      </p>
    </div>
  </div>
</div>
