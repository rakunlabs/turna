<script lang="ts">
  import { router } from "svelte-spa-router";
  import { storeInfo } from "@/store/store";
  import Link from "./Link.svelte";
  import Group from "./Group.svelte";
  import OverlayScroll from "./OverlayScroll.svelte";

  let {
    shortcutPrefix = "Ctrl",
    onsearch,
    oncollapse,
  }: {
    shortcutPrefix?: string;
    onsearch: () => void;
    oncollapse: () => void;
  } = $props();

  const home = $derived(router.location === "/");
</script>

<aside class="sidebar-bg flex h-full min-h-0 flex-col border-r border-black">
  <div
    class={`flex h-8 shrink-0 items-stretch border-b border-black ${home ? "bg-black text-white" : "bg-white text-black"}`}
  >
    <a
      href="#/"
      class={`flex min-w-0 flex-1 items-center px-2 ${home ? "" : "hover:bg-gray-100"}`}
    >
      View
    </a>
    <button
      type="button"
      class={`grid w-8 place-items-center ${home ? "hover:bg-white/20" : "hover:bg-gray-100"} focus-visible:bg-yellow-100 focus-visible:text-black focus-visible:outline-none`}
      title={`Search (${shortcutPrefix} K)`}
      aria-label="Search View"
      onclick={onsearch}
    >
      <i class="lni lni-search-1 text-2xl!" aria-hidden="true"></i>
    </button>
    <button
      type="button"
      class={`grid w-8 place-items-center ${home ? "hover:bg-white/20" : "hover:bg-gray-100"} focus-visible:bg-yellow-100 focus-visible:text-black focus-visible:outline-none`}
      title={`Close sidebar (${shortcutPrefix} B)`}
      aria-label="Close sidebar"
      onclick={oncollapse}
    >
      <i class="lni lni-chevron-left text-2xl!" aria-hidden="true"></i>
    </button>
  </div>

  <OverlayScroll class="min-h-0 flex-1">
    {#if ($storeInfo.iframe || []).length > 0}
      <div>
        <span
          class="block h-8 leading-8 bg-yellow-100 border-b border-black px-2 w-full text-left"
        >
          Iframes
        </span>
        {#each $storeInfo.iframe || [] as iframe}
          <Link path={iframe.path} name={iframe.name} type="iframe" />
        {/each}
      </div>
    {/if}
    {#if ($storeInfo.page || []).length > 0}
      <div>
        <span
          class="block h-8 leading-8 bg-yellow-100 border-b border-black px-2 w-full text-left"
        >
          Pages
        </span>
        {#each $storeInfo.page || [] as page}
          <Link
            path={page.path + (page.path_extra ?? "")}
            name={page.name}
            type="page"
          />
        {/each}
      </div>
    {/if}
    {#if ($storeInfo.grpc || []).length > 0}
      <div>
        <span
          class="block h-8 leading-8 bg-yellow-100 border-b border-black px-2 w-full text-left"
        >
          gRPC APIs
        </span>
        {#each $storeInfo.grpc || [] as grpc}
          <Link path={grpc.name} name={grpc.name} type="grpc" />
        {/each}
      </div>
    {/if}
    {#if ($storeInfo.swagger || []).length > 0}
      <div>
        <span
          class="block h-8 leading-8 bg-yellow-100 border-b border-black px-2 w-full text-left"
        >
          Swagger APIs
        </span>
        {#each $storeInfo.swagger || [] as swagger}
          <Link path={swagger.name} name={swagger.name} type="swagger" />
        {/each}
      </div>
    {/if}
    <Group groups={$storeInfo.groups} />
  </OverlayScroll>
</aside>

<style>
  .sidebar-bg {
    background: url(data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAoAAAAKCAYAAACNMs+9AAAAAXNSR0IArs4c6QAAAB1JREFUKFNjvHjx4n99fX1GBgKAcVQhvhCifvAAAM43KAsXWPfwAAAAAElFTkSuQmCC)
      repeat;
    background-color: #f9fafb;
  }

  :global(.sb-link-active) {
    background-color: #000;
    color: #fff;
  }

  :global(.sb-link-active > span) {
    border-color: #22c55e;
  }

  :global(.sb-link-inactive) {
    background-color: #fff;
    color: #000;
  }
</style>
