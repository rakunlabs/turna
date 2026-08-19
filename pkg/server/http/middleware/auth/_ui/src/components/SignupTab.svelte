<script lang="ts">
  import Instrument from "./ui/Instrument.svelte";
  import Section from "./ui/Section.svelte";
  import Entry from "./ui/Entry.svelte";
  import Switch from "./ui/Switch.svelte";
  import Seal from "./ui/Seal.svelte";

  import { messageOf, session } from "../lib/state/session.svelte";
  import {
    getSettingBool,
    getSettingList,
    getSettingNumber,
    getSettingString,
    saveSetting,
    setSettingBool,
    setSettingList,
    setSettingNumber,
    setSettingString,
  } from "../lib/state/settings.svelte";

  type EmailPreview = {
    subject: string;
    body: string;
    magic_link?: string;
  };

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

  /** The fields both signup mails are rendered with. */
  const signupFields = [
    "{{.Email}}",
    "{{.Name}}",
    "{{.Code}}",
    "{{.MagicLink}}",
    "{{.ExpiresIn}}",
    "{{.ClientID}}",
    "{{.RedirectURI}}",
  ];

  const endpoints = [
    "POST /auth/oauth2/signup",
    "POST /auth/oauth2/signup/verify",
    "POST /auth/oauth2/password-reset",
    "POST /auth/oauth2/password-reset/confirm",
  ];

  const remoteConfig = `# remote auth instance only:
# oauth2:
#   signup_url: https://auth.example.com/auth/oauth2/signup
#   password_reset_url: https://auth.example.com/auth/oauth2/password-reset`;

  let previewKind = $state<"verify" | "reset">("verify");
  let previewEmail = $state("user@example.com");
  let previewCode = $state("123456");
  let previewRedirectURI = $state("https://app.example.com/login/?flow=verify");
  let preview = $state<EmailPreview | null>(null);
  let previewBusy = $state(false);
  let previewError = $state("");

  const enabled = $derived(getSettingBool("signup", ["enabled"]));
  const emailVerification = $derived(getSettingBool("signup", ["email_verification"], true));
  const passwordReset = $derived(getSettingBool("signup", ["password_reset"]));
  const codeLifetime = $derived(getSettingString("signup", ["code_lifetime"]));
  const verifySubject = $derived(getSettingString("signup", ["verify_subject"]));
  const verifyBody = $derived(getSettingString("signup", ["verify_body_template"]));
  const resetSubject = $derived(getSettingString("signup", ["reset_subject"]));
  const resetBody = $derived(getSettingString("signup", ["reset_body_template"]));

  /**
   * Self-registration is the one flow on this instance that creates a principal
   * for someone nobody vouched for, so the page opens by saying whether it is
   * open and on what terms.
   */
  const standing = $derived.by(() => {
    if (!enabled) {
      return {
        state: "void" as const,
        label: "Closed",
        detail:
          "Nobody can create an account here. The signup and password-reset endpoints answer as disabled, and the login page shows neither link.",
      };
    }

    if (!emailVerification) {
      return {
        state: "held" as const,
        label: "Open, unverified",
        detail:
          "Anyone who can reach the login page creates an active local user immediately, with no proof that the address is theirs. Duplicate addresses answer 409, which also confirms who is registered.",
      };
    }

    return {
      state: "endorsed" as const,
      label: "Open, verified",
      detail:
        "Anyone who can reach the login page can register, but the account exists only after the emailed code is confirmed. Responses never reveal whether an address is already registered.",
    };
  });

  // The signup mails reuse the email preview endpoint: the selected subject and
  // body are passed as a synthetic email settings payload.
  async function renderPreview() {
    previewBusy = true;
    previewError = "";

    const subject = previewKind === "verify" ? verifySubject : resetSubject;
    const body = previewKind === "verify" ? verifyBody : resetBody;
    const fallbackSubject = previewKind === "verify" ? "Verify your email" : "Reset your password";
    const fallbackBody = previewKind === "verify" ? defaultVerifyBody : defaultResetBody;

    try {
      const payload = await session.request<EmailPreview>("email/preview", {
        method: "POST",
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

      preview = payload.payload;
    } catch (err) {
      preview = null;
      previewError = messageOf(err, "Cannot render preview");
    } finally {
      previewBusy = false;
    }
  }
</script>

<Instrument
  title="Self registration"
  note="Optional signup and forgot-password flows for the login page. Signup creates local users; verification and reset codes go out over the SMTP relay configured on the Email page."
>
  {#snippet actions()}
    <button
      type="button"
      class="act act-primary"
      disabled={session.busy}
      onclick={() => saveSetting("signup")}
    >
      {session.busy ? "Committing…" : "Commit"}
    </button>
  {/snippet}

  {#snippet custody()}
    <span class="stamp">Namespace <span class="serial stamp-raw">signup</span></span>
    <span class="serial stamp-raw">{session.apiBase}/settings/signup</span>
  {/snippet}

  <div class="border border-rule bg-sheet px-4 py-3.5">
    <div class="flex flex-wrap items-baseline gap-x-5 gap-y-2">
      <span class="shrink-0"><Seal state={standing.state} label={standing.label} /></span>
      <p class="min-w-0 flex-1 basis-72 max-w-[70ch] text-[13px] leading-[1.6] text-ink">
        {standing.detail}
      </p>
    </div>
  </div>

  <Section title="Registration">
    <div class="grid gap-6">
      <Switch
        label="Accept self-registration"
        consequential
        hint="Registration is anonymous by design: the caller only needs valid client credentials. Turning this on lets strangers create principals in this instance."
        bind:checked={
          () => getSettingBool("signup", ["enabled"]),
          (v) => setSettingBool("signup", ["enabled"], v)
        }
      />

      <Switch
        label="Require email verification"
        hint="Recommended. The account is created only after the emailed code is confirmed, and responses stay uniform so signup cannot be used to test which addresses exist."
        bind:checked={
          () => getSettingBool("signup", ["email_verification"], true),
          (v) => setSettingBool("signup", ["email_verification"], v)
        }
      />

      <Switch
        label="Allow password reset over email"
        hint="Adds the forgot-password flow: a one-time code or link mailed to the registered address."
        bind:checked={
          () => getSettingBool("signup", ["password_reset"]),
          (v) => setSettingBool("signup", ["password_reset"], v)
        }
      />
    </div>

    {#if enabled && !passwordReset}
      <p class="mt-6 max-w-[70ch] text-[13px] leading-[1.6] text-muted">
        Password reset is off, so a local user who forgets their password needs an administrator to
        set a new one.
      </p>
    {/if}
  </Section>

  <Section title="Terms of the account">
    <div class="grid gap-6 sm:grid-cols-2">
      <Entry
        label="Password minimum length"
        type="number"
        mono
        hint="Enforced on signup, reset and self-service change. The login page reflects it live. Default 8."
        bind:value={
          () => String(getSettingNumber("signup", ["password_min_length"], 8)),
          (v) => setSettingNumber("signup", ["password_min_length"], v)
        }
      />

      <Entry
        label="Code lifetime"
        placeholder="1h"
        mono
        hint="Verification and reset codes are single use and expire after this."
        bind:value={
          () => getSettingString("signup", ["code_lifetime"]),
          (v) => setSettingString("signup", ["code_lifetime"], v)
        }
      />

      <div class="sm:col-span-2">
        <Entry
          label="Default role IDs"
          placeholder="role-id-a, role-id-b"
          mono
          hint="Granted to every user created through signup. Comma or newline separated."
          bind:value={
            () => getSettingList("signup", ["default_role_ids"]),
            (v) => setSettingList("signup", ["default_role_ids"], v)
          }
        />
      </div>
    </div>
  </Section>

  <Section
    title="Verification mail"
    note="Leave the body empty to send the built-in text. Anything you write here is a Go text/template rendered per recipient."
  >
    <Entry
      label="Subject"
      placeholder="Verify your email"
      bind:value={
        () => getSettingString("signup", ["verify_subject"]),
        (v) => setSettingString("signup", ["verify_subject"], v)
      }
    />

    <div class="mt-7">
      <div class="flex flex-wrap items-baseline gap-x-4 gap-y-1.5">
        <span class="stamp shrink-0">Body</span>
        <span class="min-w-0 flex-1"></span>
        <span class="stamp shrink-0">Fields</span>
      </div>

      <ul class="mt-1.5 flex flex-wrap items-center gap-x-4 gap-y-1">
        {#each signupFields as field (field)}
          <li class="serial text-[12px] text-muted">{field}</li>
        {/each}
      </ul>

      <textarea
        class="exhibit mt-2.5 min-h-[11rem]"
        spellcheck="false"
        placeholder="empty = built-in template"
        aria-label="Verification mail body template"
        value={verifyBody}
        oninput={(e) => setSettingString("signup", ["verify_body_template"], e.currentTarget.value)}
      ></textarea>

      <div class="mt-3 flex flex-wrap gap-2">
        <button
          type="button"
          class="act"
          disabled={session.busy}
          onclick={() => setSettingString("signup", ["verify_body_template"], defaultVerifyBody)}
        >
          Load the standard text
        </button>
        <button
          type="button"
          class="act"
          disabled={session.busy}
          onclick={() => setSettingString("signup", ["verify_body_template"], "")}
        >
          Clear to built-in
        </button>
      </div>

      <p class="mt-4 max-w-[70ch] text-[13px] leading-[1.6] text-muted">
        <span class="serial">{"{{.MagicLink}}"}</span> is empty unless the request supplies a
        redirect_uri, so guard it with
        <span class="serial">{"{{if .MagicLink}}"}</span> as the built-in text does.
      </p>
    </div>
  </Section>

  <Section
    title="Password reset mail"
    note="Sent by the forgot-password flow. Same fields, same rules."
  >
    <Entry
      label="Subject"
      placeholder="Reset your password"
      bind:value={
        () => getSettingString("signup", ["reset_subject"]),
        (v) => setSettingString("signup", ["reset_subject"], v)
      }
    />

    <div class="mt-7">
      <div class="flex flex-wrap items-baseline gap-x-4 gap-y-1.5">
        <span class="stamp shrink-0">Body</span>
        <span class="min-w-0 flex-1"></span>
        <span class="stamp shrink-0">Fields</span>
      </div>

      <ul class="mt-1.5 flex flex-wrap items-center gap-x-4 gap-y-1">
        {#each signupFields as field (field)}
          <li class="serial text-[12px] text-muted">{field}</li>
        {/each}
      </ul>

      <textarea
        class="exhibit mt-2.5 min-h-[11rem]"
        spellcheck="false"
        placeholder="empty = built-in template"
        aria-label="Password reset mail body template"
        value={resetBody}
        oninput={(e) => setSettingString("signup", ["reset_body_template"], e.currentTarget.value)}
      ></textarea>

      <div class="mt-3 flex flex-wrap gap-2">
        <button
          type="button"
          class="act"
          disabled={session.busy}
          onclick={() => setSettingString("signup", ["reset_body_template"], defaultResetBody)}
        >
          Load the standard text
        </button>
        <button
          type="button"
          class="act"
          disabled={session.busy}
          onclick={() => setSettingString("signup", ["reset_body_template"], "")}
        >
          Clear to built-in
        </button>
      </div>
    </div>
  </Section>

  <Section
    title="Preview"
    note="Renders the selected template on the server against sample values. Nothing is sent and nothing is saved."
  >
    <div class="grid gap-10 lg:grid-cols-[20rem_minmax(0,1fr)]">
      <div>
        <span class="stamp block">Mail</span>
        <div class="mt-2 flex flex-wrap gap-2">
          <button
            type="button"
            class="act {previewKind === 'verify' ? 'act-primary' : ''}"
            aria-pressed={previewKind === "verify"}
            onclick={() => (previewKind = "verify")}
          >
            Verification
          </button>
          <button
            type="button"
            class="act {previewKind === 'reset' ? 'act-primary' : ''}"
            aria-pressed={previewKind === "reset"}
            onclick={() => (previewKind = "reset")}
          >
            Password reset
          </button>
        </div>

        <div class="mt-6 grid gap-5">
          <Entry label="Recipient" placeholder="user@example.com" bind:value={previewEmail} />
          <Entry label="Code" placeholder="123456" mono bind:value={previewCode} />
          <Entry
            label="Redirect URI"
            placeholder="https://app.example.com/login/?flow=verify"
            mono
            hint="Fills the magic link field. Leave it empty to see the code-only branch."
            bind:value={previewRedirectURI}
          />
        </div>

        <button
          type="button"
          class="act act-primary mt-6 w-full"
          disabled={previewBusy}
          onclick={renderPreview}
        >
          {previewBusy ? "Rendering…" : "Render"}
        </button>
      </div>

      <div class="min-w-0">
        {#if previewError}
          <div class="border border-seal/45 px-4 py-3.5">
            <p class="stamp text-seal">Template rejected</p>
            <p class="mt-1.5 max-w-[70ch] text-[13px] leading-[1.6] text-ink">{previewError}</p>
            <p class="mt-2 max-w-[70ch] text-[12.5px] leading-[1.55] text-muted">
              Fix the body above and render again — the stored setting is untouched.
            </p>
          </div>
        {:else if preview}
          <!-- The mail as received: a document, not a form field. -->
          <div class="border border-rule bg-sheet">
            <div class="border-b border-rule px-5 py-4">
              <p class="stamp">Subject</p>
              <p class="mt-1.5 break-words text-[15.5px] font-semibold leading-[1.35] text-ink">
                {preview.subject}
              </p>
              {#if preview.magic_link}
                <p class="stamp mt-3">Link</p>
                <p class="serial mt-1 break-all text-[12px] leading-[1.5] text-muted">
                  {preview.magic_link}
                </p>
              {/if}
            </div>
            <p class="serial whitespace-pre-wrap px-5 py-5 text-[12.5px] leading-[1.7] text-ink">
              {preview.body}
            </p>
          </div>
        {:else}
          <div class="border border-dashed border-rule px-6 py-12 text-center">
            <p class="text-[13.5px] font-semibold text-ink">Nothing rendered yet</p>
            <p class="mx-auto mt-2 max-w-[52ch] text-[13px] leading-[1.6] text-muted">
              Render before you commit: these two mails are the only thing a locked-out person ever
              sees from this instance.
            </p>
          </div>
        {/if}
      </div>
    </div>
  </Section>

  <Section title="How the login page picks this up">
    <div class="grid gap-8 lg:grid-cols-2">
      <div class="min-w-0">
        <p class="max-w-[70ch] text-[13px] leading-[1.6] text-ink">
          The login middleware reads these settings live and shows
          <span class="font-semibold">Create account</span> and
          <span class="font-semibold">Forgot password?</span> by itself for password providers backed
          by this auth middleware. In-process providers need no login configuration; a remote instance
          needs the two URLs below.
        </p>
        <pre class="exhibit mt-4 overflow-auto whitespace-pre-wrap">{remoteConfig}</pre>
      </div>

      <div class="min-w-0">
        <span class="stamp block">Public endpoints</span>
        <ul class="mt-2">
          {#each endpoints as endpoint (endpoint)}
            <li class="serial border-b border-rule py-2 text-[12.5px] text-ink last:border-b-0">
              {endpoint}
            </li>
          {/each}
        </ul>
        <p class="mt-4 max-w-[70ch] text-[13px] leading-[1.6] text-muted">
          Public, but every call still has to present valid client credentials. With verification on,
          the responses never reveal whether an address is registered.
        </p>
      </div>
    </div>
  </Section>
</Instrument>
