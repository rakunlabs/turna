<script lang="ts">
  export let params: Record<string, string> | undefined = {};

  let iframeElement: HTMLIFrameElement;

  const modifyIframeContent = () => {
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

<iframe
  bind:this={iframeElement}
  class="w-full h-full"
  src="./grpc/{params?.name}/"
  title={params?.name}
  on:load={modifyIframeContent}
/>
