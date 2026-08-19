<script lang="ts">
  import Section from "./ui/Section.svelte";
  import Entry from "./ui/Entry.svelte";
  import Seal from "./ui/Seal.svelte";
  import { docket, messageOf, session } from "../lib/state/session.svelte";
  import { getSettingBool, getSettingList } from "../lib/state/settings.svelte";
  import { pretty } from "../lib/records";

  /**
   * The one page in this console that answers a question rather than storing a
   * value. Everything here exists to make the answer unmistakable: the terms go
   * in at reading size, the verdict comes back at heading size, and the raw
   * response stays available underneath for anyone correlating with a log.
   *
   * The terms are snapshotted onto the verdict when the check runs. Editing the
   * form afterwards must never quietly re-caption an answer that was decided on
   * different input.
   */
  type Verdict = {
    allowed: boolean;
    alias: string;
    host: string;
    path: string;
    method: string;
    noHostCheck: boolean;
    defaultHosts: string;
    raw: string;
    seq: number;
  };

  let alias = $state("");
  let host = $state("");
  let path = $state("/");
  let method = $state("GET");
  let running = $state(false);
  let missingAlias = $state(false);
  let verdict = $state<Verdict | null>(null);
  let seq = 0;

  const methodOptions = ["GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"];

  const noHostCheck = $derived(getSettingBool("check", ["no_host_check"]));
  const defaultHosts = $derived(getSettingList("check", ["default_hosts"]));

  /**
   * The evidence. `/v1/check` answers with the decision alone, so the honest
   * list is the terms the decision was taken on plus the matching rules that
   * governed each one — not an invented reconstruction of the permission graph.
   */
  const findings = $derived.by(() => {
    const v = verdict;
    if (!v) return [];

    return [
      {
        label: "Subject",
        value: v.alias,
        note: "Resolved by alias across users and service accounts. An unknown or disabled identity is refused before any permission is read.",
      },
      {
        label: "Host",
        value: v.host || "not supplied",
        note: v.noHostCheck
          ? "Host checking is off, so every permission matches on method and path alone."
          : v.defaultHosts
            ? `Host checking is on. A permission that names no host falls back to the default hosts: ${v.defaultHosts}.`
            : "Host checking is on and no default hosts are set, so a permission that names no host cannot match anything.",
      },
      {
        label: "Path",
        value: v.path || "not supplied",
        note: "Matched as a glob against each resource path. A resource listed under excluded vetoes the match even when the path fits.",
      },
      {
        label: "Method",
        value: v.method || "not supplied",
        note: "Must appear in the resource method list, or the list must contain the wildcard. A resource with no methods matches nothing.",
      },
    ];
  });

  async function run() {
    if (!alias.trim()) {
      missingAlias = true;
      docket.reject("Alias is required — enter the alias or id of the identity to test.");
      return;
    }

    missingAlias = false;
    running = true;
    docket.clearRejections();

    const terms = { alias: alias.trim(), path, method: method.trim(), host };

    try {
      const body = await session.raw<{ allowed: boolean }>("check", {
        method: "POST",
        body: JSON.stringify(terms),
      });

      verdict = {
        allowed: Boolean(body.allowed),
        alias: terms.alias,
        host: terms.host,
        path: terms.path,
        method: terms.method,
        noHostCheck,
        defaultHosts,
        raw: pretty(body),
        seq: ++seq,
      };
    } catch (err) {
      verdict = null;
      docket.reject(messageOf(err));
    } finally {
      running = false;
    }
  }
</script>

<Section title="Terms of the check" note="Nothing is written and no token is issued — this asks the same permission graph the middleware asks on every request." first>
  <div class="grid gap-5 sm:grid-cols-2">
    <Entry
      label="Subject alias"
      bind:value={alias}
      placeholder="user@example.com"
      mono
      invalid={missingAlias}
      hint={missingAlias
        ? "Required — the alias or id of the identity to test."
        : "Any alias the identity answers to, or its id."}
    />

    <Entry
      label="Host"
      bind:value={host}
      placeholder="api.example.com"
      mono
      hint="Left empty, only permissions that match an empty host can pass."
    />

    <Entry label="Path" bind:value={path} placeholder="/path/to/check" mono hint="The request path, as the service would see it." />

    <div class="min-w-0">
      <label class="stamp block" for="live-check-method">Method</label>
      <input
        id="live-check-method"
        class="entry serial mt-1.5"
        list="live-check-methods"
        autocomplete="off"
        aria-describedby="live-check-method-hint"
        placeholder="GET or custom method"
        bind:value={method}
      />
      <datalist id="live-check-methods">
        {#each methodOptions as option (option)}
          <option value={option}></option>
        {/each}
      </datalist>
      <p id="live-check-method-hint" class="mt-1.5 text-[12px] leading-[1.5] text-muted">
        Compared case-insensitively; a custom verb is accepted.
      </p>
    </div>
  </div>

  <div class="mt-7 flex flex-wrap items-center gap-3">
    <button type="button" class="act act-primary" disabled={running} onclick={run}>
      {running ? "Checking…" : "Run check"}
    </button>

    {#if verdict}
      <button type="button" class="act act-quiet" disabled={running} onclick={() => (verdict = null)}>
        Clear verdict
      </button>
    {/if}

    {#if running}
      <span class="stamp" role="status" aria-live="polite">Asking the permission graph…</span>
    {/if}
  </div>
</Section>

<!-- The verdict owns the page: it is why anyone opened it. -->
<div class="mt-10" role="status" aria-live="polite">
  {#if !verdict}
    <div class="border border-dashed border-rule px-6 py-14 text-center">
      <p class="text-[15px] font-semibold text-ink">No verdict yet</p>
      <p class="mx-auto mt-2 max-w-[58ch] text-[13px] leading-[1.6] text-muted">
        Name an identity above, give it a request to attempt, and run the check. The answer and the
        terms it was decided on appear here.
      </p>
    </div>
  {:else}
    {#key verdict.seq}
      <div class="guilloche stamp-in border border-rule bg-sheet">
        <div class="px-5 pb-7 pt-6 sm:px-7">
          <div class="flex items-center gap-4">
            <Seal state={verdict.allowed ? "endorsed" : "broken"} size={30} />
            <p
              class="text-[2.1rem] font-bold leading-none tracking-[-0.025em] sm:text-[2.6rem]
                {verdict.allowed ? 'text-endorsed' : 'text-seal'}"
            >
              {verdict.allowed ? "Allowed" : "Denied"}
            </p>
          </div>

          <p class="mt-4 max-w-[70ch] text-[13.5px] leading-[1.6] text-ink">
            {#if verdict.allowed}
              <span class="serial">{verdict.alias}</span> holds a permission that covers
              <span class="serial">{verdict.method || "—"}</span>
              <span class="serial">{verdict.path || "—"}</span>{verdict.host
                ? " on "
                : ""}{#if verdict.host}<span class="serial">{verdict.host}</span>{/if}. A request made
              on these terms passes the middleware.
            {:else}
              Nothing held by <span class="serial">{verdict.alias}</span> covers
              <span class="serial">{verdict.method || "—"}</span>
              <span class="serial">{verdict.path || "—"}</span>{verdict.host
                ? " on "
                : ""}{#if verdict.host}<span class="serial">{verdict.host}</span>{/if}. A request made
              on these terms is refused.
            {/if}
          </p>
        </div>

        <div class="border-t border-rule px-5 py-6 sm:px-7">
          <div class="flex items-baseline gap-4 border-b border-rule pb-1.5">
            <h3 class="stamp stamp-ink shrink-0">Findings</h3>
            <span class="min-w-0 flex-1"></span>
            <span class="stamp shrink-0">Decided on</span>
          </div>

          <ul>
            {#each findings as finding (finding.label)}
              <li
                class="grid gap-x-6 gap-y-1 border-b border-rule py-3 last:border-b-0 md:grid-cols-[7rem_minmax(0,18rem)_minmax(0,1fr)] md:items-baseline"
              >
                <span class="stamp">{finding.label}</span>
                <span class="serial min-w-0 break-all text-[13px] font-medium text-ink">
                  {finding.value}
                </span>
                <span class="min-w-0 text-[12.5px] leading-[1.55] text-muted">{finding.note}</span>
              </li>
            {/each}
          </ul>

          <p class="mt-4 max-w-[70ch] text-[12.5px] leading-[1.55] text-muted">
            The check endpoint answers with the decision alone, so these are the terms it was decided
            on rather than the individual permission that matched. To see which records could carry
            it, open the subject on Users or Service accounts.
          </p>
        </div>

        <div class="border-t border-rule px-5 py-6 sm:px-7">
          <h3 class="stamp stamp-ink border-b border-rule pb-1.5">Raw response</h3>
          <pre class="exhibit mt-3 overflow-x-auto">{verdict.raw}</pre>
        </div>
      </div>
    {/key}
  {/if}
</div>
