<script lang="ts">
  import axios from "axios";
  import { onMount } from "svelte";
  import { get } from "svelte/store";
  import Router, { type RouteDefinition } from "svelte-spa-router";
  import { storeInfo } from "@/store/store";
  import {
    buildNavigation,
    recordNavigationVisit,
    routePath,
  } from "@/navigation";
  import { readSidebarOpen, writeSidebarOpen } from "@/preferences";

  import Sidebar from "./components/Sidebar.svelte";
  import Swagger from "./components/Swagger.svelte";
  import Grpc from "./components/Grpc.svelte";
  import Main from "./components/Main.svelte";
  import Page from "./components/Page.svelte";
  import Iframe from "./components/Iframe.svelte";
  import CommandPalette from "./components/CommandPalette.svelte";
  import OverlayScroll from "./components/OverlayScroll.svelte";

  let mounted = false;
  let paletteOpen = false;
  let sidebarOpen = readSidebarOpen();
  let shortcutPrefix = "Ctrl";

  const setSidebarOpen = (open: boolean) => {
    sidebarOpen = open;
    writeSidebarOpen(open);
  };

  const routes: RouteDefinition = new Map();
  routes.set("/swagger/*", Swagger);
  routes.set("/grpc/*", Grpc);
  routes.set("/page/*", Page);
  routes.set("/iframe/*", Iframe);
  routes.set("*", Main);

  const onKeydown = (event: KeyboardEvent) => {
    const key = event.key.toLocaleLowerCase();
    const modifier = event.metaKey || event.ctrlKey;
    const target = event.target as HTMLElement | null;
    const editing =
      target?.tagName === "INPUT" ||
      target?.tagName === "TEXTAREA" ||
      target?.isContentEditable;

    if (modifier && key === "k") {
      event.preventDefault();
      paletteOpen = true;
    } else if (modifier && key === "b" && !editing) {
      event.preventDefault();
      setSidebarOpen(!sidebarOpen);
    } else if (event.key === "Escape" && paletteOpen) {
      paletteOpen = false;
    }
  };

  const trackVisit = () => {
    const currentPath = window.location.hash.slice(1) || "/";
    const item = buildNavigation(get(storeInfo)).find(
      (candidate) => routePath(candidate.type, candidate.path) === currentPath
    );
    if (item) recordNavigationVisit(item.href);
  };

  const loadInfo = async () => {
    try {
      const { data } = await axios.get("./ui-info");
      if (data) {
        storeInfo.set(data);
        trackVisit();
      }
    } catch (error) {
      console.error(error);
    } finally {
      mounted = true;
    }
  };

  onMount(() => {
    shortcutPrefix = /Mac|iPhone|iPad/.test(navigator.platform) ? "Cmd" : "Ctrl";
    window.addEventListener("hashchange", trackVisit);
    void loadInfo();

    return () => window.removeEventListener("hashchange", trackVisit);
  });
</script>

<svelte:window onkeydown={onKeydown} />

<div
  class:sidebar-closed={!sidebarOpen}
  class="layout relative grid h-full w-full overflow-hidden"
>
  {#if !mounted}
    <div class="absolute inset-0 flex items-center justify-center">
      <i
        class="lni lni-spinner-3 animate-spin text-[40px]!"
        role="status"
        aria-label="Loading View"
      ></i>
    </div>
  {:else}
    {#if sidebarOpen}
      <div class="sidebar-shell min-h-0">
        <Sidebar
          {shortcutPrefix}
          onsearch={() => (paletteOpen = true)}
          oncollapse={() => setSidebarOpen(false)}
        />
      </div>
      <button
        type="button"
        class="sidebar-backdrop fixed inset-0 z-20 hidden bg-black/40"
        aria-label="Close sidebar"
        onclick={() => setSidebarOpen(false)}
      ></button>
    {:else}
      <nav
        class="flex h-full flex-col border-r border-black bg-yellow-50"
        aria-label="View controls"
      >
        <button
          type="button"
          class="grid h-8 w-full place-items-center border-b border-black hover:bg-yellow-200 focus-visible:bg-yellow-200 focus-visible:outline-none"
          title={`Open sidebar (${shortcutPrefix} B)`}
          aria-label="Open sidebar"
          onclick={() => setSidebarOpen(true)}
        >
          <i class="lni lni-chevron-left text-2xl! rotate-180" aria-hidden="true"></i>
        </button>
        <button
          type="button"
          class="grid h-8 w-full place-items-center border-b border-black hover:bg-yellow-200 focus-visible:bg-yellow-200 focus-visible:outline-none"
          title={`Search (${shortcutPrefix} K)`}
          aria-label="Search View"
          onclick={() => (paletteOpen = true)}
        >
          <i class="lni lni-search-1 text-2xl!" aria-hidden="true"></i>
        </button>
      </nav>
    {/if}
    <main class="h-full min-h-0 w-full min-w-0">
      <OverlayScroll class="h-full">
        <Router {routes} />
      </OverlayScroll>
    </main>
  {/if}
</div>

{#if paletteOpen}
  <CommandPalette onclose={() => (paletteOpen = false)} />
{/if}

<style>
  .layout {
    grid-template-columns: 14rem minmax(0, 1fr);
  }

  .layout.sidebar-closed {
    grid-template-columns: 2rem minmax(0, 1fr);
  }

  @media (max-width: 640px) {
    .layout {
      grid-template-columns: 2rem minmax(0, 1fr);
    }

    .sidebar-shell {
      position: fixed;
      inset: 0 auto 0 0;
      z-index: 30;
      width: min(17rem, 86vw);
    }

    .sidebar-backdrop {
      display: block;
    }
  }
</style>
