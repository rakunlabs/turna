<script lang="ts">
  import { onMount } from "svelte";
  import type { ComponentType } from "svelte";
  import Router from "svelte-spa-router";

  import { storeApiPrefix, storeNavbar } from "@/store/store";

  import Sidebar from "@/components/Sidebar.svelte";
  import Navbar from "@/components/Navbar.svelte";
  import Toast from "./components/Toast.svelte";

  import MainPage from "@/pages/Main.svelte";
  import LdapPage from "./pages/Ldap.svelte";

  import NotFound from "./pages/NotFound.svelte";

  let layout: HTMLElement;
  let mounted = false;

  const routes = new Map<string | RegExp, ComponentType>();
  routes.set("/ldap", LdapPage);
  routes.set("/", MainPage);
  routes.set("/*", NotFound);

  onMount(async () => {
    try {
      const res = await fetch("./info");
      const data = await res.json();

      storeApiPrefix.set(data["prefix_path"] || "");
    } catch (error) {
      console.error(error);
    }

    mounted = true;
  });
</script>

<Toast />

<div
  bind:this={layout}
  class="grid h-full w-full relative overflow-y-auto bg-slate-100"
  class:grid-cols-[8rem,1fr]={$storeNavbar.sideBarOpen}
  class:grid-cols-[0,1fr]={!$storeNavbar.sideBarOpen}
>
  {#if !mounted}
    <div class="absolute inset-0 flex items-center justify-center">
      <div
        class="animate-spin rounded-full h-32 w-32 border-t-2 border-b-2 border-gray-900"
      ></div>
    </div>
  {:else}
    <Sidebar />
    <div class="h-full w-full grid grid-rows-[1.75rem]">
      <Navbar />
      <Router {routes} />
    </div>
  {/if}
</div>
