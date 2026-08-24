<script lang="ts">
  import { onDestroy, onMount, type Snippet } from "svelte";

  let {
    class: className = "",
    children,
  }: {
    class?: string;
    children?: Snippet;
  } = $props();

  const MIN_THUMB = 24;
  const HIDE_AFTER = 700;

  let scroller: HTMLDivElement;
  let thumbHeight = $state(0);
  let thumbTop = $state(0);
  let visible = $state(false);

  let hideTimer: ReturnType<typeof setTimeout>;
  let frame = 0;
  let resizeObserver: ResizeObserver | undefined;
  let mutationObserver: MutationObserver | undefined;

  const flash = () => {
    visible = true;
    clearTimeout(hideTimer);
    hideTimer = setTimeout(() => (visible = false), HIDE_AFTER);
  };

  // The native scrollbar is removed from the layout, so its geometry is
  // measured here and painted as an overlay that never shifts the content.
  const measure = () => {
    if (!scroller) return;

    const { scrollHeight, clientHeight, scrollTop } = scroller;

    if (scrollHeight <= clientHeight + 1) {
      thumbHeight = 0;

      return;
    }

    thumbHeight = Math.max(MIN_THUMB, (clientHeight / scrollHeight) * clientHeight);
    thumbTop = (scrollTop / (scrollHeight - clientHeight)) * (clientHeight - thumbHeight);
  };

  // Content and viewport changes arrive in bursts, so measuring is coalesced
  // into a single frame.
  const schedule = () => {
    cancelAnimationFrame(frame);

    frame = requestAnimationFrame(() => {
      const wasScrollable = thumbHeight > 0;
      measure();

      // Reveal the bar once when the area becomes scrollable, otherwise there
      // is nothing telling the user that more content is below.
      if (!wasScrollable && thumbHeight > 0) flash();
    });
  };

  const onScroll = () => {
    measure();
    flash();
  };

  onMount(() => {
    measure();
    if (thumbHeight > 0) flash();

    resizeObserver = new ResizeObserver(schedule);
    resizeObserver.observe(scroller);

    mutationObserver = new MutationObserver(schedule);
    mutationObserver.observe(scroller, { childList: true, subtree: true });

    window.addEventListener("resize", schedule);
  });

  onDestroy(() => {
    cancelAnimationFrame(frame);
    clearTimeout(hideTimer);
    resizeObserver?.disconnect();
    mutationObserver?.disconnect();
    window.removeEventListener("resize", schedule);
  });
</script>

<div class={`relative flex flex-col ${className}`}>
  <div
    bind:this={scroller}
    class="overlay-scroll min-h-0 flex-1 overflow-y-auto"
    onscroll={onScroll}
  >
    {@render children?.()}
  </div>

  {#if thumbHeight > 0}
    <div
      class:visible
      class="overlay-thumb"
      style={`height:${thumbHeight}px;transform:translateY(${thumbTop}px)`}
      aria-hidden="true"
    ></div>
  {/if}
</div>

<style>
  .overlay-scroll {
    scrollbar-width: none;
    -ms-overflow-style: none;
  }

  .overlay-scroll::-webkit-scrollbar {
    display: none;
  }

  .overlay-thumb {
    position: absolute;
    top: 0;
    right: 2px;
    width: 4px;
    border-radius: 2px;
    background: #9ca3af;
    opacity: 0;
    pointer-events: none;
    transition: opacity 180ms ease-out;
  }

  .overlay-thumb.visible {
    opacity: 0.9;
  }
</style>
