<script lang="ts">
  import active from "svelte-spa-router/active";
  import { storeInfo } from "@/store/store";
  import Link from "./Link.svelte";
  import Group from "./Group.svelte";
</script>

<div class="sidebar-bg border-r border-black">
  <div class="sticky top-0 overflow-auto max-h-svh no-scrollbar">
    <div class="border-b border-black">
      <a
        href="#/"
        class="block"
        use:active={{
          path: `/`,
          className: "bg-black text-white",
          inactiveClassName: "bg-white text-black hover:bg-gray-100",
        }}
      >
        <span class="block px-2 py-1">View</span>
      </a>
    </div>
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
  </div>
</div>

<style lang="scss">
  .sidebar-bg {
    background: url(data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAoAAAAKCAYAAACNMs+9AAAAAXNSR0IArs4c6QAAAB1JREFUKFNjvHjx4n99fX1GBgKAcVQhvhCifvAAAM43KAsXWPfwAAAAAElFTkSuQmCC)
      repeat;
    @apply bg-gray-50;
  }

  :global(.sb-link-active) {
    @apply bg-black text-white;
    > span {
      @apply border-green-500;
    }
  }

  :global(.sb-link-inactive) {
    @apply bg-white text-black;
  }

  /* Hide scrollbar for Chrome, Safari and Opera */
  .no-scrollbar::-webkit-scrollbar {
    display: none;
  }

  /* Hide scrollbar for IE, Edge and Firefox */
  .no-scrollbar {
    -ms-overflow-style: none; /* IE and Edge */
    scrollbar-width: none; /* Firefox */
  }
</style>
