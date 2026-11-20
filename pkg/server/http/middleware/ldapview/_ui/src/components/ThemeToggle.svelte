<script lang="ts">
  import { Sun, Moon, Monitor } from "@lucide/svelte";
  import { theme } from "../lib/theme.svelte";

  let { class: cls = "" }: { class?: string } = $props();
</script>

<button type="button" class="icon-btn {cls}" onclick={() => theme.cycle()} aria-label="Theme: {theme.mode}" title="Theme: {theme.mode}">
  {#if theme.mode === "light"}<Sun class="size-4" />{:else if theme.mode === "dark"}<Moon class="size-4" />{:else}<Monitor class="size-4" />{/if}
</button>
