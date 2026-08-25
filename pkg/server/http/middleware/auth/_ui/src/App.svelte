<script lang="ts">
  import { onMount } from "svelte";

  import Masthead from "./components/Masthead.svelte";
  import IndexRail from "./components/IndexRail.svelte";
  import Docket from "./components/Docket.svelte";

  import OverviewTab from "./components/OverviewTab.svelte";
  import FlowsTab from "./components/FlowsTab.svelte";
  import AccessCheckTab from "./components/AccessCheckTab.svelte";
  import AccountTab from "./components/AccountTab.svelte";
  import DeviceTab from "./components/DeviceTab.svelte";
  import OAuthOverview from "./components/OAuthOverview.svelte";
  import APIKeysTab from "./components/APIKeysTab.svelte";
  import EmailTab from "./components/EmailTab.svelte";
  import MagicLinkTab from "./components/MagicLinkTab.svelte";
  import SignupTab from "./components/SignupTab.svelte";
  import MTLSTab from "./components/MTLSTab.svelte";
  import TotpTab from "./components/TotpTab.svelte";
  import CustomInfoTab from "./components/CustomInfoTab.svelte";
  import SessionProvidersTab from "./components/SessionProvidersTab.svelte";
  import DeviceSettingsTab from "./components/DeviceSettingsTab.svelte";
  import TokenExchangeTab from "./components/TokenExchangeTab.svelte";
  import AdminTab from "./components/AdminTab.svelte";
  import CacheTab from "./components/CacheTab.svelte";
  import EncryptionTab from "./components/EncryptionTab.svelte";
  import AuthFlowGuide from "./components/AuthFlowGuide.svelte";
  import ResourcePage from "./components/ResourcePage.svelte";
  import RecordEditor from "./components/RecordEditor.svelte";
  import LdapGroupsPanel from "./components/LdapGroupsPanel.svelte";

  import { navGroups } from "./lib/navigation";
  import type { Tab } from "./lib/navigation";
  import { docket, messageOf, session } from "./lib/state/session.svelte";
  import { registry } from "./lib/state/registry.svelte";
  import { editor } from "./lib/state/editor.svelte";
  import { route } from "./lib/state/route.svelte";
  import { theme } from "./lib/state/theme.svelte";

  let menuOpen = $state(false);
  let search = $state("");
  let rail = $state<ReturnType<typeof IndexRail> | null>(null);

  // Self-service visitors never learn the admin plane exists; capability
  // decides what is in the index, not what is disabled inside it.
  const visibleGroups = $derived(
    navGroups
      .map((group) => ({
        ...group,
        items: group.items.filter((item) => session.isAdmin || route.isSelfService(item.id)),
      }))
      .filter((group) => group.items.length > 0),
  );

  function selectTab(tab: Tab) {
    route.select(tab);
    menuOpen = false;
  }

  async function refresh() {
    await session.run(async () => {
      await session.loadCapabilities();
      if (!session.isAdmin) {
        registry.loaded = false;
        return;
      }

      await session.loadCore();
      if (registry.loaded || route.needsRegistry(route.tab)) await registry.loadAll();
    });

    session.loading = false;
  }

  async function boot() {
    route.readLocation();

    try {
      await session.loadCapabilities();
    } catch (err) {
      docket.reject(messageOf(err));
      session.loading = false;
      return;
    }

    route.enforce();
    if (route.isResource(route.tab)) editor.reset(route.tab);

    if (session.isAdmin) {
      try {
        await session.loadCore();
        if (route.needsRegistry(route.tab)) await registry.loadAll();
        if (route.recordID && route.isResource(route.tab)) await editor.load(route.tab, route.recordID);
      } catch (err) {
        docket.reject(messageOf(err));
      }
    }

    session.loading = false;
  }

  function onKeydown(event: KeyboardEvent) {
    const target = event.target as HTMLElement | null;
    const typing =
      target?.tagName === "INPUT" || target?.tagName === "TEXTAREA" || target?.isContentEditable;

    if (event.key === "/" && !typing && !event.metaKey && !event.ctrlKey) {
      event.preventDefault();
      menuOpen = true;
      rail?.focusSearch();
    }

    if (event.key === "Escape" && menuOpen) menuOpen = false;
  }

  onMount(() => {
    const teardownTheme = theme.init();
    session.deriveApiBase();
    void boot();

    const onHash = () => {
      route.readLocation();
      route.enforce();
      if (route.recordID && route.isResource(route.tab)) {
        void editor.load(route.tab, route.recordID);
      } else {
        selectTab(route.tab);
      }
    };

    window.addEventListener("hashchange", onHash);
    return () => {
      teardownTheme();
      window.removeEventListener("hashchange", onHash);
    };
  });

  async function reloadAfterWrite() {
    await session.loadCore();
    await registry.loadAll();
  }
</script>

<svelte:window onkeydown={onKeydown} />

<div class="grid h-screen grid-rows-[auto_1fr] overflow-hidden bg-ground text-ink">
  <Masthead onrefresh={refresh} onmenu={() => (menuOpen = !menuOpen)} {menuOpen} />

  <div class="grid min-h-0 overflow-hidden lg:grid-cols-[15rem_1fr]">
    <div class="hidden min-h-0 lg:block">
      <IndexRail bind:this={rail} groups={visibleGroups} active={route.tab} onselect={selectTab} bind:search />
    </div>

    {#if menuOpen}
      <div class="fixed inset-0 z-40 lg:hidden">
        <button
          type="button"
          class="absolute inset-0 bg-ink/45"
          aria-label="Close index"
          onclick={() => (menuOpen = false)}
        ></button>
        <div data-theme="dark" class="absolute inset-y-0 left-0 w-[17rem] max-w-[85vw] border-r border-rule shadow-2xl">
          <IndexRail groups={visibleGroups} active={route.tab} onselect={selectTab} bind:search />
        </div>
      </div>
    {/if}

    <main class="min-h-0 min-w-0 overflow-y-auto overscroll-contain bg-ground">
      {#if session.loading}
        <div class="grid min-h-[60vh] place-items-center px-6">
          <p class="stamp">Reading the register…</p>
        </div>
      {:else}
        {#if session.capabilities?.anonymous_admin}
          <p class="hatch border-b border-seal/40 px-5 py-2.5 text-[13px] text-ink sm:px-8 lg:px-10">
            <span class="stamp text-seal">Break-glass</span>
            <span class="ml-3">
              No <code class="serial">X-User</code> on this request and admin access is allowed anyway.
              Intended for local recovery — do not leave this route publicly reachable.
            </span>
          </p>
        {:else if session.capabilities?.bootstrap_admin}
          <p class="hatch border-b border-caution/40 px-5 py-2.5 text-[13px] text-ink sm:px-8 lg:px-10">
            <span class="stamp text-caution">Bootstrap</span>
            <span class="ml-3">
              No admin permission is configured, so every authenticated request manages this instance.
              Set one under Platform → Admin once the first role exists.
            </span>
          </p>
        {/if}

        {#if route.tab === "overview"}
          <OverviewTab onselect={selectTab} />
        {:else if route.tab === "flows"}
          <FlowsTab />
        {:else if route.tab === "check"}
          <AccessCheckTab />
        {:else if route.tab === "account"}
          <AccountTab />
        {:else if route.tab === "device"}
          <DeviceTab />
        {:else if route.tab === "oauth2-overview"}
          <OAuthOverview />
        {:else if route.tab === "api-keys"}
          <APIKeysTab />
        {:else if route.tab === "email"}
          <EmailTab />
        {:else if route.tab === "magic-link"}
          <MagicLinkTab />
        {:else if route.tab === "signup"}
          <SignupTab />
        {:else if route.tab === "mtls"}
          <MTLSTab onselect={selectTab} />
        {:else if route.tab === "totp"}
          <TotpTab />
        {:else if route.tab === "custom-info"}
          <CustomInfoTab />
        {:else if route.tab === "session-providers"}
          <SessionProvidersTab />
        {:else if route.tab === "device-settings"}
          <DeviceSettingsTab />
        {:else if route.tab === "token-exchange"}
          <TokenExchangeTab />
        {:else if route.tab === "admin"}
          <AdminTab />
        {:else if route.tab === "cache"}
          <CacheTab />
        {:else if route.tab === "encryption"}
          <EncryptionTab />
        {:else if route.tab === "docs"}
          <AuthFlowGuide />
        {:else if route.isResource(route.tab)}
          {#if editor.open}
            <RecordEditor oncommitted={reloadAfterWrite} onclose={() => route.select(route.tab)} />
          {:else}
            <ResourcePage kind={route.tab} oncommitted={reloadAfterWrite}>
              {#snippet extra()}
                {#if route.tab === "lmaps"}
                  <LdapGroupsPanel />
                {/if}
              {/snippet}
            </ResourcePage>
          {/if}
        {/if}
      {/if}
    </main>
  </div>
</div>

<Docket />
