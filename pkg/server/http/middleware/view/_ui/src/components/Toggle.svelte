<script lang="ts">
  import { onMount } from "svelte";

  export let name: string = "";
  export let toggle = false;
  let className = "";

  export let toggleCheck: (() => boolean) | undefined = undefined;

  export { className as class };

  onMount(() => {
    if (toggleCheck) {
      toggle = toggleCheck();
    }
  });
</script>

<button
  class={`${className} block h-8 leading-8 border-b border-black px-2 w-full text-left whitespace-nowrap overflow-hidden overflow-ellipsis`}
  on:click={() => (toggle = !toggle)}
>
  {name}
  <span class="float-right">
    <i
      class={`!text-2xl lni lni-chevron-down transform transition-all ${toggle ? "" : "-rotate-90"}`}
    />
  </span></button
>

<div class={toggle ? "" : "hidden"}>
  <slot />
</div>
