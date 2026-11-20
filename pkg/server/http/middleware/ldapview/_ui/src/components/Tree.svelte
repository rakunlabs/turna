<script lang="ts">
  import { ChevronRight, LoaderCircle, Folder, User, Users, Building2, FileText, Globe } from "@lucide/svelte";
  import Tree from "./Tree.svelte";
  import { app } from "../lib/state.svelte";
  import type { Child } from "../lib/api";

  let {
    node,
    depth = 0,
    selected,
    onselect,
  }: { node: Child; depth?: number; selected: string | null; onselect: (dn: string) => void } = $props();

  const open = $derived(!!app.expanded[node.dn]);
  const kids = $derived(app.children[node.dn]);
  const leaf = $derived(node.has_children === false || (kids !== undefined && kids.length === 0));

  const Icon = $derived.by(() => {
    const oc = (node.object_class ?? []).map((c) => c.toLowerCase());
    if (depth === 0) return Globe;
    if (oc.some((c) => ["person", "inetorgperson", "user", "posixaccount"].includes(c))) return User;
    if (oc.some((c) => c.includes("group"))) return Users;
    if (oc.includes("organization") || oc.includes("dcobject") || oc.includes("domain")) return Building2;
    if (oc.includes("organizationalunit") || oc.includes("container")) return Folder;
    return leaf ? FileText : Folder;
  });

  async function toggle(e?: MouseEvent) {
    e?.stopPropagation();
    if (leaf) return;
    const next = !open;
    app.expanded[node.dn] = next;
    if (next) await app.loadChildren(node.dn);
  }

  function key(e: KeyboardEvent) {
    if (e.key === "ArrowRight" && !open) toggle();
    else if (e.key === "ArrowLeft" && open) toggle();
  }
</script>

<div
  role="treeitem"
  aria-selected={selected === node.dn}
  aria-expanded={leaf ? undefined : open}
  tabindex="0"
  class="group flex h-7 cursor-pointer items-center gap-1 rounded-md pr-2 text-[13px] select-none
    {selected === node.dn ? 'bg-accent-soft text-accent-text' : 'hover:bg-hover'}"
  style="padding-left: {depth * 14 + 4}px"
  onclick={() => onselect(node.dn)}
  ondblclick={() => toggle()}
  onkeydown={(e) => {
    if (e.key === "Enter") onselect(node.dn);
    else key(e);
  }}
  title={node.dn}
>
  <button class="flex size-5 shrink-0 items-center justify-center rounded text-faint hover:text-ink" tabindex="-1" onclick={toggle} aria-label={open ? "Collapse" : "Expand"}>
    {#if app.loading[node.dn]}
      <LoaderCircle class="size-3.5 animate-spin" />
    {:else if !leaf}
      <ChevronRight class="size-3.5 transition-transform duration-150 {open ? 'rotate-90' : ''}" />
    {/if}
  </button>
  <Icon class="size-3.5 shrink-0 {selected === node.dn ? '' : 'text-muted'}" />
  <span class="truncate {depth === 0 ? 'font-mono text-[12px]' : ''}">{depth === 0 ? node.dn : node.rdn}</span>
</div>

{#if open && kids}
  <div role="group">
    {#each kids as c (c.dn)}
      <Tree node={c} depth={depth + 1} {selected} {onselect} />
    {/each}
  </div>
{/if}
