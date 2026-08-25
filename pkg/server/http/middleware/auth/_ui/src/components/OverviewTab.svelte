<script lang="ts">
  import { onMount } from "svelte";

  import Seal from "./ui/Seal.svelte";
  import Serial from "./ui/Serial.svelte";
  import Section from "./ui/Section.svelte";
  import { custodyLine } from "../lib/records";
  import { session } from "../lib/state/session.svelte";
  import { registry } from "../lib/state/registry.svelte";
  import { settings } from "../lib/state/settings.svelte";
  import { hrefOf, plainClick, type Tab } from "../lib/navigation";

  let { onselect }: { onselect: (tab: Tab) => void } = $props();

  /** Plain click stays in the SPA; modified clicks let the anchor open a new tab. */
  function select(event: MouseEvent, tab: Tab) {
    if (!plainClick(event)) return;

    event.preventDefault();
    onselect(tab);
  }

  let clientCount = $state<number | null>(null);
  let surveyed = $state(false);
  /** Who last amended this instance's configuration, and when. */
  let countersign = $state("");

  const dash = $derived(session.dashboard);

  /**
   * The holdings of this instance, as an index rather than a row of metric
   * cards. Each line is a door to the page that owns those records.
   */
  const holdings = $derived([
    { label: "Users", value: dash?.total_users ?? 0, tab: "users" as Tab },
    { label: "Service accounts", value: dash?.total_service_accounts ?? 0, tab: "service-accounts" as Tab },
    { label: "Roles", value: dash?.total_roles ?? 0, tab: "roles" as Tab },
    { label: "Permissions", value: dash?.total_permissions ?? 0, tab: "permissions" as Tab },
  ]);

  /**
   * The ceremony: the steps that take an empty instance to one that can issue
   * tokens. This is the console's onboarding during setup and collapses to a
   * single standing line once every step is done — the operator who comes back
   * a month later should not have to scroll past a completed checklist.
   */
  const ceremony = $derived([
    {
      label: "Signing key issued",
      done: registry.jwks.length > 0,
      detail: registry.jwks.length
        ? `RS256 · kid ${String(registry.signingKey.kid ?? "—")}`
        : "No published JWKS key — tokens cannot be verified yet",
      tab: "oauth2-overview" as Tab,
    },
    {
      label: "Admin permission set",
      done: session.capabilities?.admin_permission_configured === true,
      detail:
        session.capabilities?.admin_permission_configured
          ? `Requires ${session.capabilities.admin_permission}`
          : "Every authenticated request currently manages this instance",
      tab: "admin" as Tab,
    },
    {
      label: "Identity source connected",
      done: (dash?.total_users ?? 0) > 0 || registry.ldapActive || registry.providerCount > 0,
      detail:
        registry.ldapActive || registry.providerCount > 0
          ? "Federated source active"
          : `${dash?.total_users ?? 0} local users`,
      tab: "users" as Tab,
    },
    {
      label: "OAuth client registered",
      done: (clientCount ?? 0) > 0,
      detail:
        clientCount === null
          ? "Reading…"
          : clientCount > 0
            ? `${clientCount} client${clientCount === 1 ? "" : "s"} accepted at the token endpoint`
            : "Nothing can exchange credentials for a token yet",
      tab: "clients" as Tab,
    },
    {
      label: "Mail relay configured",
      done: Boolean(String(settings.pathValue("email", ["smtp", "host"]) ?? "").trim()),
      detail: String(settings.pathValue("email", ["smtp", "host"]) ?? "").trim()
        ? String(settings.pathValue("email", ["smtp", "host"]))
        : "Optional — needed for email login, magic links and signup",
      tab: "email" as Tab,
    },
  ]);

  const outstanding = $derived(ceremony.filter((step) => !step.done));
  const settled = $derived(surveyed && outstanding.length === 0);

  const references = $derived([
    { label: "OpenID configuration", href: `${session.oauthBase}/oauth2/.well-known/openid-configuration` },
    { label: "Authorization server metadata", href: `${session.oauthBase}/oauth2/.well-known/oauth-authorization-server` },
    { label: "JWKS", href: `${session.oauthBase}/oauth2/certs` },
    { label: "API reference", href: `${session.oauthBase}/swagger/index.html` },
  ]);

  /**
   * Overview stays off the bulk admin load, so it surveys only what the
   * ceremony actually needs: the published keys, one settings namespace, and
   * the client count.
   */
  onMount(async () => {
    if (!session.isAdmin) return;

    await Promise.allSettled([
      registry.loadJWKS(),
      settings.load("email"),
      (async () => {
        const res = await session.request<unknown[]>("oauth/clients");
        clientCount = (res.payload ?? []).length;
      })(),
      (async () => {
        // The settings register carries updated_by/updated_at per namespace;
        // the most recent one is who last changed what this instance does.
        const res = await session.request<Record<string, unknown>[]>("settings");
        const latest = (res.payload ?? [])
          .filter((item) => typeof item.updated_at === "string")
          .sort((a, b) => String(b.updated_at).localeCompare(String(a.updated_at)))[0];

        if (latest) countersign = custodyLine(latest);
      })(),
    ]);

    surveyed = true;
  });
</script>

<article class="px-5 pb-24 pt-8 sm:px-8 lg:px-10">
  <!-- The certificate of standing: what this instance currently IS. -->
  <header class="guilloche max-w-[104ch] border border-rule bg-sheet">
    <div class="flex flex-wrap items-start justify-between gap-x-8 gap-y-3 px-5 pt-5 sm:px-7">
      <div class="min-w-0">
        <h1 class="text-[1.6rem] font-bold leading-[1.15] tracking-[-0.02em] text-ink sm:text-[1.85rem]">
          Turna Auth
        </h1>
        <p class="serial mt-1.5 text-[13px] text-muted">
          {session.info?.prefix_path ?? "/auth"} · {session.info?.storage ?? "postgres"}
        </p>
      </div>

      <Seal
        state={session.linked ? "endorsed" : "broken"}
        label={session.linked ? "Linked" : "No link"}
        size={13}
      />
    </div>

    <div class="flex flex-wrap items-end gap-x-10 gap-y-6 px-5 pb-5 pt-6 sm:px-7">
      <div class="min-w-0">
        <Serial value={session.version} size="xl" />
        <p class="stamp mt-3">Auth version</p>
      </div>

      <p class="max-w-[46ch] flex-1 basis-72 text-[13px] leading-[1.65] text-muted">
        Every write to this instance advances the version, appends an event and notifies the other
        instances, which converge by polling. Nothing here needs a restart to take effect — this
        number is how you know a change actually landed.
      </p>
    </div>

    <!-- The countersign: who last changed what this instance does. -->
    <div class="flex flex-wrap items-baseline gap-x-3 gap-y-1 border-t border-rule px-5 py-3 sm:px-7">
      <span class="stamp">Last countersigned</span>
      <span class="stamp-raw serial text-ink">{countersign || (surveyed ? "never amended" : "…")}</span>
    </div>
  </header>

  <div class="max-w-[104ch]">
    <Section title="Holdings" first={false}>
      <ul class="-mt-1">
        {#each holdings as item (item.label)}
          <li>
            <a
              href={hrefOf(item.tab)}
              class="group flex w-full items-baseline gap-3 py-2.5 text-left no-underline transition-colors hover:text-carbon"
              onclick={(event) => select(event, item.tab)}
            >
              <span class="shrink-0 text-[13.5px] text-ink group-hover:text-carbon">{item.label}</span>
              <span class="mb-[3px] min-w-6 flex-1 border-b border-dotted border-rule"></span>
              <span class="serial shrink-0 text-[15px] font-semibold text-ink group-hover:text-carbon">
                {item.value}
              </span>
            </a>
          </li>
        {/each}
      </ul>
    </Section>

    <Section
      title="Ceremony"
      note={settled
        ? ""
        : "The steps that take an empty instance to one that can issue tokens. Each line reads live state — nothing here is a stored checkbox."}
    >
      {#snippet aside()}
        {#if surveyed}
          <span class="stamp {settled ? 'text-endorsed' : 'text-caution'}">
            {settled ? "Complete" : `${outstanding.length} outstanding`}
          </span>
        {/if}
      {/snippet}

      {#if settled}
        <p class="flex items-center gap-3 py-1 text-[13.5px] text-ink">
          <Seal state="endorsed" />
          This instance is fully commissioned — signing key, admin permission, identity source, client
          and mail relay are all in place.
        </p>
      {:else}
        <ol class="-mt-2">
          {#each ceremony as step (step.label)}
            <li class="flex flex-wrap items-baseline gap-x-4 gap-y-1 border-b border-rule py-3 last:border-b-0">
              <span class="flex shrink-0 items-center gap-2.5">
                <Seal state={step.done ? "endorsed" : "void"} />
                <span class="text-[13.5px] {step.done ? 'text-ink' : 'font-semibold text-ink'}">
                  {step.label}
                </span>
              </span>

              <span class="min-w-0 flex-1 basis-64 text-[12.5px] leading-[1.5] text-muted">
                {step.detail}
              </span>

              {#if !step.done}
                <a
                  href={hrefOf(step.tab)}
                  class="act shrink-0 no-underline"
                  onclick={(event) => select(event, step.tab)}
                >
                  Set up
                </a>
              {/if}
            </li>
          {/each}
        </ol>
      {/if}
    </Section>

    <Section title="Operations">
      <div class="flex flex-wrap gap-2">
        <button type="button" class="act" disabled={session.busy} onclick={() => registry.syncLdap()}>
          Run LDAP sync
        </button>
        <a href={hrefOf("check")} class="act no-underline" onclick={(event) => select(event, "check")}>
          Check an identity
        </a>
        <a href={hrefOf("flows")} class="act no-underline" onclick={(event) => select(event, "flows")}>
          Review enabled flows
        </a>
      </div>
    </Section>

    <Section title="Published endpoints" note="Served from this instance and safe to hand to a client.">
      <ul>
        {#each references as item (item.href)}
          <li class="border-b border-rule last:border-b-0">
            <a
              class="flex items-baseline gap-3 py-2.5 no-underline hover:underline"
              href={item.href}
              target="_blank"
              rel="noreferrer"
            >
              <span class="shrink-0 text-[13.5px] text-carbon">{item.label}</span>
              <span class="serial min-w-0 flex-1 truncate text-right text-[12px] text-muted">
                {item.href.replace(/^https?:\/\/[^/]+/, "")}
              </span>
            </a>
          </li>
        {/each}
      </ul>
    </Section>
  </div>
</article>
