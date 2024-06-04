<script lang="ts">
  import active from "svelte-spa-router/active";
  import { storeInfo } from "@/store/store";
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
          inactiveClassName: "bg-white text-black",
        }}
      >
        <span class="block px-2 py-1">View</span>
      </a>
    </div>
    {#if $storeInfo.grpc?.length > 0}
      <div>
        <span
          class="block h-8 leading-8 bg-gray-50 border-b border-black px-2 w-full text-left"
        >
          gRPC APIs
        </span>
        {#each $storeInfo.grpc as grpc}
          <a
            href={`#/grpc/${grpc.name}`}
            class="block border-b border-black h-8 leading-8"
            use:active={{
              path: `/grpc/${encodeURIComponent(grpc.name)}`,
              className: "sb-link-active",
              inactiveClassName: "sb-link-inactive hover:bg-gray-100",
            }}
            title={grpc.name}
          >
            <span
              class="block px-1 border-l-4 border-gray-400 whitespace-nowrap overflow-hidden overflow-ellipsis"
            >
              {grpc.name}
            </span>
          </a>
        {/each}
      </div>
    {/if}
    {#if $storeInfo.swagger?.length > 0}
      <div>
        <span
          class="block h-8 leading-8 bg-gray-50 border-b border-black px-2 w-full text-left"
        >
          Swagger APIs
        </span>
        {#each $storeInfo.swagger as swagger}
          <a
            href={`#/swagger/${swagger.name}`}
            class="block border-b border-black h-8 leading-8"
            use:active={{
              path: `/swagger/${encodeURIComponent(swagger.name)}`,
              className: "sb-link-active",
              inactiveClassName: "sb-link-inactive hover:bg-gray-100",
            }}
            title={swagger.name}
          >
            <span
              class="block px-1 border-l-4 border-gray-400 whitespace-nowrap overflow-hidden overflow-ellipsis"
            >
              {swagger.name}
            </span>
          </a>
        {/each}
      </div>
    {/if}
  </div>
</div>

<style lang="scss">
  .sidebar-bg {
    background: url(data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAoAAAAKCAYAAACNMs+9AAAAAXNSR0IArs4c6QAAAB1JREFUKFNjvHjx4n99fX1GBgKAcVQhvhCifvAAAM43KAsXWPfwAAAAAElFTkSuQmCC)
      repeat;
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
