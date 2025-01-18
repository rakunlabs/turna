<script lang="ts">
  import active from "svelte-spa-router/active";

  type PosTypes = "grpc" | "swagger" | "page" | "iframe";

  export let name: string | undefined = undefined;
  export let path: string | undefined = undefined;

  export let icon = false;
  export let type: PosTypes;

  const sanitizePath = (v: string) => {
    // remove only leading slash
    return v.replace(/^\/+/, "");
  };
</script>

{#if path}
  <a
    href={`#/${type.toString()}/${sanitizePath(path)}`}
    class="block border-b border-black h-7 leading-7 box-content"
    use:active={{
      path: `/${type.toString()}/${encodeURI(sanitizePath(path))}`,
      className: "sb-link-active",
      inactiveClassName: "sb-link-inactive hover:bg-gray-100",
    }}
    title={type.toString() + " - " + path}
  >
    <span
      class="flex px-1 border-l-4 border-gray-400 whitespace-nowrap overflow-hidden overflow-ellipsis"
    >
      {#if icon}
        <span
          class={`font-mono px-1 -ml-1 mr-1 text-white ${type.toString() === "swagger" ? "bg-green-500" : type.toString() === "grpc" ? "bg-blue-500" : type.toString() === "page" ? "bg-indigo-500" : type.toString() === "iframe" ? "bg-teal-500" : ""}`}
        >
          {type.toString().toUpperCase()[0]}
        </span>
      {/if}
      {name ?? type.toString()}
    </span>
  </a>
{/if}
