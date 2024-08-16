<script lang="ts">
  import { storeInfo } from "@/store/store";
  import { onMount } from "svelte";

  export let params: Record<string, string> | undefined = {};

  let src = "";
  let error = "";

  onMount(() => {
    let iframe = $storeInfo.iframe.find(
      (iframe) => iframe.path === params?.name
    );

    console.log(iframe);

    if (iframe) {
      error = "";
      src = iframe.url;
    } else {
      error = "Iframe not found";
    }
  });
</script>

{#if error.length > 0}
  <div class="flex items-center justify-center h-full w-full">
    <span class="text-white bg-red-500">{error}</span>
  </div>
{:else}
  <iframe class="w-full h-full" {src} title={params?.name} />
{/if}
