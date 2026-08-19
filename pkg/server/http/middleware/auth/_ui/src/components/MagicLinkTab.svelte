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
    getSettingString,
    saveSetting,
    setSettingBool,
    setSettingString,
    settingRecord,
  } from "../lib/state/settings.svelte";

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

  /** The code mail fields plus the link itself, which only this template gets. */
  const magicFields = [
    "{{.Email}}",
    "{{.Code}}",
    "{{.MagicLink}}",
    "{{.ExpiresIn}}",
    "{{.ClientID}}",
    "{{.RedirectURI}}",
    "{{.UserID}}",
    "{{.UserAlias}}",
  ];

  let previewEmail = $state("user@example.com");
  let previewCode = $state("123456");
  let previewClientID = $state("ui");
  let previewRedirectURI = $state("https://app.example.com/login/");
  let preview = $state<EmailPreview | null>(null);
  let previewBusy = $state(false);
  let previewError = $state("");

  const magicLink = $derived(getSettingBool("email", ["magic_link"], true));
  const codeHeld = $derived(getSettingBool("email", ["disabled"]));
  const host = $derived(getSettingString("email", ["smtp", "host"]).trim());
  const magicBodyTemplate = $derived(getSettingString("email", ["magic_link_body_template"]));

  /**
   * A magic link needs two things to exist: a relay to carry it, and the toggle
   * that lets email login build one. Either missing means no link mail ships.
   */
  const standing = $derived.by(() => {
    if (!host) {
      return {
        state: "void" as const,
        label: "Not delivering",
        detail:
          "No SMTP host is set on the Email page, so no link can be sent. The relay is shared by every email flow — set it there first.",
      };
    }

    if (!magicLink) {
      return {
        state: "held" as const,
        label: "Link held",
        detail:
          "Email login sends only the one-time code, even when the request carries an allowed redirect_uri. Nothing else changes.",
      };
    }

    return {
      state: "endorsed" as const,
      label: "Sending links",
      detail: `Email login through ${host} sends a link whenever the request carries a redirect_uri the OAuth client whitelists.`,
    };
  });

  // Preview the magic-link mail: force magic_link on and send a redirect_uri,
  // otherwise the server has nothing to build a link from.
  async function renderPreview() {
    previewBusy = true;
    previewError = "";

    const settings = { ...settingRecord("email"), magic_link: true };

    try {
      const body = await session.request<EmailPreview>("email/preview", {
        method: "POST",
        body: JSON.stringify({
          settings,
          email: previewEmail.trim(),
          code: previewCode.trim(),
          client_id: previewClientID.trim(),
          redirect_uri: previewRedirectURI.trim(),
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
  title="Magic link"
  note="During email login, a request carrying a redirect_uri the OAuth client whitelists receives a link — redirect_uri?code=… — instead of a bare code. It shares the relay and code lifetime configured on the Email page."
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
    <span class="stamp">Shared with the Email page</span>
  {/snippet}

  <div class="border border-rule bg-sheet px-4 py-3.5">
    <div class="flex flex-wrap items-baseline gap-x-5 gap-y-2">
      <span class="shrink-0"><Seal state={standing.state} label={standing.label} /></span>
      <p class="min-w-0 flex-1 basis-72 max-w-[70ch] text-[13px] leading-[1.6] text-ink">
        {standing.detail}
      </p>
    </div>
  </div>

  <Section title="Delivery">
    <Switch
      label="Build a magic link when a redirect_uri is supplied"
      hint="Off sends only the one-time code. Useful when this relay is shared with signup and you do not want login links in mail."
      bind:checked={
        () => getSettingBool("email", ["magic_link"], true),
        (v) => setSettingBool("email", ["magic_link"], v)
      }
    />

    {#if codeHeld && magicLink}
      <p class="mt-5 max-w-[70ch] text-[13px] leading-[1.6] text-muted">
        Code login is held on the Email page. Link mail still goes out for requests that supply a
        redirect_uri — the link carries the same code the token endpoint would have refused on its
        own.
      </p>
    {/if}
  </Section>

  <Section
    title="Link mail"
    note="Leave the body empty to send the built-in text. Anything you write here is a Go text/template rendered per recipient."
  >
    <Entry
      label="Subject"
      placeholder="Your login link"
      disabled={!magicLink}
      bind:value={
        () => getSettingString("email", ["magic_link_subject"]),
        (v) => setSettingString("email", ["magic_link_subject"], v)
      }
    />

    <div class="mt-7">
      <div class="flex flex-wrap items-baseline gap-x-4 gap-y-1.5">
        <span class="stamp shrink-0">Body</span>
        <span class="min-w-0 flex-1"></span>
        <span class="stamp shrink-0">Fields</span>
      </div>

      <ul class="mt-1.5 flex flex-wrap items-center gap-x-4 gap-y-1">
        {#each magicFields as field (field)}
          <li class="serial text-[12px] text-muted">{field}</li>
        {/each}
      </ul>

      <textarea
        class="exhibit mt-2.5 min-h-[13rem]"
        spellcheck="false"
        placeholder="empty = built-in magic-link body"
        aria-label="Magic link mail body template"
        disabled={!magicLink}
        value={magicBodyTemplate}
        oninput={(e) =>
          setSettingString("email", ["magic_link_body_template"], e.currentTarget.value)}
      ></textarea>

      <div class="mt-3 flex flex-wrap gap-2">
        <button
          type="button"
          class="act"
          disabled={session.busy || !magicLink}
          onclick={() =>
            setSettingString("email", ["magic_link_body_template"], defaultMagicBodyTemplate)}
        >
          Load the standard text
        </button>
        <button
          type="button"
          class="act"
          disabled={session.busy || !magicLink}
          onclick={() => setSettingString("email", ["magic_link_body_template"], "")}
        >
          Clear to built-in
        </button>
      </div>
    </div>
  </Section>

  <Section
    title="Preview"
    note="Rendered on the server with magic_link forced on, so the link appears even while delivery is held. Nothing is sent and nothing is saved."
  >
    <div class="grid gap-10 lg:grid-cols-[20rem_minmax(0,1fr)]">
      <div>
        <div class="grid gap-5">
          <Entry label="Recipient" placeholder="user@example.com" bind:value={previewEmail} />
          <Entry label="Code" placeholder="123456" mono bind:value={previewCode} />
          <Entry label="Client ID" placeholder="ui" mono bind:value={previewClientID} />
          <Entry
            label="Redirect URI"
            placeholder="https://app.example.com/login/"
            mono
            hint="The link is this URI with the code appended as a query parameter."
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
              Render before you commit: this is the only place the link is shown next to the words
              around it.
            </p>
          </div>
        {/if}
      </div>
    </div>
  </Section>
</Instrument>
