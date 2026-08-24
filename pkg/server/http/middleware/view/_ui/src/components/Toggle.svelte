<script lang="ts">
  export let name: string = "";
  export let toggle = false;
  let className = "";

  export let forceOpen = false;

  export { className as class };

  $: if (forceOpen) toggle = true;
</script>

<button
  title={name}
  class={`${className} flex justify-between h-8 leading-8 border-b border-black pl-2 pr-1 w-full text-left`}
  onclick={() => (toggle = !toggle)}
>
  <span class="whitespace-nowrap overflow-hidden overflow-ellipsis">
    {name}
  </span>
  <span class="">
    <i
      class={`lni lni-chevron-down text-2xl! transform transition-transform ${toggle ? "" : "-rotate-90"}`}
      aria-hidden="true"
    ></i>
  </span></button
>

<div class={toggle ? "" : "hidden"}>
  <slot />
</div>
