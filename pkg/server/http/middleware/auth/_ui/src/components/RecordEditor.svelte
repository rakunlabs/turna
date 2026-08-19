<script lang="ts">
  import Instrument from "./ui/Instrument.svelte";
  import Section from "./ui/Section.svelte";
  import Switch from "./ui/Switch.svelte";
  import Seal from "./ui/Seal.svelte";
  import EditorSimpleForm from "./editor/EditorSimpleForm.svelte";
  import { editor } from "../lib/state/editor.svelte";
  import { session } from "../lib/state/session.svelte";

  /**
   * The drafting bench for one instrument. Everything it reads and writes lives
   * on the editor store — the page only says who to tell once the record lands.
   */
  let { oncommitted }: { oncommitted: () => Promise<void> } = $props();

  const recordID = $derived(editor.loadedID || editor.id.trim());

  /**
   * Where this draft will be written. New records on raw kinds POST to the
   * collection because the API owns the identifier; everything else PUTs to its
   * own path.
   */
  const apiPath = $derived(
    editor.spec.body === "raw" && editor.isNew
      ? `${session.apiBase}/${editor.spec.listPath}`
      : `${session.apiBase}/${editor.spec.listPath}/${recordID || `{${editor.spec.idField}}`}`,
  );

  const namespaces = $derived(Object.keys(editor.spec.namespaceExamples ?? {}));
</script>

<Instrument
  title={editor.isNew ? `Issue a ${editor.spec.singular}` : `Amend ${editor.spec.singular}`}
  note={editor.spec.description}
>
  {#snippet actions()}
    <button type="button" class="act act-quiet" disabled={session.busy} onclick={() => editor.close()}>
      Back to register
    </button>
    <button
      type="button"
      class="act act-primary"
      disabled={!editor.canCommit}
      onclick={() => void editor.commit(oncommitted)}
    >
      {session.busy ? "Committing…" : "Commit"}
    </button>
  {/snippet}

  {#snippet custody()}
    <span class="stamp">
      {editor.spec.primaryLabel}
      <span class="serial ml-2 {recordID ? 'text-ink' : 'text-muted'}">{recordID || "unassigned"}</span>
    </span>
    <span class="stamp-raw serial">{apiPath}</span>
    <Seal
      state={editor.isNew ? "void" : "endorsed"}
      label={editor.isNew ? "Draft — not yet written" : "Issued"}
    />
    {#if !editor.isNew && editor.custody}
      <span class="stamp">
        Countersigned <span class="stamp-raw serial ml-1 text-ink">{editor.custody}</span>
      </span>
    {/if}
    {#if editor.spec.body === "config"}
      <Seal state={editor.enabled ? "endorsed" : "void"} label={editor.enabled ? "Enabled" : "Held"} />
    {/if}
  {/snippet}

  <Section
    title="Instrument"
    note={editor.isNew
      ? "Nothing is written until you commit. Until then this draft exists only in this browser."
      : "Committing replaces the stored record with what stands here."}
    first
  >
    <div class="grid gap-6 sm:grid-cols-2">
      {#if editor.spec.body === "raw"}
        <div class="min-w-0">
          <span class="stamp block">{editor.spec.primaryLabel}</span>
          <p class="serial mt-1.5 text-[13.5px] text-ink">
            {editor.loadedID || "Assigned by the API on commit"}
          </p>
          {#if editor.isNew}
            <p class="mt-1.5 max-w-[62ch] text-[12px] leading-[1.5] text-muted">
              This kind of record gets its identifier from the server, so there is nothing to fill in here.
            </p>
          {/if}
        </div>
      {:else if editor.kind === "settings"}
        <div class="min-w-0">
          <span class="stamp block">Namespace</span>
          <p class="serial mt-1.5 text-[13.5px] {editor.id ? 'text-ink' : 'text-muted'}">
            {editor.id || "None chosen"}
          </p>
          <p class="mt-1.5 max-w-[62ch] text-[12px] leading-[1.5] text-muted">
            Settings namespaces are reserved — choose one below rather than naming your own.
          </p>
        </div>
      {:else}
        <div class="min-w-0">
          <label class="stamp block" for="record-id">{editor.spec.primaryLabel}</label>
          <input
            id="record-id"
            class="entry serial mt-1.5"
            placeholder="default"
            autocomplete="off"
            bind:value={editor.id}
            oninput={(event) => editor.applyNamespaceExample(event.currentTarget.value)}
          />
          <p class="mt-1.5 max-w-[62ch] text-[12px] leading-[1.5] text-muted">
            The identifier the record is stored and addressed under. Changing it on an existing record
            writes to the new path and leaves the old one standing.
          </p>
        </div>
      {/if}

      {#if editor.spec.body === "config"}
        <Switch
          label="Enabled"
          bind:checked={editor.enabled}
          hint="A held record stays stored but is ignored at request time."
        />
      {/if}
    </div>

    {#if namespaces.length > 0 && editor.isNew}
      <div class="mt-8">
        <span class="stamp block">Reserved namespaces</span>
        <div class="mt-2.5 flex flex-wrap gap-2">
          {#each namespaces as ns (ns)}
            <button
              type="button"
              class="stamp border border-rule px-3 py-1.5 transition-colors
                {editor.id.trim() === ns ? 'bg-ink text-sheet' : 'text-muted hover:text-ink'}"
              aria-pressed={editor.id.trim() === ns}
              onclick={() => {
                editor.id = ns;
                editor.applyNamespaceExample(ns);
              }}
            >
              {ns}
            </button>
          {/each}
        </div>
        <p class="mt-2.5 max-w-[70ch] text-[12px] leading-[1.5] text-muted">
          Choosing a namespace replaces the draft with that namespace's template.
        </p>
      </div>
    {/if}
  </Section>

  {#if editor.kind === "permissions" && editor.isNew}
    <Section
      title="Starting point"
      note="Ready-made documents scoped to this instance's live auth prefix. Read them before committing — they are a draft, not a policy decision."
    >
      <div class="flex flex-wrap gap-2">
        <button
          type="button"
          class="act"
          disabled={session.busy}
          onclick={() => editor.applyPermissionPreset("auth_admin")}
        >
          auth_admin
        </button>
        <button
          type="button"
          class="act"
          disabled={session.busy}
          onclick={() => editor.applyPermissionPreset("auth_user")}
        >
          auth_user
        </button>
      </div>
      <p class="mt-3 max-w-[70ch] text-[13px] leading-[1.6] text-muted">
        auth_admin grants full management access to this console's API; auth_user grants only the
        self-service surfaces. After committing, name the permission on the admin user and set the same
        name under Admin.
      </p>
    </Section>
  {/if}

  <!-- One document, two views. The raw exhibit stays reachable on every kind. -->
  <div class="mt-12 flex flex-wrap items-center justify-between gap-x-6 gap-y-3">
    <div class="flex items-stretch border border-rule" role="group" aria-label="Document view">
      <button
        type="button"
        class="stamp px-3 py-1.5 transition-colors
          {editor.advanced ? 'text-muted hover:text-ink' : 'bg-ink text-sheet'}"
        aria-pressed={!editor.advanced}
        onclick={() => editor.setAdvanced(false)}
      >
        Form
      </button>
      <button
        type="button"
        class="stamp border-l border-rule px-3 py-1.5 transition-colors
          {editor.advanced ? 'bg-ink text-sheet' : 'text-muted hover:text-ink'}"
        aria-pressed={editor.advanced}
        onclick={() => editor.setAdvanced(true)}
      >
        Raw document
      </button>
    </div>

    <div class="flex flex-wrap gap-2">
      {#if editor.advanced}
        <button type="button" class="act" disabled={session.busy} onclick={() => editor.formatJSON()}>
          Format
        </button>
      {/if}
      {#if editor.isNew}
        <button type="button" class="act" disabled={session.busy} onclick={() => editor.loadTemplate()}>
          Load template
        </button>
      {/if}
    </div>
  </div>

  <!-- Validation stands beside the document it refers to, never in a dialog. -->
  {#if editor.jsonError || editor.requirementError}
    <div class="mt-4 border-t border-seal/50 pt-3" role="status" aria-live="polite">
      {#if editor.requirementError}
        <p class="max-w-[70ch] text-[13px] leading-[1.55] text-seal">{editor.requirementError}</p>
      {/if}
      {#if editor.jsonError}
        <p class="mt-1.5 max-w-[70ch] text-[13px] leading-[1.55] text-seal">
          {editor.jsonError} — the document must parse before it can be committed.
        </p>
      {/if}
    </div>
  {/if}

  {#if editor.advanced}
    <Section
      title="Raw document"
      note="The form and this exhibit are two views of one document: whatever stands here is exactly what gets sent. This is the escape hatch for every field the form does not draw."
    >
      {#snippet aside()}
        <span class="stamp {editor.jsonError ? 'text-seal' : 'text-endorsed'}">
          {editor.jsonError ? "Does not parse" : "Parses"}
        </span>
      {/snippet}

      <textarea
        class="exhibit min-h-[26rem]"
        spellcheck="false"
        aria-label="Raw record document"
        aria-invalid={editor.jsonError ? "true" : undefined}
        bind:value={editor.json}
      ></textarea>

      {#if editor.spec.body === "raw" && editor.isNew}
        <p class="mt-3 max-w-[70ch] text-[13px] leading-[1.6] text-muted">
          New records take a generated id on commit wherever the API owns the id field — anything you
          write in <span class="serial">id</span> here is ignored.
        </p>
      {/if}
    </Section>
  {:else if !editor.simpleFormAvailable}
    <div class="mt-12 border border-dashed border-rule px-6 py-14 text-center">
      <p class="text-[15px] font-semibold text-ink">No form for this namespace</p>
      <p class="mx-auto mt-2 max-w-[56ch] text-[13px] leading-[1.6] text-muted">
        Reserved settings namespaces are drawn as fields. This one is not, so edit it as a raw document
        — the same record, without the guides.
      </p>
      <button type="button" class="act act-primary mt-6" onclick={() => editor.setAdvanced(true)}>
        Open the raw document
      </button>
    </div>
  {:else}
    <EditorSimpleForm />
  {/if}
</Instrument>
