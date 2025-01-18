<script lang="ts">
  export let params: Record<string, string> | undefined = {};

  let iframeElement: HTMLIFrameElement;

  let msg = "Loading...";

  const modifyIframeContent = () => {
    msg = "";

    if (!iframeElement) return;

    const contentDocument: Document | undefined =
      iframeElement.contentDocument || iframeElement.contentWindow?.document;
    const headerElement: HTMLElement | null | undefined =
      contentDocument?.querySelector(".heading");

    if (headerElement) {
      headerElement.style.padding = "4px 24px";
    }
  };
</script>

<div class={!!msg ? "" : "hidden"}>
  <p class={`block py-1 px-2 text-white bg-blue-500`}>
    {msg}
  </p>
</div>

<iframe
  bind:this={iframeElement}
  class="w-full h-full"
  src="./grpc/{params?.wild}/"
  title={params?.wild}
  on:load={modifyIframeContent}
/>
