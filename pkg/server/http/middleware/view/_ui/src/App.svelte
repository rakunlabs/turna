<script lang="ts">
  import axios from "axios";
  import { onMount } from "svelte";
  import type { ComponentType } from "svelte";
  import Router from "svelte-spa-router";
  import { storeInfo } from "@/store/store";

  import Sidebar from "./components/Sidebar.svelte";
  import Swagger from "./components/Swagger.svelte";
  import Grpc from "./components/Grpc.svelte";
  import Main from "./components/Main.svelte";
  import Page from "./components/Page.svelte";
  import Iframe from "./components/Iframe.svelte";

  let layout: HTMLElement;
  let mounted = false;

  const routes = new Map<string | RegExp, ComponentType>();
  routes.set("/swagger/:service", Swagger);
  routes.set("/grpc/:name", Grpc);
  routes.set("/page/:name", Page);
  routes.set("/iframe/:name", Iframe);
  routes.set("*", Main);

  onMount(async () => {
    try {
      const { data } = await axios.get("./ui-info");
      if (data) {
        storeInfo.set(data);
      }
    } catch (error) {
      console.error(error);
    } finally {
      mounted = true;
    }
  });
</script>

<div
  bind:this={layout}
  class="grid grid-cols-[14rem,1fr] h-full w-full relative overflow-y-auto"
>
  {#if !mounted}
    <div class="absolute inset-0 flex items-center justify-center">
      <div
        class="animate-spin rounded-full h-32 w-32 border-t-2 border-b-2 border-gray-900"
      ></div>
    </div>
  {:else}
    <Sidebar />
    <div class="h-full w-full">
      <div class="h-full min-h-full overflow-y-auto">
        <Router {routes} />
      </div>
    </div>
  {/if}
</div>
