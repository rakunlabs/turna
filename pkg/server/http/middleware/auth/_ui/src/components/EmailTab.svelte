<script lang="ts">
  import Instrument from "./ui/Instrument.svelte";
  import Section from "./ui/Section.svelte";
  import Entry from "./ui/Entry.svelte";
  import Switch from "./ui/Switch.svelte";
  import Seal from "./ui/Seal.svelte";

  import type { AnyRecord } from "../lib/api";
  import { messageOf, session } from "../lib/state/session.svelte";
  import {
    getSettingBool,
    getSettingNumber,
    getSettingString,
    saveSetting,
    setSettingBool,
    setSettingNumber,
    setSettingString,
    settingRecord,
  } from "../lib/state/settings.svelte";

  type EmailPreview = {
    subject: string;
    body: string;
    magic_link?: string;
    data?: AnyRecord;
  };

  const defaultCodeBodyTemplate = `Your one-time login code is:

{{.Code}}

The code expires in {{.ExpiresIn}}.`;

  /** Exactly the fields buildEmailMessage exposes to the code mail template. */
  const codeFields = [
    "{{.Email}}",
    "{{.Code}}",
    "{{.ExpiresIn}}",
    "{{.ClientID}}",
    "{{.RedirectURI}}",
    "{{.UserID}}",
    "{{.UserAlias}}",
  ];

  let previewEmail = $state("user@example.com");
  let previewCode = $state("123456");
  let previewClientID = $state("ui");
  let preview = $state<EmailPreview | null>(null);
  let previewBusy = $state(false);
  let previewError = $state("");

  const host = $derived(getSettingString("email", ["smtp", "host"]).trim());
  const port = $derived(getSettingNumber("email", ["smtp", "port"], 587));
  const from = $derived(getSettingString("email", ["from"]).trim());
  const codeHeld = $derived(getSettingBool("email", ["disabled"]));
  const magicLink = $derived(getSettingBool("email", ["magic_link"], true));
  const noAuth = $derived(getSettingBool("email", ["smtp", "no_auth"]));
  const bodyTemplate = $derived(getSettingString("email", ["body_template"]));

  /**
   * Without a relay host nothing can leave this instance, so the whole email
   * family — codes, magic links, signup verification, password reset — is off.
   * That is the first thing the page has to say, not a hint under a field.
   */
  const standing = $derived.by(() => {
    if (!host) {
      return {
        state: "void" as const,
        label: "Not delivering",
        detail:
          "No SMTP host is set, so this instance cannot send mail. One-time code login, magic links, signup verification and password reset are all off until a relay is entered below.",
      };
    }

    if (codeHeld) {
      return {
        state: "held" as const,
        label: "Code login held",
        detail: `The relay at ${host}:${port} is configured, but the token endpoint refuses grant_type=email_code. Magic link, signup and password reset mail still go out.`,
      };
    }

    return {
      state: "endorsed" as const,
      label: "Delivering",
      detail: `Codes are sent from ${from || "the relay's default sender"} through ${host}:${port}.`,
    };
  });

  // Preview the one-time code mail: no redirect_uri, so no magic link is built.
  async function renderPreview() {
    previewBusy = true;
    previewError = "";

    try {
      const body = await session.request<EmailPreview>("email/preview", {
        method: "POST",
        body: JSON.stringify({
          settings: settingRecord("email"),
          email: previewEmail.trim(),
          code: previewCode.trim(),
          client_id: previewClientID.trim(),
        }),
      });

      preview = body.payload;
    } catch (err) {
      preview = null;
      previewError = messageOf(err, "Cannot render preview");
    } finally {
      previewBusy = false;
    }
  }
</script>

<Instrument
  title="Email code login"
  note="Passwordless one-time codes delivered over your own SMTP relay. This relay is shared: magic link, signup verification and password reset all send through it."
>
  {#snippet actions()}
    <button
      type="button"
      class="act act-primary"
      disabled={session.busy}
      onclick={() => saveSetting("email")}
    >
      {session.busy ? "Committing…" : "Commit"}
    </button>
  {/snippet}

  {#snippet custody()}
    <span class="stamp">Namespace <span class="serial stamp-raw">email</span></span>
    <span class="serial stamp-raw">{session.apiBase}/settings/email</span>
  {/snippet}

  <div class="border border-rule bg-sheet px-4 py-3.5">
    <div class="flex flex-wrap items-baseline gap-x-5 gap-y-2">
      <span class="shrink-0"><Seal state={standing.state} label={standing.label} /></span>
      <p class="min-w-0 flex-1 basis-72 max-w-[70ch] text-[13px] leading-[1.6] text-ink">
        {standing.detail}
      </p>
    </div>
  </div>

  <Section
    title="Relay"
    note="Credentials are stored encrypted and applied without a restart."
  >
    <div class="grid gap-6 sm:grid-cols-2">
      <Entry
        label="From"
        placeholder="auth@example.com"
        hint="Envelope sender. Must be an address the relay will accept."
        bind:value={
          () => getSettingString("email", ["from"]),
          (v) => setSettingString("email", ["from"], v)
        }
      />

      <Entry
        label="Code lifetime"
        placeholder="15m"
        mono
        hint="Go duration. Codes are single use and expire after this."
        bind:value={
          () => getSettingString("email", ["code_lifetime"]),
          (v) => setSettingString("email", ["code_lifetime"], v)
        }
      />

      <Entry
        label="SMTP host"
        placeholder="smtp.example.com"
        mono
        invalid={!host}
        hint={host ? "" : "Empty turns off every email flow on this instance."}
        bind:value={
          () => getSettingString("email", ["smtp", "host"]),
          (v) => setSettingString("email", ["smtp", "host"], v)
        }
      />

      <Entry
        label="SMTP port"
        type="number"
        mono
        hint="587 for STARTTLS, 465 for implicit TLS, 25 for an unauthenticated internal relay."
        bind:value={
          () => String(getSettingNumber("email", ["smtp", "port"], 587)),
          (v) => setSettingNumber("email", ["smtp", "port"], v)
        }
      />

      <Entry
        label="SMTP username"
        placeholder="optional"
        mono
        disabled={noAuth}
        bind:value={
          () => getSettingString("email", ["smtp", "username"]),
          (v) => setSettingString("email", ["smtp", "username"], v)
        }
      />

      <Entry
        label="SMTP password"
        type="password"
        placeholder={noAuth ? "not used while auth is skipped" : "optional"}
        disabled={noAuth}
        bind:value={
          () => getSettingString("email", ["smtp", "password"]),
          (v) => setSettingString("email", ["smtp", "password"], v)
        }
      />
    </div>
  </Section>

  <Section title="Transport">
    <div class="grid gap-4 sm:grid-cols-2">
      <Switch
        label="Skip SMTP authentication"
        hint="For an internal relay that accepts mail from this host without credentials."
        bind:checked={
          () => getSettingBool("email", ["smtp", "no_auth"]),
          (v) => setSettingBool("email", ["smtp", "no_auth"], v)
        }
      />

      <Switch
        label="STARTTLS"
        hint="Upgrade the plaintext connection after greeting. The usual choice on port 587."
        bind:checked={
          () => getSettingBool("email", ["smtp", "starttls"], true),
          (v) => setSettingBool("email", ["smtp", "starttls"], v)
        }
      />

      <Switch
        label="Implicit TLS"
        hint="Open the connection already encrypted. The usual choice on port 465."
        bind:checked={
          () => getSettingBool("email", ["smtp", "tls"]),
          (v) => setSettingBool("email", ["smtp", "tls"], v)
        }
      />

      <Switch
        label="Accept any relay certificate"
        consequential
        hint="Stops verifying the relay's certificate, so a machine on the path can read the codes in transit. Use only against a relay with a self-signed certificate you control."
        bind:checked={
          () => getSettingBool("email", ["smtp", "insecure_skip_verify"]),
          (v) => setSettingBool("email", ["smtp", "insecure_skip_verify"], v)
        }
      />
    </div>

    <div class="mt-7">
      <Switch
        label="Hold code login"
        hint="While held, the token endpoint refuses grant_type=email_code. Mail for magic link, signup and password reset is unaffected."
        bind:checked={
          () => getSettingBool("email", ["disabled"]),
          (v) => setSettingBool("email", ["disabled"], v)
        }
      />
    </div>

    {#if magicLink}
      <p class="mt-5 max-w-[70ch] text-[13px] leading-[1.6] text-muted">
        Magic link login is on and configured on its own page — subject and body for that mail live
        there, in the same <span class="serial">email</span> namespace.
      </p>
    {/if}
  </Section>

  <Section
    title="Code mail"
    note="Leave the body empty to send the built-in text. Anything you write here is a Go text/template rendered per recipient."
  >
    <Entry
      label="Subject"
      placeholder="Your login code"
      bind:value={
        () => getSettingString("email", ["subject"]),
        (v) => setSettingString("email", ["subject"], v)
      }
    />

    <div class="mt-7">
      <div class="flex flex-wrap items-baseline gap-x-4 gap-y-1.5">
        <span class="stamp shrink-0">Body</span>
        <span class="min-w-0 flex-1"></span>
        <span class="stamp shrink-0">Fields</span>
      </div>

      <ul class="mt-1.5 flex flex-wrap items-center gap-x-4 gap-y-1">
        {#each codeFields as field (field)}
          <li class="serial text-[12px] text-muted">{field}</li>
        {/each}
      </ul>

      <textarea
        class="exhibit mt-2.5 min-h-[13rem]"
        spellcheck="false"
        placeholder="empty = built-in code body"
        aria-label="Code mail body template"
        value={bodyTemplate}
        oninput={(e) => setSettingString("email", ["body_template"], e.currentTarget.value)}
      ></textarea>

      <div class="mt-3 flex flex-wrap gap-2">
        <button
          type="button"
          class="act"
          disabled={session.busy}
          onclick={() => setSettingString("email", ["body_template"], defaultCodeBodyTemplate)}
        >
          Load the standard text
        </button>
        <button
          type="button"
          class="act"
          disabled={session.busy}
          onclick={() => setSettingString("email", ["body_template"], "")}
        >
          Clear to built-in
        </button>
      </div>
    </div>
  </Section>

  <Section
    title="Preview"
    note="Renders the template above against sample values on the server. Nothing is sent and nothing is saved."
  >
    <div class="grid gap-10 lg:grid-cols-[20rem_minmax(0,1fr)]">
      <div>
        <div class="grid gap-5">
          <Entry label="Recipient" placeholder="user@example.com" bind:value={previewEmail} />
          <Entry label="Code" placeholder="123456" mono bind:value={previewCode} />
          <Entry label="Client ID" placeholder="ui" mono bind:value={previewClientID} />
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
            </div>
            <p class="serial whitespace-pre-wrap px-5 py-5 text-[12.5px] leading-[1.7] text-ink">
              {preview.body}
            </p>
          </div>
        {:else}
          <div class="border border-dashed border-rule px-6 py-12 text-center">
            <p class="text-[13.5px] font-semibold text-ink">Nothing rendered yet</p>
            <p class="mx-auto mt-2 max-w-[52ch] text-[13px] leading-[1.6] text-muted">
              Render before you commit: a template that fails to parse is only found here or by the
              first person who tries to log in.
            </p>
          </div>
        {/if}
      </div>
    </div>
  </Section>
</Instrument>
