<script lang="ts">
  import Icon from "@/components/Icon.svelte";
  import { storeNavbar } from "@/store/store";
  import update from "immutability-helper";

  let className = "";
  export { className as class };
</script>

<div
  class={`${className} bg-black text-white flex items-center border-b border-gray-500`}
>
  <button
    class="mr-2 hover:bg-slate-700 flex items-center h-full px-1"
    on:click={() =>
      storeNavbar.update((v) =>
        update(v, { sideBarOpen: { $set: !v.sideBarOpen } }),
      )}
  >
    <Icon
      icon={$storeNavbar.sideBarOpen ? "left" : "menu"}
      height="18"
      class="fill-white"
    />
  </button>
  <span>
    {$storeNavbar.title}
  </span>
</div>
