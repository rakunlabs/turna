<script lang="ts">
  import type { AnyRecord, SettingNamespace } from "../lib/api";

  export let apiBase = "/auth/v1";
  export let busy = false;
  export let settingsRevision = 0;
  export let settingRecord: (namespace: SettingNamespace) => AnyRecord = () => ({});
  export let setSettingRecord: (namespace: SettingNamespace, value: AnyRecord) => void = () => {};
  export let saveSetting: (namespace: SettingNamespace) => void | Promise<void> = () => {};

  type ClaimRow = { id: number; key: string; tmpl: string };
  type SetRow = { id: number; name: string; claims: ClaimRow[] };

  const ns: SettingNamespace = "custom_info";

  // ready-to-paste endpoint URLs for a set (the auth prefix is apiBase without /v1).
  $: origin = typeof window !== "undefined" ? window.location.origin : "";
  $: oauthBase = apiBase.replace(/\/v1$/, "");

  function userinfoURL(name: string) {
    return `${origin}${oauthBase}/oauth2/userinfo/${encodeURIComponent(name.trim())}`;
  }

  function discoveryURL(name: string) {
    return `${origin}${oauthBase}/oauth2/openid/${encodeURIComponent(name.trim())}/.well-known/openid-configuration`;
  }

  async function copyText(value: string) {
    try {
      await navigator.clipboard.writeText(value);
    } catch {
      // clipboard may be unavailable (insecure context); ignore.
    }
  }

  let uid = 0;
  let disabled = false;
  let sets: SetRow[] = [];
  // re-sync the local editable model from the canonical record only when it
  // actually changes (initial load + after save); typing keeps edits local.
  let syncedRevision = -1;

  $: if (settingsRevision !== syncedRevision) {
    syncFromRecord();
    syncedRevision = settingsRevision;
  }

  function asRecord(value: unknown): Record<string, unknown> {
    return value && typeof value === "object" && !Array.isArray(value) ? (value as Record<string, unknown>) : {};
  }

  function newClaim(key = "", tmpl = ""): ClaimRow {
    return { id: uid++, key, tmpl };
  }

  function newSet(name = "", claims: ClaimRow[] = [newClaim()]): SetRow {
    return { id: uid++, name, claims };
  }

  function syncFromRecord() {
    const rec = asRecord(settingRecord(ns));
    disabled = Boolean(rec.disabled);

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

    return { disabled, sets: out };
  }

  function addSet() {
    sets = [...sets, newSet()];
  }

  function removeSet(id: number) {
    sets = sets.filter((set) => set.id !== id);
  }

  function addClaim(setID: number) {
    sets = sets.map((set) => (set.id === setID ? { ...set, claims: [...set.claims, newClaim()] } : set));
  }

  function removeClaim(setID: number, claimID: number) {
    sets = sets.map((set) => (set.id === setID ? { ...set, claims: set.claims.filter((claim) => claim.id !== claimID) } : set));
  }

  async function save() {
    setSettingRecord(ns, buildRecord());
    await saveSetting(ns);
  }

  // ---- preview -----------------------------------------------------------
  let previewSetID: number | "" = "";
  let previewClaims = `{
  "sub": "user-123",
  "name": "Jane Doe",
  "preferred_username": "jane",
  "email": "jane@example.com",
  "given_name": "Jane",
  "family_name": "Doe"
}`;
  let previewUserDetails = `{
  "department": "Engineering"
}`;
  let preview: AnyRecord | null = null;
  let previewError = "";
  let previewBusy = false;

  $: if (previewSetID === "" && sets.length) previewSetID = sets[0].id;

  function prettyJSON(value: unknown) {
    return JSON.stringify(value, null, 2);
  }

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
        throw new Error(`SAMPLE CLAIMS: ${err instanceof Error ? err.message : String(err)}`);
      }
      try {
        details = JSON.parse(previewUserDetails || "{}");
      } catch (err) {
        throw new Error(`USER DETAILS: ${err instanceof Error ? err.message : String(err)}`);
      }

      const set = sets.find((item) => item.id === previewSetID);
      const claimsMap: Record<string, string> = {};
      if (set) {
        for (const claim of set.claims) {
          const key = claim.key.trim();
          if (key) claimsMap[key] = claim.tmpl;
        }
      }

      const res = await fetch(`${apiBase}/custom-info/preview`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ claims, user: { details }, set: { claims: claimsMap } }),
      });
      const body = await res.json().catch(() => ({}));
      if (!res.ok) throw new Error(body?.message ?? body?.error ?? `preview failed: ${res.status}`);

      preview = (body.payload ?? {}) as AnyRecord;
    } catch (err) {
      previewError = err instanceof Error ? err.message : String(err);
    } finally {
      previewBusy = false;
    }
  }
</script>

<div class="grid gap-px bg-line p-px">
  <div class="grid gap-3 bg-panel p-4">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div>
        <span class="t-label text-fg">[ CUSTOM INFO ]</span>
        <h3 class="mt-2 font-display text-3xl uppercase leading-none tracking-tight md:text-4xl">Userinfo Claim Templates</h3>
      </div>
      <button class="btn-t-solid" disabled={busy} on:click={save}>[ SAVE CUSTOM INFO ]</button>
    </div>
    <p class="max-w-3xl text-xs leading-5 text-dim">
      Named template sets rewrite the response of <span class="text-fg">GET /auth/oauth2/userinfo/&#123;name&#125;</span>. Each claim template is a Go
      <code class="text-fg">text/template</code> rendered with <code class="text-fg">{"{{ .claims.<name> }}"}</code> (base userinfo claims) and
      <code class="text-fg">{"{{ .user.Details.<key> }}"}</code> (full user record). A new key <span class="text-fg">adds</span> a claim, an existing key
      <span class="text-fg">overwrites</span> it, and a template that renders <span class="text-alert">empty</span> <span class="text-fg">removes</span> it
      (use <code class="text-fg">{"{{- -}}"}</code> to trim whitespace). The plain <span class="text-fg">/userinfo</span> endpoint is never affected.
    </p>
    <label class="flex w-fit items-center gap-3 text-xs font-bold uppercase tracking-[0.15em]">
      <input type="checkbox" bind:checked={disabled} class="h-3.5 w-3.5 appearance-none border border-line bg-crt checked:bg-alert" />
      <span class={disabled ? "text-alert" : "text-fg"}>{disabled ? "CUSTOM INFO DISABLED" : "CUSTOM INFO ENABLED"}</span>
    </label>
  </div>

  <div class="grid gap-px bg-line">
    <div class="flex flex-wrap items-center justify-between gap-3 bg-panel px-3 py-2">
      <span class="t-label text-fg">[ TEMPLATE SETS ]</span>
      <button class="btn-t-solid" disabled={busy} on:click={addSet}>[ + ADD SET ]</button>
    </div>

    {#if sets.length === 0}
      <p class="bg-panel p-4 text-[11px] leading-4 text-dim">No template sets yet. Add a set; its name becomes the <span class="text-fg">&#123;name&#125;</span> path segment, e.g. <span class="text-fg">/auth/oauth2/userinfo/myapp</span>.</p>
    {:else}
      {#each sets as set (set.id)}
        <div class="grid gap-px bg-line">
          <div class="flex flex-wrap items-end justify-between gap-3 bg-panel p-3">
            <label class="grid min-w-0 flex-1 gap-1">
              <span class="t-label">SET NAME (URL SEGMENT)</span>
              <input class="field-t" bind:value={set.name} placeholder="myapp" />
            </label>
            <button class="btn-t border border-line text-alert hover:border-alert" disabled={busy} on:click={() => removeSet(set.id)}>[ REMOVE SET ]</button>
          </div>

          {#if set.name.trim()}
            <div class="grid gap-px bg-line lg:grid-cols-2">
              <div class="grid gap-1 bg-panel p-3">
                <span class="t-label">USERINFO URL</span>
                <div class="flex items-center gap-2">
                  <input class="field-t min-w-0 flex-1 font-mono text-[11px]" readonly value={userinfoURL(set.name)} />
                  <button class="btn-t border border-line text-dim hover:border-fg hover:text-fg" on:click={() => copyText(userinfoURL(set.name))}>[ COPY ]</button>
                </div>
              </div>
              <div class="grid gap-1 bg-panel p-3">
                <span class="t-label">DISCOVERY URL (.well-known)</span>
                <div class="flex items-center gap-2">
                  <input class="field-t min-w-0 flex-1 font-mono text-[11px]" readonly value={discoveryURL(set.name)} />
                  <button class="btn-t border border-line text-dim hover:border-fg hover:text-fg" on:click={() => copyText(discoveryURL(set.name))}>[ COPY ]</button>
                </div>
                <span class="text-[10px] leading-4 text-dim">Point the app's OIDC discovery here; its <code class="text-fg">userinfo_endpoint</code> resolves to the URL above (other endpoints and issuer stay shared).</span>
              </div>
            </div>
          {/if}

          <div class="hidden grid-cols-[minmax(0,1fr),minmax(0,2fr),auto] gap-3 bg-panel px-3 py-2 md:grid">
            <span class="t-label text-fg">CLAIM</span>
            <span class="t-label text-fg">TEMPLATE</span>
            <span class="t-label text-fg">&nbsp;</span>
          </div>

          {#each set.claims as claim (claim.id)}
            <div class="grid gap-2 bg-crt px-3 py-2 md:grid-cols-[minmax(0,1fr),minmax(0,2fr),auto] md:items-center md:gap-3">
              <input class="field-t" bind:value={claim.key} placeholder="full_name" />
              <input class="field-t font-mono text-[12px]" bind:value={claim.tmpl} placeholder={"{{ .claims.given_name }} {{ .claims.family_name }}"} />
              <button class="btn-t border border-line text-alert hover:border-alert" disabled={busy} on:click={() => removeClaim(set.id, claim.id)} aria-label="remove claim">[ x ]</button>
            </div>
          {/each}

          <div class="bg-panel p-3">
            <button class="btn-t border border-line text-dim hover:border-fg hover:text-fg" disabled={busy} on:click={() => addClaim(set.id)}>[ + ADD CLAIM ]</button>
          </div>
        </div>
      {/each}
    {/if}
  </div>

  <div class="grid gap-px bg-line lg:grid-cols-[360px,minmax(0,1fr)]">
    <div class="grid content-start gap-px bg-line">
      <div class="flex items-center justify-between gap-2 bg-panel px-3 py-2">
        <span class="t-label text-fg">[ PREVIEW INPUT ]</span>
      </div>
      <label class="grid gap-1 bg-panel p-3">
        <span class="t-label">SET</span>
        <select class="field-t" bind:value={previewSetID}>
          {#if sets.length === 0}
            <option value="">— no sets —</option>
          {/if}
          {#each sets as set (set.id)}
            <option value={set.id}>{set.name.trim() || "(unnamed)"}</option>
          {/each}
        </select>
      </label>
      <label class="grid gap-1 bg-panel p-3">
        <span class="t-label">SAMPLE BASE CLAIMS (JSON)</span>
        <textarea class="field-t min-h-32 font-mono text-[11px] leading-4" bind:value={previewClaims} spellcheck="false"></textarea>
      </label>
      <label class="grid gap-1 bg-panel p-3">
        <span class="t-label">SAMPLE USER DETAILS (JSON)</span>
        <textarea class="field-t min-h-24 font-mono text-[11px] leading-4" bind:value={previewUserDetails} spellcheck="false"></textarea>
        <span class="text-[10px] leading-4 text-dim">Reachable as <code class="text-fg">{"{{ .user.Details.<key> }}"}</code> in templates.</span>
      </label>
      <div class="bg-panel p-3">
        <button class="btn-t-solid w-full" disabled={previewBusy || sets.length === 0} on:click={renderPreview}>[ RENDER PREVIEW ]</button>
      </div>
    </div>

    <div class="grid content-start gap-px bg-line">
      <div class="bg-panel px-3 py-2">
        <span class="t-label text-fg">[ RESULTING CLAIMS ]</span>
      </div>
      {#if previewError}
        <div class="bg-panel p-3 text-xs text-alert">{previewError}</div>
      {:else if preview}
        <pre class="overflow-auto whitespace-pre-wrap border border-line bg-crt p-3 text-[11px] leading-5 text-fg">{prettyJSON(preview)}</pre>
      {:else}
        <div class="bg-panel p-4 text-[11px] leading-4 text-dim">Render a preview to apply the selected set to the sample claims before saving. Validates the templates too.</div>
      {/if}
    </div>
  </div>
</div>
