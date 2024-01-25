<script lang="ts">
  import active from "svelte-spa-router/active";
  import { storeInfo } from "@/store/store";
</script>

<div class="sidebar-bg border-r border-black">
  <div class="sticky top-0">
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
    {#if Object.keys($storeInfo.swagger).length > 0}
      <div>
        <span
          class="block capitalize h-8 leading-8 bg-gray-50 border-b border-black px-2 w-full text-left"
        >
          Swagger APIs
        </span>
        {#each Object.keys($storeInfo.swagger) as swagger}
          <a
            href={`#/swagger/${swagger}`}
            class="block border-b border-black h-8 leading-8"
            use:active={{
              path: `/swagger/${encodeURIComponent(swagger)}`,
              className: "sb-link-active",
              inactiveClassName: "sb-link-inactive hover:bg-gray-100",
            }}
            title={swagger}
          >
            <span
              class="block px-1 border-l-4 border-gray-400 whitespace-nowrap overflow-hidden overflow-ellipsis"
            >
              {swagger}
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
</style>
