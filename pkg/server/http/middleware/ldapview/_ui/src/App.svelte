<script lang="ts">
  import { onMount } from "svelte";
  import { Server, LogOut, Fingerprint, Info as InfoIcon, LoaderCircle, RefreshCw, PanelLeftClose, PanelLeftOpen, KeyRound, Search, Sparkles } from "@lucide/svelte";
  import { api, setUnauthorized, type Child, type SearchResult, type Session } from "./lib/api";
  import { app } from "./lib/state.svelte";
  import { compactFilter, escapeFilter } from "./lib/format";
  import { detectFlavor, flavorLabel, quickFind } from "./lib/presets";
  import Login from "./components/Login.svelte";
  import Tree from "./components/Tree.svelte";
  import SearchForm from "./components/SearchForm.svelte";
  import Results from "./components/Results.svelte";
  import EntryPanel from "./components/EntryPanel.svelte";
  import Verify from "./components/Verify.svelte";
  import Splitter from "./components/Splitter.svelte";
  import Toasts from "./components/Toasts.svelte";
  import ThemeToggle from "./components/ThemeToggle.svelte";

  /** Values per attribute in search results; the entry panel loads them all. */
  const MAX_VALUES = 100;

  let fatal = $state<string | null>(null);
  let roots = $state<Child[]>([]);
  let selected = $state<string | null>(null);
  let result = $state<SearchResult | null>(null);
  let busy = $state(false);
  let searchError = $state<string | null>(null);
  let verifyOpen = $state(false);
  let find = $state("");
  let navHidden = $state(localStorage.getItem("turna-lv-nav-hidden") === "1");
  // below md the tree is a drawer; below lg the entry panel covers the results
  const media = (q: string) => {
    const mq = matchMedia(q);
    let v = $state(mq.matches);
    mq.addEventListener("change", () => (v = mq.matches));
    return { get value() { return v; } };
  };
  const mobileMQ = media("(max-width: 767px)");
  const wideMQ = media("(min-width: 1024px)");
  const mobile = $derived(mobileMQ.value);
  const wide = $derived(wideMQ.value);
  let navOpen = $state(false);
  const navVisible = $derived(mobile ? navOpen : !navHidden);
  const toggleNav = () => (mobile ? (navOpen = !navOpen) : (navHidden = !navHidden));
  let navWidth = $state(Number(localStorage.getItem("turna-lv-nav-width")) || 280);
  let panelWidth = $state(Number(localStorage.getItem("turna-lv-panel-width")) || 440);

  $effect(() => {
    localStorage.setItem("turna-lv-nav-hidden", navHidden ? "1" : "0");
    localStorage.setItem("turna-lv-nav-width", String(navWidth));
    localStorage.setItem("turna-lv-panel-width", String(panelWidth));
  });

  $effect(() => {
    if (app.info) document.title = app.info.title || "LDAP";
  });

  onMount(async () => {
    setUnauthorized(() => {
      if (app.session) app.toast("Session ended, connect again", "error");
      app.session = null;
    });
    try {
      app.info = await api.info();
      if (app.info.session) await enter(app.info.session);
    } catch (e) {
      fatal = (e as Error).message;
    }
  });

  async function enter(s: Session) {
    app.session = s;
    app.resetTree();
    result = null;
    selected = null;

    let contexts: string[] = [];
    try {
      const dse = (await api.rootDSE()).entries[0];
      app.flavor = detectFlavor(dse);
      contexts = dse?.attributes.find((a) => a.name.toLowerCase() === "namingcontexts")?.values ?? [];
      // AD lists configuration and DNS partitions too; the domain comes first in defaultNamingContext
      const def = dse?.attributes.find((a) => a.name.toLowerCase() === "defaultnamingcontext")?.values[0];
      if (def) contexts = [def, ...contexts.filter((c) => c !== def)];
    } catch {
      app.flavor = "generic";
    }
    if (s.base_dn && !contexts.some((c) => c.toLowerCase() === s.base_dn!.toLowerCase())) contexts.unshift(s.base_dn);
    roots = contexts.map((dn) => ({ dn, rdn: dn }));

    app.baseDN = s.base_dn || contexts[0] || "";
    // keep a base DN the user typed when reconnecting to the same server
    if (s.addr !== localStorage.getItem("turna-lv-last-addr") || !app.query.base_dn) app.query = { ...app.query, base_dn: app.baseDN };
    localStorage.setItem("turna-lv-last-addr", s.addr);
    if (roots.length > 0) {
      app.expanded[roots[0].dn] = true;
      app.loadChildren(roots[0].dn);
    }
  }

  async function logout() {
    await api.logout().catch(() => {});
    app.session = null;
  }

  async function whoami() {
    try {
      const r = await api.whoami();
      app.toast(r.authz_id ? `Bound as ${r.authz_id}` : "Bound anonymously (empty authz id)");
    } catch (e) {
      app.toast(`Who am I: ${(e as Error).message}`, "error");
    }
  }

  function open(dn: string) {
    selected = dn;
    navOpen = false;
  }

  async function run() {
    busy = true;
    searchError = null;
    const q = { ...$state.snapshot(app.query), max_values: MAX_VALUES };
    q.filter = compactFilter(q.filter);
    try {
      result = await api.search(q);
      app.remember($state.snapshot(app.query));
    } catch (e) {
      searchError = (e as Error).message;
      result = null;
    } finally {
      busy = false;
    }
  }

  function quick(e: SubmitEvent) {
    e.preventDefault();
    if (!find.trim()) return;
    app.run({ base_dn: app.baseDN, scope: "sub", filter: quickFind(app.flavor, find, escapeFilter), attributes: [] });
  }

  // run requests from entry cards ("search under", "children") and quick find
  let lastToken = 0;
  $effect(() => {
    if (app.runToken !== lastToken) {
      lastToken = app.runToken;
      run();
    }
  });

  const tls = $derived.by(() => {
    const s = app.session;
    if (!s) return "";
    const kind = s.addr.startsWith("ldaps://") ? "LDAPS" : s.start_tls ? "StartTLS" : "";
    if (!kind) return "";
    return s.insecure ? `${kind} · unverified` : s.custom_ca ? `${kind} · custom CA` : kind;
  });

  const modeLabel = { simple: "credentials", anonymous: "anonymous", unauthenticated: "unauthenticated" };
</script>

{#if fatal}
  <div class="flex h-full items-center justify-center p-6 text-danger">{fatal}</div>
{:else if !app.info}
  <div class="flex h-full items-center justify-center text-faint"><LoaderCircle class="size-5 animate-spin" /></div>
{:else if !app.session}
  <Login info={app.info} onlogin={enter} />
{:else}
  <div class="flex h-full flex-col">
    <header class="flex h-11 shrink-0 items-center gap-1.5 border-b border-line bg-surface px-2 sm:gap-2 sm:px-3">
      <button class="icon-btn shrink-0" onclick={toggleNav} aria-label="Toggle tree" title="Toggle tree" aria-expanded={navVisible}>
        {#if navVisible}<PanelLeftClose class="size-4" />{:else}<PanelLeftOpen class="size-4" />{/if}
      </button>
      <div class="hidden size-6 shrink-0 items-center justify-center rounded-md bg-accent text-accent-ink sm:flex"><Server class="size-3.5" /></div>
      <span class="hidden shrink-0 font-semibold whitespace-nowrap sm:inline">{app.info.title || "LDAP"}</span>
      <div class="ml-1 hidden min-w-0 shrink items-center gap-1.5 overflow-hidden rounded-full border border-line bg-sunk px-2.5 py-0.5 text-[12px] whitespace-nowrap xl:flex" title="{app.session.addr}{app.session.bind_dn ? ` as ${app.session.bind_dn}` : ''}">
        <span class="size-1.5 shrink-0 rounded-full bg-ok"></span>
        <span class="truncate font-mono">{app.session.server}</span>
        {#if tls}<span class="shrink-0 rounded px-1 text-[11px] font-medium {app.session.insecure ? 'bg-danger-soft text-danger' : 'bg-accent-soft text-accent-text'}" title={app.session.insecure ? "Certificate not verified" : app.session.custom_ca ? "Verified with the custom CA" : "Verified with the system CAs"}>{tls}</span>{/if}
        <span class="text-faint">·</span>
        <span class="shrink-0 text-muted">{flavorLabel[app.flavor]}</span>
        <span class="text-faint">·</span>
        <span class="shrink-0 text-muted">{modeLabel[app.session.mode]}</span>
        {#if app.session.bind_dn}<span class="text-faint">·</span><span class="max-w-[260px] truncate font-mono text-muted">{app.session.bind_dn}</span>{/if}
      </div>

      <form class="relative w-[260px] min-w-0 shrink sm:mx-2" onsubmit={quick}>
        <Search class="pointer-events-none absolute top-2 left-2 size-4 text-faint" />
        <input class="input h-8 pl-7" bind:value={find} placeholder={app.flavor === "ad" ? "Find user, group, computer…" : "Find user or group…"} aria-label="Quick find" />
      </form>

      <div class="ml-auto flex shrink-0 items-center whitespace-nowrap sm:gap-0.5">
        <button class="btn btn-ghost px-1.5 sm:px-2.5" onclick={() => (verifyOpen = true)} title="Check another user's password" aria-label="Test login"><KeyRound class="size-3.5" /> <span class="hidden lg:inline">Test login</span></button>
        <button class="icon-btn {app.formatted ? 'text-accent-text' : ''}" onclick={() => app.toggleFormatted()} aria-pressed={app.formatted} aria-label="Readable values" title={app.formatted ? "Showing readable values (dates, flags, SIDs); click for raw" : "Showing raw values; click for readable"}><Sparkles class="size-4" /></button>
        <button class="icon-btn hidden sm:inline-flex" onclick={whoami} title="Who am I" aria-label="Who am I"><Fingerprint class="size-4" /></button>
        <ThemeToggle />
        <button class="btn btn-ghost px-1.5 sm:px-2.5" onclick={logout} title="Disconnect" aria-label="Disconnect"><LogOut class="size-3.5" /> <span class="hidden lg:inline">Disconnect</span></button>
      </div>
    </header>

    <div class="relative flex min-h-0 flex-1">
      {#if navVisible}
        {#if mobile}
          <button class="absolute inset-0 z-20 cursor-default bg-black/30" aria-label="Close tree" onclick={() => (navOpen = false)}></button>
        {/if}
        <nav
          class="flex shrink-0 flex-col bg-surface {mobile ? 'absolute inset-y-0 left-0 z-30 w-[min(85vw,320px)] border-r border-line shadow-[var(--shadow)]' : ''}"
          style={mobile ? "" : `width: ${navWidth}px`}
          aria-label="Directory tree"
        >
          <div class="flex items-center justify-between px-3 pt-2.5 pb-1 text-[11.5px] font-medium text-faint uppercase">
            Directory
            <button class="icon-btn size-6" title="Reload tree" aria-label="Reload tree" onclick={() => app.session && enter(app.session)}><RefreshCw class="size-3.5" /></button>
          </div>
          <div class="min-h-0 flex-1 overflow-auto px-1.5 pb-2" role="tree">
            <button class="mb-0.5 flex h-7 w-full items-center gap-1.5 rounded-md px-2 text-[13px] {selected === '' ? 'bg-accent-soft text-accent-text' : 'text-muted hover:bg-hover'}" onclick={() => open("")}>
              <InfoIcon class="size-3.5" /> Root DSE
            </button>
            {#each roots as r (r.dn)}
              <Tree node={r} {selected} onselect={open} />
            {:else}
              <p class="px-2 py-2 text-[12px] text-faint">No naming contexts found. Set a base DN on the login screen or search below.</p>
            {/each}
          </div>
        </nav>
        {#if !mobile}<Splitter bind:size={navWidth} min={180} max={640} label="Resize tree" />{/if}
      {/if}

      <main class="flex min-w-0 flex-1 flex-col">
        <SearchForm {busy} onrun={run} />

        <div class="relative flex min-h-0 flex-1">
          <section class="flex min-h-0 min-w-0 flex-1 flex-col bg-canvas" aria-label="Search results">
            {#if searchError}
              <div class="m-3 rounded-md bg-danger-soft px-3 py-2 break-words text-danger" role="alert">{searchError}</div>
            {:else if result}
              <Results {result} onopen={open} />
            {:else}
              <div class="flex flex-1 items-center justify-center p-8 text-center text-faint">
                Run a search, use Find above, or pick an entry in the tree.
              </div>
            {/if}
          </section>

          {#if selected !== null}
            {#if wide}
              <Splitter bind:size={panelWidth} min={300} max={960} reverse label="Resize entry panel" />
            {/if}
            <!-- below lg the panel covers the results instead of squeezing them -->
            <aside
              class="flex min-h-0 flex-col bg-surface {wide ? '' : 'absolute inset-0 z-10'}"
              style={wide ? `width: ${panelWidth}px` : ""}
              aria-label="Entry"
            >
              <EntryPanel dn={selected} onopen={open} onclose={() => (selected = null)} />
            </aside>
          {/if}
        </div>
      </main>
    </div>
  </div>

  {#if verifyOpen}
    <Verify onclose={() => (verifyOpen = false)} onopen={open} />
  {/if}
{/if}

<Toasts />
