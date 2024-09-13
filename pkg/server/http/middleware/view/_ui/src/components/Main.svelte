<script lang="ts">
  import { storeInfo } from "@/store/store";
  import { onMount } from "svelte";
  import showdown from "showdown";

  let mounted = false;
  let contentType = "";
  let content = "";

  showdown.setFlavor("github");

  let converter = new showdown.Converter({
    tables: true,
    simplifiedAutoLink: true,
    strikethrough: true,
    tasklists: true,
    openLinksInNewWindow: true,
    emoji: true,
  });

  onMount(() => {
    contentType = $storeInfo.home?.type ?? "";
    switch (contentType.toUpperCase()) {
      case "HTML":
        content = $storeInfo.home?.content ?? "";
        break;
      case "MARKDOWN":
        content = converter.makeHtml($storeInfo.home?.content ?? "");
        break;
      default:
        content = "";
        break;
    }

    mounted = true;
  });
</script>

{#if !mounted}
  <div>
    <p class="block p-1 text-black bg-yellow-300 bg-bottom border-black">
      Loading...
    </p>
  </div>
{:else if contentType != ""}
  <div class="h-full min-h-full markdown-body !bg-gray-50 p-2 list-[square]">
    {@html content}
  </div>
{:else}
  <div class="h-full min-h-full p-2 bg-gray-50">
    <h1 class="text-2xl font-bold">Welcome to the API View</h1>
    <p>
      <b>View</b> shows the list of services and easy access to their Swagger and
      gRPC documentation.
    </p>
  </div>
{/if}
