<script lang="ts">
  import { ChevronRight, Folder, FolderOpen, LoaderCircle } from "@lucide/svelte";
  import type { Entry } from "../lib/api";
  import { app, router } from "../lib/state.svelte";
  import { fileIcon } from "../lib/icons";
  import { DRAG_TYPE, dragData, moveInto, collectDrop, upload, leaveDirty } from "../lib/actions";
  import Tree from "./Tree.svelte";

  let { dir, depth = 0 }: { dir: string; depth?: number } = $props();

  const entries = $derived(app.dirs[dir]);

  $effect(() => {
    if (!app.dirs[dir]) app.loadDir(dir).catch((e) => app.error(e));
  });

  let dropTarget = $state<string | null>(null);

  function iconFor(e: Entry) {
    return fileIcon(e.name);
  }

  async function open(e: Entry) {
    if (e.dir) {
      app.expanded[e.path] = true;
    }
    if (router.path === e.path) return;
    if (!(await leaveDirty())) return;
    router.go(e.path);
  }

  function toggle(e: Entry, ev: MouseEvent) {
    ev.stopPropagation();
    app.expanded[e.path] = !app.expanded[e.path];
  }

  function onKey(e: Entry, ev: KeyboardEvent) {
    if (ev.key === "ArrowRight" && e.dir) {
      ev.preventDefault();
      app.expanded[e.path] = true;
    } else if (ev.key === "ArrowLeft" && e.dir) {
      ev.preventDefault();
      app.expanded[e.path] = false;
    } else if (ev.key === "ArrowDown" || ev.key === "ArrowUp") {
      ev.preventDefault();
      const rows = [...document.querySelectorAll<HTMLElement>("[data-tree-row]")];
      const i = rows.indexOf(ev.currentTarget as HTMLElement);
      rows[ev.key === "ArrowDown" ? i + 1 : i - 1]?.focus();
    }
  }

  function canDrop(ev: DragEvent) {
    return !app.info.read_only && (ev.dataTransfer?.types.includes(DRAG_TYPE) || ev.dataTransfer?.types.includes("Files"));
  }

  async function onDrop(target: string, ev: DragEvent) {
    ev.preventDefault();
    ev.stopPropagation();
    dropTarget = null;
    if (!ev.dataTransfer) return;
    const moved = dragData(ev);
    if (moved) {
      await moveInto(moved, target);
      return;
    }
    const items = await collectDrop(ev.dataTransfer);
    await upload(target, items);
    app.expanded[target] = true;
  }
</script>

{#if entries}
  <ul role={depth === 0 ? "tree" : "group"} aria-label={depth === 0 ? "Files" : undefined}>
    {#each entries as e (e.path)}
      {@const Icon = e.dir ? (app.expanded[e.path] ? FolderOpen : Folder) : iconFor(e)}
      {@const active = router.path === e.path}
      <li role="treeitem" aria-expanded={e.dir ? !!app.expanded[e.path] : undefined} aria-selected={active}>
        <!-- svelte-ignore a11y_no_static_element_interactions, a11y_no_noninteractive_tabindex -->
        <div
          data-tree-row
          tabindex="0"
          class={[
            "group flex h-7 cursor-pointer items-center gap-1.5 rounded-md pr-2 text-[13px] transition-colors duration-100 select-none",
            active ? "bg-accent-soft font-medium text-accent-text" : "text-ink hover:bg-hover",
            dropTarget === e.path && "ring-2 ring-accent ring-inset",
                      ]}
          style:padding-left="{8 + depth * 14}px"
          draggable={!app.info.read_only}
          ondragstart={(ev) => {
            ev.dataTransfer?.setData(DRAG_TYPE, JSON.stringify({ path: e.path, dir: e.dir }));
            ev.dataTransfer?.setData("text/plain", app.rawURL(e.path));
            if (ev.dataTransfer) ev.dataTransfer.effectAllowed = "move";
          }}
          ondragover={(ev) => {
            if (!e.dir || !canDrop(ev)) return;
            ev.preventDefault();
            ev.stopPropagation();
            dropTarget = e.path;
          }}
          ondragleave={() => dropTarget === e.path && (dropTarget = null)}
          ondrop={(ev) => e.dir && onDrop(e.path, ev)}
          onclick={() => open(e)}
          onkeydown={(ev) => (ev.key === "Enter" ? open(e) : onKey(e, ev))}
          title={e.path}
        >
          {#if e.dir}
            <button
              tabindex="-1"
              class="-ml-1 flex size-4 shrink-0 items-center justify-center rounded text-faint hover:text-ink"
              onclick={(ev) => toggle(e, ev)}
              aria-label={app.expanded[e.path] ? "Collapse" : "Expand"}
            >
              {#if app.loading[e.path]}
                <LoaderCircle class="size-3 animate-spin" />
              {:else}
                <ChevronRight class={["size-3.5 transition-transform duration-150", app.expanded[e.path] && "rotate-90"]} />
              {/if}
            </button>
          {:else}
            <span class="-ml-1 w-4 shrink-0"></span>
          {/if}
          <Icon class={["size-4 shrink-0", active ? "text-accent-text" : e.dir ? "text-muted" : "text-faint"]} strokeWidth={1.75} />
          <span class="min-w-0 flex-1 truncate">{e.name}</span>
          {#if active && !e.dir && app.dirty}
            <span class="size-1.5 shrink-0 rounded-full bg-accent" aria-label="Unsaved changes"></span>
          {/if}
        </div>
        {#if e.dir && app.expanded[e.path]}
          <Tree dir={e.path} depth={depth + 1} />
        {/if}
      </li>
    {:else}
      {#if depth > 0}
        <li class="h-7 text-[12px] leading-7 text-faint" style:padding-left="{30 + depth * 14}px">Empty folder</li>
      {/if}
    {/each}
  </ul>
{:else if depth > 0}
  <div class="h-7" style:padding-left="{30 + depth * 14}px"><LoaderCircle class="mt-1.5 size-3.5 animate-spin text-faint" /></div>
{/if}
