<script lang="ts">
  import { storeInfo } from "@/store/store";
  import { onMount } from "svelte";
  import { marked } from "marked";
  import DOMPurify from "dompurify";

  let mounted = false;
  let contentType = "";
  let content = "";

  // links of the rendered page open in a new tab, like before
  DOMPurify.addHook("afterSanitizeAttributes", (node) => {
    if (node.tagName === "A" && node.hasAttribute("href")) {
      node.setAttribute("target", "_blank");
      node.setAttribute("rel", "noopener noreferrer");
    }
  });

  const renderMarkdown = (text: string): string => {
    const html = marked.parse(text, { async: false, gfm: true }) as string;

    return DOMPurify.sanitize(html, { USE_PROFILES: { html: true }, ADD_ATTR: ["target"] });
  };

  onMount(() => {
    contentType = $storeInfo.home?.type ?? "";
    switch (contentType.toUpperCase()) {
      case "HTML":
        content = $storeInfo.home?.content ?? "";
        break;
      case "MARKDOWN":
        content = renderMarkdown($storeInfo.home?.content ?? "");
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
