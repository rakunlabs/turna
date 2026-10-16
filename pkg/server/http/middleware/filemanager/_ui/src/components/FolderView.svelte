<script lang="ts">
  import { Folder, FolderPlus, FilePlus, Upload, ArrowRightLeft, Trash2, Download, LoaderCircle, FolderOpen } from "@lucide/svelte";
  import type { Entry } from "../lib/api";
  import { api } from "../lib/api";
  import { app, router } from "../lib/state.svelte";
  import { fileIcon } from "../lib/icons";
  import { formatRelative, formatDate, formatSize, kindLabel, parentOf } from "../lib/format";
  import { newFile, newFolder, move, remove, upload, collectDrop, dragData, moveInto, DRAG_TYPE } from "../lib/actions";

  let { path }: { path: string } = $props();

  let error = $state<string | null>(null);
  let filter = $state("");
  let sortKey = $state<"name" | "size" | "mod_time">("name");
  let sortAsc = $state(true);
  let dragOver = $state(false);
  let fileInput = $state<HTMLInputElement>();
  let now = $state(Date.now());

  const entries = $derived(app.dirs[path]);

  $effect(() => {
    error = null;
    filter = "";
    app.loadDir(path).catch((e) => (error = e.message));
  });

  $effect(() => {
    const t = setInterval(() => (now = Date.now()), 30_000);
    return () => clearInterval(t);
  });

  const rows = $derived.by(() => {
    if (!entries) return [];
    const q = filter.trim().toLowerCase();
    const list = q ? entries.filter((e) => e.name.toLowerCase().includes(q)) : [...entries];
    const dir = sortAsc ? 1 : -1;
    return list.sort((a, b) => {
      if (a.dir !== b.dir) return a.dir ? -1 : 1;
      if (sortKey === "size") return (a.size - b.size) * dir;
      if (sortKey === "mod_time") return (new Date(a.mod_time).getTime() - new Date(b.mod_time).getTime()) * dir;
      return a.name.localeCompare(b.name, undefined, { numeric: true, sensitivity: "base" }) * dir;
    });
  });

  const counts = $derived.by(() => {
    const list = entries ?? [];
    const dirs = list.filter((e) => e.dir).length;
    return { dirs, files: list.length - dirs, bytes: list.reduce((n, e) => n + (e.dir ? 0 : e.size), 0) };
  });

  function sortBy(k: typeof sortKey) {
    if (sortKey === k) sortAsc = !sortAsc;
    else {
      sortKey = k;
      sortAsc = k === "name";
    }
  }

  function open(e: Entry) {
    if (e.dir) app.expanded[e.path] = true;
    router.go(e.path);
  }

  async function onDrop(ev: DragEvent) {
    ev.preventDefault();
    dragOver = false;
    if (app.info.read_only || !ev.dataTransfer) return;
    const moved = dragData(ev);
    if (moved) return moveInto(moved, path);
    await upload(path, await collectDrop(ev.dataTransfer));
  }

  async function onPick() {
    const files = [...(fileInput?.files ?? [])];
    if (fileInput) fileInput.value = "";
    await upload(
      path,
      files.map((file) => ({ file, name: file.name })),
    );
  }

  const sortLabel = (k: typeof sortKey) => (sortKey === k ? (sortAsc ? "ascending" : "descending") : "none");
</script>

<section
  class="relative flex min-h-0 flex-1 flex-col"
  aria-label="Folder contents"
  ondragover={(ev) => {
    if (app.info.read_only) return;
    const types = ev.dataTransfer?.types ?? [];
    if (!types.includes("Files") && !types.includes(DRAG_TYPE)) return;
    ev.preventDefault();
    dragOver = true;
  }}
  ondragleave={(ev) => {
    if (!(ev.currentTarget as HTMLElement).contains(ev.relatedTarget as Node)) dragOver = false;
  }}
  ondrop={onDrop}
>
  <div class="flex h-12 shrink-0 items-center gap-2 border-b border-line px-4">
    <input class="input h-7 max-w-[260px]" placeholder="Filter this folder" bind:value={filter} aria-label="Filter this folder" />
    <span class="tnum ml-1 text-[12px] whitespace-nowrap text-faint">
      {counts.dirs} folder{counts.dirs === 1 ? "" : "s"} · {counts.files} file{counts.files === 1 ? "" : "s"} · {formatSize(counts.bytes)}
    </span>
    <div class="ml-auto flex items-center gap-1.5">
      {#if !app.info.read_only}
        <input bind:this={fileInput} type="file" multiple class="hidden" onchange={onPick} />
        <button class="btn" onclick={() => fileInput?.click()}><Upload class="size-3.5" /> Upload</button>
        <button class="btn" onclick={() => newFolder(path)}><FolderPlus class="size-3.5" /> New folder</button>
        <button class="btn btn-primary" onclick={() => newFile(path)}><FilePlus class="size-3.5" /> New file</button>
      {/if}
    </div>
  </div>

  <div class="min-h-0 flex-1 overflow-auto">
    {#if error}
      <div class="flex flex-col items-start gap-2 p-8">
        <p class="font-medium">Could not open this folder</p>
        <p class="text-muted">{error}</p>
        <button class="btn mt-1" onclick={() => router.go(parentOf(path))}>Go to parent folder</button>
      </div>
    {:else if !entries}
      <div class="flex items-center gap-2 p-8 text-muted"><LoaderCircle class="size-4 animate-spin" /> Loading</div>
    {:else if entries.length === 0}
      <div class="flex h-full flex-col items-center justify-center gap-3 p-8 text-center">
        <FolderOpen class="size-8 text-faint" strokeWidth={1.5} />
        <div>
          <p class="font-medium">This folder is empty</p>
          <p class="mt-0.5 text-muted">
            {app.info.read_only ? "Nothing has been placed here yet." : "Create a file, or drop files and folders here to upload them."}
          </p>
        </div>
        {#if !app.info.read_only}
          <div class="mt-1 flex gap-2">
            <button class="btn" onclick={() => newFolder(path)}><FolderPlus class="size-3.5" /> New folder</button>
            <button class="btn btn-primary" onclick={() => newFile(path)}><FilePlus class="size-3.5" /> New file</button>
          </div>
        {/if}
      </div>
    {:else}
      <table class="w-full border-collapse text-left">
        <thead class="sticky top-0 z-[1] bg-surface">
          <tr class="h-9 border-b border-line text-[12px] text-muted">
            <th class="pl-4 font-medium" aria-sort={sortLabel("name")}>
              <button class="hover:text-ink" onclick={() => sortBy("name")}>Name{sortKey === "name" ? (sortAsc ? " ↑" : " ↓") : ""}</button>
            </th>
            <th class="w-28 font-medium">Type</th>
            <th class="w-24 pr-4 text-right font-medium" aria-sort={sortLabel("size")}>
              <button class="hover:text-ink" onclick={() => sortBy("size")}>Size{sortKey === "size" ? (sortAsc ? " ↑" : " ↓") : ""}</button>
            </th>
            <th class="w-36 font-medium" aria-sort={sortLabel("mod_time")}>
              <button class="hover:text-ink" onclick={() => sortBy("mod_time")}>Modified{sortKey === "mod_time" ? (sortAsc ? " ↑" : " ↓") : ""}</button>
            </th>
            <th class="w-28 pr-3"><span class="sr-only">Actions</span></th>
          </tr>
        </thead>
        <tbody>
          {#each rows as e (e.path)}
            <tr
              class="group h-9 cursor-pointer border-b border-line/70 transition-colors duration-100 hover:bg-hover"
              onclick={() => open(e)}
              draggable={!app.info.read_only}
              ondragstart={(ev) => {
                ev.dataTransfer?.setData(DRAG_TYPE, JSON.stringify({ path: e.path, dir: e.dir }));
                ev.dataTransfer?.setData("text/plain", app.rawURL(e.path));
              }}
              ondragover={(ev) => {
                if (!e.dir || app.info.read_only) return;
                ev.preventDefault();
                ev.stopPropagation();
              }}
              ondrop={async (ev) => {
                if (!e.dir) return;
                ev.stopPropagation();
                ev.preventDefault();
                dragOver = false;
                const moved = dragData(ev);
                if (moved) await moveInto(moved, e.path);
                else if (ev.dataTransfer) await upload(e.path, await collectDrop(ev.dataTransfer));
              }}
            >
              <td class="max-w-0 pl-4">
                <a
                  href="#{encodeURI(e.path)}"
                  class="flex min-w-0 items-center gap-2 outline-offset-2"
                  onclick={(ev) => ev.stopPropagation()}
                >
                  {#if e.dir}
                    <Folder class="size-4 shrink-0 text-muted" strokeWidth={1.75} />
                  {:else}
                    {@const Icon = fileIcon(e.name)}
                    <Icon class="size-4 shrink-0 text-faint" strokeWidth={1.75} />
                  {/if}
                  <span class="truncate">{e.name}</span>
                </a>
              </td>
              <td class="text-muted">{e.dir ? "Folder" : kindLabel(e.name, e.type)}</td>
              <td class="tnum pr-4 text-right text-muted">{e.dir ? "" : formatSize(e.size)}</td>
              <td class="tnum text-muted" title={formatDate(e.mod_time)}>{formatRelative(e.mod_time, now)}</td>
              <td class="pr-3">
                <div class="flex justify-end gap-0.5 opacity-0 transition-opacity duration-100 group-focus-within:opacity-100 group-hover:opacity-100">
                  {#if !e.dir}
                    <a
                      class="icon-btn"
                      href={api.downloadURL(e.path)}
                      download={e.name}
                      onclick={(ev) => ev.stopPropagation()}
                      aria-label="Download {e.name}"
                      title="Download"
                    >
                      <Download class="size-3.5" />
                    </a>
                  {/if}
                  {#if !app.info.read_only}
                    <button
                      class="icon-btn"
                      onclick={(ev) => (ev.stopPropagation(), move(e))}
                      aria-label="Move or rename {e.name}"
                      title="Move or rename"
                    >
                      <ArrowRightLeft class="size-3.5" />
                    </button>
                    <button
                      class="icon-btn hover:!text-danger"
                      onclick={(ev) => (ev.stopPropagation(), remove(e))}
                      aria-label="Delete {e.name}"
                      title="Delete"
                    >
                      <Trash2 class="size-3.5" />
                    </button>
                  {/if}
                </div>
              </td>
            </tr>
          {:else}
            <tr><td colspan="5" class="p-8 text-center text-muted">No names match “{filter}”.</td></tr>
          {/each}
        </tbody>
      </table>
    {/if}
  </div>

  {#if dragOver}
    <div class="pointer-events-none absolute inset-2 flex items-center justify-center rounded-lg border-2 border-dashed border-accent bg-accent-soft/80">
      <p class="rounded-md bg-surface px-3 py-1.5 font-medium text-accent-text shadow-[var(--shadow)]">Drop to put into {path}</p>
    </div>
  {/if}
</section>
