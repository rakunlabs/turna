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
  title={name}
  class={`${className} flex justify-between h-8 leading-8 border-b border-black pl-2 pr-1 w-full text-left`}
  on:click={() => (toggle = !toggle)}
>
  <span class="whitespace-nowrap overflow-hidden overflow-ellipsis">
    {name}
  </span>
  <span class="">
    <i
      class={`!text-2xl lni lni-chevron-down transform transition-all ${toggle ? "" : "-rotate-90"}`}
    />
  </span></button
>

<div class={toggle ? "" : "hidden"}>
  <slot />
</div>
