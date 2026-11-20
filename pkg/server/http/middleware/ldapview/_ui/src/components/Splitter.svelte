<script lang="ts">
  let {
    size = $bindable(),
    min,
    max,
    reverse = false,
    label,
  }: { size: number; min: number; max: number; reverse?: boolean; label: string } = $props();

  let dragging = $state(false);
  $effect(() => {
    document.body.classList.toggle("select-none", dragging);
    document.body.classList.toggle("cursor-col-resize", dragging);
  });

  const clamp = (v: number) => Math.round(Math.min(max, Math.max(min, v)));

  function down(e: PointerEvent) {
    e.preventDefault();
    const el = e.currentTarget as HTMLElement;
    el.setPointerCapture(e.pointerId);
    const startX = e.clientX;
    const start = size;
    dragging = true;

    const move = (ev: PointerEvent) => {
      const dx = ev.clientX - startX;
      size = clamp(reverse ? start - dx : start + dx);
    };
    const up = () => {
      dragging = false;
      el.removeEventListener("pointermove", move);
      el.removeEventListener("pointerup", up);
      el.removeEventListener("pointercancel", up);
    };
    el.addEventListener("pointermove", move);
    el.addEventListener("pointerup", up);
    el.addEventListener("pointercancel", up);
  }

  function key(e: KeyboardEvent) {
    const step = e.shiftKey ? 48 : 16;
    if (e.key === "ArrowLeft") size = clamp(size + (reverse ? step : -step));
    else if (e.key === "ArrowRight") size = clamp(size + (reverse ? -step : step));
    else return;
    e.preventDefault();
  }
</script>

<!-- a focusable separator is the ARIA window splitter pattern -->
<!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
<div
  role="separator"
  aria-orientation="vertical"
  aria-label={label}
  aria-valuenow={size}
  aria-valuemin={min}
  aria-valuemax={max}
  tabindex="0"
  class="group relative z-10 w-px shrink-0 cursor-col-resize bg-line"
  onpointerdown={down}
  onkeydown={key}
  ondblclick={() => (size = clamp((min + max) / 3))}
>
  <span class="absolute inset-y-0 -right-1.5 -left-1.5 transition-colors group-hover:bg-accent/25 group-focus-visible:bg-accent/25 {dragging ? 'bg-accent/40' : ''}"></span>
</div>
