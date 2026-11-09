<script lang="ts">
  import { untrack } from "svelte";
  import Instrument from "./ui/Instrument.svelte";
  import Section from "./ui/Section.svelte";
  import Switch from "./ui/Switch.svelte";
  import Seal from "./ui/Seal.svelte";
  import { session } from "../lib/state/session.svelte";
  import {
    settings,
    getSettingBool,
    setSettingBool,
    getSettingString,
    setSettingString,
    getSettingList,
    setSettingList,
    saveSetting,
  } from "../lib/state/settings.svelte";

  type AuditEvent = {
    id: number;
    created_at: string;
    event: string;
    outcome: string;
    actor?: string;
    subject?: string;
    client_id?: string;
    ip?: string;
    user_agent?: string;
    detail?: Record<string, unknown>;
  };

  const events = [
    "login",
    "mfa",
    "token.issue",
    "token.refresh",
    "token.revoke",
    "logout",
    "code.reuse",
    "refresh.reuse",
    "client.auth",
    "consent.denied",
    "admin.change",
    "key.rotate",
  ];

  let filterEvent = $state("");
  let filterOutcome = $state("");
  let filterSubject = $state("");
  let rows = $state<AuditEvent[]>([]);
  let loading = $state(false);
  let more = $state(false);

  const disabled = $derived(getSettingBool("audit", ["disabled"]));

  async function load(append = false) {
    loading = true;
    const params = new URLSearchParams({ limit: "100" });
    if (filterEvent) params.set("event", filterEvent);
    if (filterOutcome) params.set("outcome", filterOutcome);
    if (filterSubject.trim()) params.set("subject", filterSubject.trim());
    if (append && rows.length) params.set("before", String(rows[rows.length - 1].id));

    await session.run(async () => {
      const res = await session.request<AuditEvent[]>(`audit?${params}`);
      const page = res.payload ?? [];
      rows = append ? [...rows, ...page] : page;
      more = page.length === 100;
    }, "Cannot load audit events");
    loading = false;
  }

  function summary(ev: AuditEvent) {
    const d = ev.detail ?? {};
    const parts = [d.grant_type, d.method && `${d.method} ${d.path}`, d.error, d.provider, d.key].filter(Boolean);

    return parts.join(" · ");
  }

  $effect(() => {
    // initial load only; the filters apply on submit
    untrack(() => {
      void settings.load("audit");
      void load();
    });
  });
</script>

<Instrument
  title="Audit log"
  note="Sign-ins, token grants and failures, revocations, logouts, replay detections, key rotations and every management write. Events are kept for the retention window and can be streamed to a webhook."
>
  {#snippet actions()}
    <button type="button" class="act act-primary" disabled={session.busy} onclick={() => saveSetting("audit")}>
      Commit
    </button>
  {/snippet}

  {#snippet custody()}
    <span class="stamp">Namespace <span class="serial stamp-raw">audit</span></span>
    <Seal state={disabled ? "void" : "endorsed"} label={disabled ? "Not recording" : "Recording"} />
  {/snippet}

  <Section title="Recording" first>
    <Switch
      label="Stop recording audit events"
      consequential
      hint="On, nothing is written or sent to the webhook. Existing events stay until the retention window removes them."
      bind:checked={() => getSettingBool("audit", ["disabled"]), (value: boolean) => setSettingBool("audit", ["disabled"], value)}
    />
    <div class="mt-6 grid max-w-[52ch] gap-6">
      <div>
        <label class="stamp block" for="audit-retention">Retention</label>
        <input
          id="audit-retention"
          class="entry serial mt-1.5"
          autocomplete="off"
          placeholder="90d"
          value={getSettingString("audit", ["retention"])}
          oninput={(e) => setSettingString("audit", ["retention"], e.currentTarget.value)}
        />
        <p class="mt-1.5 text-[12px] leading-[1.5] text-muted">Older events are deleted hourly. Duration, e.g. 30d, 2160h.</p>
      </div>
    </div>
  </Section>

  <Section title="Webhook" note="Every recorded event is POSTed as JSON. With a secret, X-Turna-Signature is sha256=HMAC(secret, timestamp + '.' + body) and X-Turna-Timestamp carries the timestamp.">
    <div class="grid max-w-[60ch] gap-6">
      <div>
        <label class="stamp block" for="audit-webhook-url">URL</label>
        <input
          id="audit-webhook-url"
          class="entry serial mt-1.5"
          autocomplete="off"
          placeholder="https://siem.example.com/turna"
          value={getSettingString("audit", ["webhook_url"])}
          oninput={(e) => setSettingString("audit", ["webhook_url"], e.currentTarget.value)}
        />
      </div>
      <div>
        <label class="stamp block" for="audit-webhook-secret">Signing secret</label>
        <input
          id="audit-webhook-secret"
          class="entry mt-1.5"
          type="password"
          autocomplete="new-password"
          placeholder={getSettingBool("audit", ["webhook_secret_set"]) ? "set — leave empty to keep" : "optional"}
          value={getSettingString("audit", ["webhook_secret"])}
          oninput={(e) => setSettingString("audit", ["webhook_secret"], e.currentTarget.value)}
        />
      </div>
      <div>
        <label class="stamp block" for="audit-webhook-events">Events</label>
        <input
          id="audit-webhook-events"
          class="entry serial mt-1.5"
          autocomplete="off"
          placeholder="all events"
          value={getSettingList("audit", ["webhook_events"])}
          oninput={(e) => setSettingList("audit", ["webhook_events"], e.currentTarget.value)}
        />
        <p class="mt-1.5 text-[12px] leading-[1.5] text-muted">Comma separated, e.g. login, code.reuse, admin.change. Empty sends every event.</p>
      </div>
    </div>
  </Section>

  <Section title="Events">
    <form
      class="flex flex-wrap items-end gap-4"
      onsubmit={(e) => {
        e.preventDefault();
        void load();
      }}
    >
      <div>
        <label class="stamp block" for="audit-filter-event">Event</label>
        <select id="audit-filter-event" class="entry mt-1.5" bind:value={filterEvent}>
          <option value="">All</option>
          {#each events as ev (ev)}
            <option value={ev}>{ev}</option>
          {/each}
        </select>
      </div>
      <div>
        <label class="stamp block" for="audit-filter-outcome">Outcome</label>
        <select id="audit-filter-outcome" class="entry mt-1.5" bind:value={filterOutcome}>
          <option value="">All</option>
          <option value="success">success</option>
          <option value="failure">failure</option>
          <option value="challenge">challenge</option>
        </select>
      </div>
      <div>
        <label class="stamp block" for="audit-filter-subject">Subject</label>
        <input id="audit-filter-subject" class="entry serial mt-1.5" autocomplete="off" bind:value={filterSubject} />
      </div>
      <button type="submit" class="act" disabled={loading}>Filter</button>
    </form>

    <div class="mt-6 overflow-x-auto">
      <table class="w-full text-left text-[13px]">
        <thead>
          <tr class="border-b border-rule">
            <th class="stamp py-2 pr-4">Time</th>
            <th class="stamp py-2 pr-4">Event</th>
            <th class="stamp py-2 pr-4">Outcome</th>
            <th class="stamp py-2 pr-4">Subject</th>
            <th class="stamp py-2 pr-4">Actor / client</th>
            <th class="stamp py-2 pr-4">IP</th>
            <th class="stamp py-2">Detail</th>
          </tr>
        </thead>
        <tbody>
          {#each rows as ev (ev.id)}
            <tr class="border-b border-rule/60 align-top">
              <td class="serial py-2 pr-4 whitespace-nowrap">{new Date(ev.created_at).toLocaleString()}</td>
              <td class="serial py-2 pr-4">{ev.event}</td>
              <td class="py-2 pr-4" class:text-seal={ev.outcome === "failure"}>{ev.outcome}</td>
              <td class="serial py-2 pr-4 break-all">{ev.subject ?? ""}</td>
              <td class="serial py-2 pr-4 break-all">{ev.actor || ev.client_id || ""}</td>
              <td class="serial py-2 pr-4">{ev.ip ?? ""}</td>
              <td class="py-2 text-muted break-all">{summary(ev)}</td>
            </tr>
          {:else}
            <tr><td colspan="7" class="py-6 text-muted">{loading ? "Loading…" : "No events match."}</td></tr>
          {/each}
        </tbody>
      </table>
    </div>
    {#if more}
      <button type="button" class="act mt-4" disabled={loading} onclick={() => void load(true)}>Older events</button>
    {/if}
  </Section>
</Instrument>
