<script lang="ts">
  import active from "svelte-spa-router/active";

  export let name: string;
  export let path: string;
</script>

<div class="border-b border-black h-7">
  <a
    href={`#${path}`}
    class="block h-full hover:bg-slate-700 hover:text-white"
    use:active={{
      path: path,
      className: "bg-black text-white",
      inactiveClassName: "bg-white text-black",
    }}
  >
    <span class="block px-2">{name}</span>
  </a>
</div>
