<script lang="ts">
  import Instrument from "./ui/Instrument.svelte";
  import Section from "./ui/Section.svelte";
  import Switch from "./ui/Switch.svelte";
  import Seal from "./ui/Seal.svelte";

  import type { AnyRecord, SettingNamespace } from "../lib/api";
  import { pretty } from "../lib/records";
  import { docket, messageOf, session } from "../lib/state/session.svelte";
  import { saveSetting, setSettingRecord, settingRecord } from "../lib/state/settings.svelte";

  type ClaimRow = { id: number; key: string; tmpl: string };
  type SetRow = { id: number; name: string; claims: ClaimRow[] };

  const ns: SettingNamespace = "custom_info";

  /** The two template inputs, stated where the templates are written. */
  const templateFields = ["{{ .claims.<name> }}", "{{ .user.Details.<key> }}"];

  // Braces are template syntax in markup, so the route is named from script.
  const pageNote =
    "Named sets that rewrite the response of GET /auth/oauth2/userinfo/{name}. Each claim is a Go text/template rendered against the base userinfo claims and the full user record.";

  let uid = 0;
  let held = $state(false);
  let sets = $state<SetRow[]>([]);

  /**
   * The editable model is local: typing must not write through to the canonical
   * record, or every keystroke would be a pending change. Re-sync only when the
   * record itself is replaced — initial load, and again after a commit.
   */
  let syncedFrom: AnyRecord | null = null;

  $effect(() => {
    const record = settingRecord(ns);
    if (record === syncedFrom) return;

    syncedFrom = record;
    syncFromRecord(record);
  });

  function asRecord(value: unknown): Record<string, unknown> {
    return value && typeof value === "object" && !Array.isArray(value)
      ? (value as Record<string, unknown>)
      : {};
  }

  function newClaim(key = "", tmpl = ""): ClaimRow {
    return { id: uid++, key, tmpl };
  }

  function newSet(name = "", claims: ClaimRow[] = [newClaim()]): SetRow {
    return { id: uid++, name, claims };
  }

  function syncFromRecord(value: unknown) {
    const rec = asRecord(value);
    held = Boolean(rec.disabled);

    const recSets = asRecord(rec.sets);
    const next: SetRow[] = [];
    for (const [name, raw] of Object.entries(recSets)) {
      const claimsObj = asRecord(asRecord(raw).claims);
      const claims = Object.entries(claimsObj).map(([key, tmpl]) => newClaim(key, String(tmpl)));
      next.push(newSet(name, claims.length ? claims : [newClaim()]));
    }

    sets = next;
  }

  function buildRecord(): AnyRecord {
    const out: Record<string, unknown> = {};
    for (const set of sets) {
      const name = set.name.trim();
      if (!name) continue;

      const claims: Record<string, string> = {};
      for (const claim of set.claims) {
        const key = claim.key.trim();
        if (!key) continue;
        claims[key] = claim.tmpl;
      }
      out[name] = { claims };
    }

    return { disabled: held, sets: out };
  }

  function addSet() {
    sets = [...sets, newSet()];
  }

  function removeSet(id: number) {
    sets = sets.filter((set) => set.id !== id);
  }

  function addClaim(setID: number) {
    sets = sets.map((set) =>
      set.id === setID ? { ...set, claims: [...set.claims, newClaim()] } : set,
    );
  }

  function removeClaim(setID: number, claimID: number) {
    sets = sets.map((set) =>
      set.id === setID
        ? { ...set, claims: set.claims.filter((claim) => claim.id !== claimID) }
        : set,
    );
  }

  async function save() {
    setSettingRecord(ns, buildRecord());
    await saveSetting(ns);
  }

  const named = $derived(sets.filter((set) => set.name.trim()).length);
  const dropped = $derived(sets.length - named);

  const standing = $derived.by(() => {
    if (held) {
      return {
        state: "held" as const,
        label: "Held",
        detail:
          "Every named userinfo route answers with the base claims, exactly as the plain endpoint does. The sets below are kept but not applied.",
      };
    }

    if (named === 0) {
      return {
        state: "void" as const,
        label: "No sets",
        detail:
          "Nothing is tailored. A request to a named userinfo route has no set to apply until one is added and committed.",
      };
    }

    return {
      state: "endorsed" as const,
      label: "Applied",
      detail: `${named} ${named === 1 ? "set rewrites" : "sets rewrite"} the response of their own named userinfo route. The plain /userinfo endpoint is never affected.`,
    };
  });

  function userinfoURL(name: string) {
    return `${window.location.origin}${session.oauthBase}/oauth2/userinfo/${encodeURIComponent(name.trim())}`;
  }

  function discoveryURL(name: string) {
    return `${window.location.origin}${session.oauthBase}/oauth2/openid/${encodeURIComponent(name.trim())}/.well-known/openid-configuration`;
  }

  async function copyText(value: string, what: string) {
    try {
      await navigator.clipboard.writeText(value);
      docket.commit(`${what} copied`);
    } catch {
      // Clipboard access needs a secure context; say what to do instead.
      docket.reject(
        `Cannot reach the clipboard — this page is not in a secure context. Select the ${what.toLowerCase()} and copy it by hand.`,
      );
    }
  }

  /* ---- preview ---------------------------------------------------------- */

  let previewSetID = $state<number | "">("");
  let previewClaims = $state(`{
  "sub": "user-123",
  "name": "Jane Doe",
  "preferred_username": "jane",
  "email": "jane@example.com",
  "given_name": "Jane",
  "family_name": "Doe"
}`);
  let previewUserDetails = $state(`{
  "department": "Engineering"
}`);
  let preview = $state<AnyRecord | null>(null);
  let previewError = $state("");
  let previewBusy = $state(false);

  $effect(() => {
    if (previewSetID === "" && sets.length) previewSetID = sets[0].id;
  });

  const previewSet = $derived(sets.find((item) => item.id === previewSetID));

  async function renderPreview() {
    previewBusy = true;
    previewError = "";
    preview = null;

    try {
      let claims: unknown;
      let details: unknown;
      try {
        claims = JSON.parse(previewClaims || "{}");
      } catch (err) {
        throw new Error(`Sample claims: ${messageOf(err, String(err))}`);
      }
      try {
        details = JSON.parse(previewUserDetails || "{}");
      } catch (err) {
        throw new Error(`User details: ${messageOf(err, String(err))}`);
      }

      const claimsMap: Record<string, string> = {};
      if (previewSet) {
        for (const claim of previewSet.claims) {
          const key = claim.key.trim();
          if (key) claimsMap[key] = claim.tmpl;
        }
      }

      const body = await session.request<AnyRecord>("custom-info/preview", {
        method: "POST",
        body: JSON.stringify({ claims, user: { details }, set: { claims: claimsMap } }),
      });

      preview = body.payload ?? {};
    } catch (err) {
      previewError = messageOf(err, "Cannot render preview");
    } finally {
      previewBusy = false;
    }
  }
</script>

<Instrument title="Userinfo claim templates" note={pageNote} wide>
  {#snippet actions()}
    <button type="button" class="act act-primary" disabled={session.busy} onclick={save}>
      {session.busy ? "Committing…" : "Commit"}
    </button>
  {/snippet}

  {#snippet custody()}
    <span class="stamp">Namespace <span class="serial stamp-raw">custom_info</span></span>
    <span class="stamp">
      {sets.length}
      {sets.length === 1 ? "set" : "sets"}{dropped > 0 ? ` · ${dropped} unnamed` : ""}
    </span>
    <span class="serial stamp-raw">{session.oauthBase}/oauth2/userinfo/…</span>
  {/snippet}

  <div class="max-w-[104ch] border border-rule bg-sheet px-4 py-3.5">
    <div class="flex flex-wrap items-baseline gap-x-5 gap-y-2">
      <span class="shrink-0"><Seal state={standing.state} label={standing.label} /></span>
      <p class="min-w-0 flex-1 basis-72 max-w-[70ch] text-[13px] leading-[1.6] text-ink">
        {standing.detail}
      </p>
    </div>
  </div>

  <div class="max-w-[104ch]">
    <Section title="What a set does to a claim">
      <!-- The whole model is three rules, and none of them are guessable. -->
      <dl class="grid gap-y-4 sm:grid-cols-[7rem_minmax(0,1fr)] sm:gap-x-6">
        <dt class="stamp stamp-ink sm:pt-[3px]">Adds</dt>
        <dd class="max-w-[70ch] text-[13px] leading-[1.6] text-ink">
          A key that is not already in the userinfo response is added, carrying whatever the
          template rendered.
        </dd>

        <dt class="stamp stamp-ink sm:pt-[3px]">Overwrites</dt>
        <dd class="max-w-[70ch] text-[13px] leading-[1.6] text-ink">
          A key that already exists is replaced by the rendered text. The original value is still
          readable inside the template as
          <span class="serial">{"{{ .claims.<name> }}"}</span>.
        </dd>

        <dt class="stamp stamp-ink sm:pt-[3px]">Removes</dt>
        <dd class="max-w-[70ch] text-[13px] leading-[1.6] text-ink">
          A template that renders to nothing <span class="font-semibold">deletes</span> the claim
          from the response. That is how a claim is suppressed — there is no separate remove
          control. Trim surrounding whitespace with
          <span class="serial">{"{{- -}}"}</span>, or the claim survives as a blank string.
        </dd>
      </dl>

      <div class="mt-7">
        <Switch
          label="Hold every set"
          hint="Named userinfo routes keep answering, but with the base claims untouched. Use this to prove a claim problem is or is not coming from these templates."
          bind:checked={held}
        />
      </div>
    </Section>

    <Section
      title="Template sets"
      note="A set's name is the URL segment. Edits here are local until you commit — leave the page to discard them."
    >
      {#snippet aside()}
        <button type="button" class="act act-quiet" onclick={addSet}>Add set</button>
      {/snippet}

      {#if sets.length === 0}
        <div class="border border-dashed border-rule px-6 py-14 text-center">
          <p class="text-[15px] font-semibold text-ink">No template sets</p>
          <p class="mx-auto mt-2 max-w-[56ch] text-[13px] leading-[1.6] text-muted">
            Every named userinfo route needs a set. The name you give it becomes the path segment —
            a set called <span class="serial">myapp</span> is served at
            <span class="serial">/auth/oauth2/userinfo/myapp</span>.
          </p>
          <button type="button" class="act act-primary mt-6" onclick={addSet}>Add set</button>
        </div>
      {:else}
        <div class="grid gap-8">
          {#each sets as set (set.id)}
            <div class="border border-rule bg-sheet">
              <div class="flex flex-wrap items-end gap-x-6 gap-y-3 border-b border-rule px-4 py-3">
                <div class="min-w-0 flex-1 basis-64">
                  <label class="stamp block" for="set-name-{set.id}">Set name · URL segment</label>
                  <input
                    id="set-name-{set.id}"
                    class="entry serial mt-1.5"
                    placeholder="myapp"
                    autocomplete="off"
                    spellcheck="false"
                    bind:value={set.name}
                  />
                </div>

                <button
                  type="button"
                  class="act act-quiet shrink-0 text-seal hover:bg-seal/10 hover:text-seal"
                  onclick={() => removeSet(set.id)}
                >
                  Remove set
                </button>
              </div>

              {#if set.name.trim()}
                <div class="grid gap-x-8 gap-y-5 border-b border-rule px-4 py-4 lg:grid-cols-2">
                  <div class="min-w-0">
                    <span class="stamp block">Userinfo URL</span>
                    <div class="mt-1.5 flex items-end gap-3">
                      <input
                        class="entry serial min-w-0 flex-1 text-[12.5px]"
                        readonly
                        aria-label="Userinfo URL for {set.name}"
                        value={userinfoURL(set.name)}
                      />
                      <button
                        type="button"
                        class="act shrink-0"
                        onclick={() => copyText(userinfoURL(set.name), "Userinfo URL")}
                      >
                        Copy
                      </button>
                    </div>
                  </div>

                  <div class="min-w-0">
                    <span class="stamp block">Discovery URL</span>
                    <div class="mt-1.5 flex items-end gap-3">
                      <input
                        class="entry serial min-w-0 flex-1 text-[12.5px]"
                        readonly
                        aria-label="Discovery URL for {set.name}"
                        value={discoveryURL(set.name)}
                      />
                      <button
                        type="button"
                        class="act shrink-0"
                        onclick={() => copyText(discoveryURL(set.name), "Discovery URL")}
                      >
                        Copy
                      </button>
                    </div>
                    <p class="mt-1.5 max-w-[62ch] text-[12px] leading-[1.5] text-muted">
                      Point the app's OIDC discovery here: its
                      <span class="serial">userinfo_endpoint</span> resolves to the URL beside it,
                      while the issuer and every other endpoint stay shared.
                    </p>
                  </div>
                </div>
              {/if}

              {#if set.claims.length === 0}
                <div class="px-4 py-10 text-center">
                  <p class="text-[13.5px] font-semibold text-ink">No claims in this set</p>
                  <p class="mx-auto mt-2 max-w-[52ch] text-[13px] leading-[1.6] text-muted">
                    As it stands this set returns the base userinfo response unchanged.
                  </p>
                  <button type="button" class="act mt-5" onclick={() => addClaim(set.id)}>
                    Add claim
                  </button>
                </div>
              {:else}
                <div
                  class="hidden items-baseline gap-4 border-b border-rule px-4 py-2 md:grid md:grid-cols-[minmax(0,1fr)_minmax(0,2fr)_5.5rem]"
                >
                  <span class="stamp">Claim</span>
                  <span class="stamp">Template</span>
                  <span class="stamp text-right">Row</span>
                </div>

                <ul>
                  {#each set.claims as claim (claim.id)}
                    <li
                      class="grid gap-x-4 gap-y-2 border-b border-rule px-4 py-3 last:border-b-0 md:grid-cols-[minmax(0,1fr)_minmax(0,2fr)_5.5rem] md:items-end"
                    >
                      <div class="min-w-0">
                        <label class="stamp block md:hidden" for="claim-key-{claim.id}">Claim</label>
                        <input
                          id="claim-key-{claim.id}"
                          class="entry serial mt-1.5 md:mt-0"
                          placeholder="full_name"
                          autocomplete="off"
                          spellcheck="false"
                          bind:value={claim.key}
                        />
                      </div>

                      <div class="min-w-0">
                        <label class="stamp block md:hidden" for="claim-tmpl-{claim.id}">
                          Template
                        </label>
                        <input
                          id="claim-tmpl-{claim.id}"
                          class="entry serial mt-1.5 md:mt-0"
                          placeholder={"{{ .claims.given_name }} {{ .claims.family_name }}"}
                          autocomplete="off"
                          spellcheck="false"
                          bind:value={claim.tmpl}
                        />
                      </div>

                      <div class="md:text-right">
                        <button
                          type="button"
                          class="act act-quiet text-seal hover:bg-seal/10 hover:text-seal"
                          onclick={() => removeClaim(set.id, claim.id)}
                        >
                          Remove
                        </button>
                      </div>
                    </li>
                  {/each}
                </ul>

                <div class="flex flex-wrap items-center gap-x-6 gap-y-2 px-4 py-3">
                  <button type="button" class="act" onclick={() => addClaim(set.id)}>
                    Add claim
                  </button>
                  <ul class="flex flex-wrap items-center gap-x-4 gap-y-1">
                    <li class="stamp">Fields</li>
                    {#each templateFields as field (field)}
                      <li class="serial text-[12px] text-muted">{field}</li>
                    {/each}
                  </ul>
                </div>
              {/if}
            </div>
          {/each}
        </div>
      {/if}
    </Section>

    <Section
      title="Preview"
      note="Applies the selected set to sample input on the server — the same rendering the userinfo route performs. Nothing is saved."
    >
      <div class="grid gap-10 lg:grid-cols-[24rem_minmax(0,1fr)]">
        <div class="min-w-0">
          <div>
            <label class="stamp block" for="preview-set">Set</label>
            <select id="preview-set" class="entry serial mt-1.5" bind:value={previewSetID}>
              {#if sets.length === 0}
                <option value="">No sets to apply</option>
              {/if}
              {#each sets as set (set.id)}
                <option value={set.id}>{set.name.trim() || "(unnamed)"}</option>
              {/each}
            </select>
          </div>

          <div class="mt-6">
            <label class="stamp block" for="preview-claims">Sample base claims · JSON</label>
            <textarea
              id="preview-claims"
              class="exhibit mt-1.5 min-h-[10rem]"
              spellcheck="false"
              bind:value={previewClaims}
            ></textarea>
          </div>

          <div class="mt-6">
            <label class="stamp block" for="preview-details">Sample user details · JSON</label>
            <textarea
              id="preview-details"
              class="exhibit mt-1.5 min-h-[7rem]"
              spellcheck="false"
              bind:value={previewUserDetails}
            ></textarea>
            <p class="mt-1.5 max-w-[62ch] text-[12px] leading-[1.5] text-muted">
              Reachable as <span class="serial">{"{{ .user.Details.<key> }}"}</span> in every
              template of the set.
            </p>
          </div>

          <button
            type="button"
            class="act act-primary mt-6 w-full"
            disabled={previewBusy || sets.length === 0}
            onclick={renderPreview}
          >
            {previewBusy ? "Rendering…" : "Render"}
          </button>
        </div>

        <div class="min-w-0">
          {#if previewError}
            <div class="border border-seal/45 px-4 py-3.5">
              <p class="stamp text-seal">Rejected</p>
              <p class="mt-1.5 max-w-[70ch] text-[13px] leading-[1.6] text-ink">{previewError}</p>
              <p class="mt-2 max-w-[70ch] text-[12.5px] leading-[1.55] text-muted">
                Fix the template or the sample JSON and render again — nothing was written.
              </p>
            </div>
          {:else if preview}
            <!-- The response as returned: a document, not a form field. -->
            <div class="border border-rule bg-sheet">
              <div class="border-b border-rule px-5 py-4">
                <p class="stamp">Response</p>
                <p class="mt-1.5 break-words text-[15.5px] font-semibold leading-[1.35] text-ink">
                  {previewSet?.name.trim() || "(unnamed set)"}
                </p>
                <p class="serial mt-1 break-all text-[12px] leading-[1.5] text-muted">
                  GET /oauth2/userinfo/{previewSet?.name.trim() || "…"}
                </p>
              </div>
              <p class="serial whitespace-pre-wrap px-5 py-5 text-[12.5px] leading-[1.7] text-ink">
                {pretty(preview)}
              </p>
            </div>
          {:else}
            <div class="border border-dashed border-rule px-6 py-12 text-center">
              <p class="text-[13.5px] font-semibold text-ink">Nothing rendered yet</p>
              <p class="mx-auto mt-2 max-w-[52ch] text-[13px] leading-[1.6] text-muted">
                Render before you commit: this is where a claim that silently disappears shows
                itself, rather than in the app that expected it.
              </p>
            </div>
          {/if}
        </div>
      </div>
    </Section>
  </div>
</Instrument>
