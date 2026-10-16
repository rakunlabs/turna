<script lang="ts">
  import { onMount } from "svelte";
  import { HardDrive, ChevronRight, Sun, Moon, Monitor, FilePlus, FolderPlus, RefreshCw, Lock, LoaderCircle, PanelLeftClose, PanelLeftOpen } from "@lucide/svelte";
  import { api, ApiError } from "./lib/api";
  import { app, router } from "./lib/state.svelte";
  import { newFile, newFolder, leaveDirty, dragData, moveInto, collectDrop, upload, DRAG_TYPE } from "./lib/actions";
  import { parentOf } from "./lib/format";
  import Tree from "./components/Tree.svelte";
  import FolderView from "./components/FolderView.svelte";
  import FileView from "./components/FileView.svelte";
  import Dialog from "./components/Dialog.svelte";
  import Toasts from "./components/Toasts.svelte";

  type Kind = "dir" | "file" | "missing" | "loading";

  let kind = $state<Kind>("loading");
  let ready = $state(false);
  let fatal = $state<string | null>(null);
  let rootDrop = $state(false);
  let navOpen = $state(false);
  let theme = $state<"light" | "dark" | "system">(
    (localStorage.getItem("turna-fm-theme") as "light" | "dark" | null) ?? "system",
  );

  const NAV_DEFAULT = 264;
  const NAV_MIN = 180;
  const NAV_MAX = 560;
  const clampNav = (w: number) => Math.round(Math.min(NAV_MAX, Math.max(NAV_MIN, w)));
  let navWidth = $state(clampNav(Number(localStorage.getItem("turna-fm-nav-width")) || NAV_DEFAULT));
  let resizing = $state(false);

  $effect(() => {
    localStorage.setItem("turna-fm-nav-width", String(navWidth));
  });

  // Desktop: the sidebar is part of the layout and can be hidden. Mobile: it is an overlay drawer.
  let navHidden = $state(localStorage.getItem("turna-fm-nav-hidden") === "1");
  let mobile = $state(matchMedia("(max-width: 767px)").matches);

  $effect(() => {
    localStorage.setItem("turna-fm-nav-hidden", navHidden ? "1" : "0");
  });

  $effect(() => {
    const mq = matchMedia("(max-width: 767px)");
    const update = () => (mobile = mq.matches);
    mq.addEventListener("change", update);
    return () => mq.removeEventListener("change", update);
  });

  const navVisible = $derived(mobile ? navOpen : !navHidden);

  function toggleNav() {
    if (mobile) navOpen = !navOpen;
    else navHidden = !navHidden;
  }

  function globalKey(e: KeyboardEvent) {
    if ((e.ctrlKey || e.metaKey) && !e.shiftKey && !e.altKey && e.key.toLowerCase() === "b") {
      e.preventDefault();
      toggleNav();
    }
  }

  function startResize(e: PointerEvent) {
    if (e.button !== 0) return;
    e.preventDefault();
    const handle = e.currentTarget as HTMLElement;
    handle.setPointerCapture(e.pointerId);
    const startX = e.clientX;
    const startW = navWidth;
    resizing = true;
    const move = (ev: PointerEvent) => (navWidth = clampNav(startW + ev.clientX - startX));
    const end = () => {
      resizing = false;
      handle.removeEventListener("pointermove", move);
      handle.removeEventListener("pointerup", end);
      handle.removeEventListener("pointercancel", end);
    };
    handle.addEventListener("pointermove", move);
    handle.addEventListener("pointerup", end);
    handle.addEventListener("pointercancel", end);
  }

  function resizeKey(e: KeyboardEvent) {
    const step = e.shiftKey ? 48 : 16;
    if (e.key === "ArrowLeft") navWidth = clampNav(navWidth - step);
    else if (e.key === "ArrowRight") navWidth = clampNav(navWidth + step);
    else if (e.key === "Home") navWidth = NAV_MIN;
    else if (e.key === "End") navWidth = NAV_MAX;
    else return;
    e.preventDefault();
  }

  const crumbs = $derived.by(() => {
    const parts = router.path.split("/").filter(Boolean);
    return parts.map((name, i) => ({ name, path: "/" + parts.slice(0, i + 1).join("/") }));
  });

  const currentDir = $derived(kind === "dir" ? router.path : parentOf(router.path));
  const title = $derived(app.info.title || "Files");

  onMount(async () => {
    try {
      app.info = await api.info();
      await app.loadDir("/");
      ready = true;
    } catch (e) {
      fatal = e instanceof Error ? e.message : String(e);
    }
  });

  $effect(() => {
    document.title = router.path === "/" ? title : `${crumbs[crumbs.length - 1]?.name} · ${title}`;
  });

  $effect(() => {
    const p = router.path;
    navOpen = false;
    if (!ready) return;
    app.expandTo(parentOf(p));
    if (p === "/") {
      kind = "dir";
      return;
    }
    kind = "loading";
    api
      .stat(p)
      .then((s) => {
        if (router.path !== p) return;
        kind = s.dir ? "dir" : "file";
        if (s.dir) app.expanded[p] = true;
      })
      .catch((e) => {
        if (router.path !== p) return;
        kind = "missing";
        if (!(e instanceof ApiError && e.status === 404)) app.error(e);
      });
  });

  $effect(() => {
    const mq = matchMedia("(prefers-color-scheme: dark)");
    const apply = () => {
      const dark = theme === "dark" || (theme === "system" && mq.matches);
      document.documentElement.dataset.theme = dark ? "dark" : "light";
    };
    apply();
    if (theme === "system") localStorage.removeItem("turna-fm-theme");
    else localStorage.setItem("turna-fm-theme", theme);
    mq.addEventListener("change", apply);
    return () => mq.removeEventListener("change", apply);
  });

  function cycleTheme() {
    theme = theme === "system" ? "light" : theme === "light" ? "dark" : "system";
  }

  async function go(path: string) {
    if (path === router.path) return;
    if (!(await leaveDirty())) return;
    router.go(path);
  }

  async function reload() {
    for (const k of Object.keys(app.dirs)) delete app.dirs[k];
    await app.loadDir("/").catch((e) => app.error(e));
    app.version++;
  }
</script>

<svelte:window onkeydown={globalKey} />

<div class={["flex h-full min-h-0", resizing && "cursor-col-resize select-none"]}>
  {#if navOpen}
    <button class="fixed inset-0 z-20 bg-black/30 md:hidden" onclick={() => (navOpen = false)} aria-label="Close folders"></button>
  {/if}
  <aside
    id="folders"
    class={[
      "relative flex shrink-0 flex-col border-r border-line bg-canvas md:w-[var(--nav-w)]",
      navHidden && "md:hidden",
      "max-md:fixed max-md:inset-y-0 max-md:left-0 max-md:z-30 max-md:w-[264px] max-md:shadow-[var(--shadow)] max-md:transition-transform max-md:duration-200 max-md:ease-out",
      !navOpen && "max-md:-translate-x-full max-md:invisible",
    ]}
    style:--nav-w="{navWidth}px"
    aria-label="Folders"
  >
    <!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
    <div
      role="separator"
      aria-orientation="vertical"
      aria-label="Resize folders"
      aria-controls="folders"
      aria-valuenow={navWidth}
      aria-valuemin={NAV_MIN}
      aria-valuemax={NAV_MAX}
      tabindex="0"
      title="Drag to resize, double-click to reset"
      class={["resize-handle absolute inset-y-0 -right-[3px] z-10 w-[6px] cursor-col-resize max-md:hidden", resizing && "active"]}
      onpointerdown={startResize}
      ondblclick={() => (navWidth = NAV_DEFAULT)}
      onkeydown={resizeKey}
    ></div>
    <div class="flex h-10 shrink-0 items-center gap-2 px-4">
      <div class="flex size-5 items-center justify-center rounded-md bg-accent text-accent-ink">
        <HardDrive class="size-3" strokeWidth={2.25} />
      </div>
      <div class="flex min-w-0 items-baseline gap-2 leading-none">
        <p class="truncate text-[13px] font-semibold tracking-[-0.01em]">{title}</p>
        <p class="truncate font-mono text-[11px] text-faint">{app.info.prefix || "/"}</p>
      </div>
    </div>

    <div class="flex h-8 shrink-0 items-center px-3 pl-4">
      <span class="text-[12px] font-medium text-muted">Folders</span>
      <div class="ml-auto flex gap-0.5">
        {#if !app.info.read_only}
          <button class="icon-btn" onclick={() => newFile(currentDir)} aria-label="New file" title="New file in {currentDir}">
            <FilePlus class="size-3.5" />
          </button>
          <button class="icon-btn" onclick={() => newFolder(currentDir)} aria-label="New folder" title="New folder in {currentDir}">
            <FolderPlus class="size-3.5" />
          </button>
        {/if}
        <button class="icon-btn" onclick={reload} aria-label="Reload" title="Reload">
          <RefreshCw class="size-3.5" />
        </button>
      </div>
    </div>

    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <nav
      class={["min-h-0 flex-1 overflow-y-auto px-2 pb-4", rootDrop && "ring-2 ring-accent ring-inset"]}
      ondragover={(ev) => {
        if (app.info.read_only) return;
        const t = ev.dataTransfer?.types ?? [];
        if (!t.includes(DRAG_TYPE) && !t.includes("Files")) return;
        ev.preventDefault();
        rootDrop = true;
      }}
      ondragleave={(ev) => {
        if (!(ev.currentTarget as HTMLElement).contains(ev.relatedTarget as Node)) rootDrop = false;
      }}
      ondrop={async (ev) => {
        ev.preventDefault();
        rootDrop = false;
        if (!ev.dataTransfer) return;
        const moved = dragData(ev);
        if (moved) await moveInto(moved, "/");
        else await upload("/", await collectDrop(ev.dataTransfer));
      }}
    >
      <button
        class={[
          "mb-0.5 flex h-7 w-full items-center gap-1.5 rounded-md px-2 text-left text-[13px] transition-colors duration-100",
          router.path === "/" ? "bg-accent-soft font-medium text-accent-text" : "hover:bg-hover",
        ]}
        onclick={() => go("/")}
      >
        <HardDrive class="size-4 text-muted" strokeWidth={1.75} />
        <span>All files</span>
      </button>
      {#if ready}
        <Tree dir="/" />
      {/if}
    </nav>

  </aside>

  <main class="flex min-w-0 flex-1 flex-col bg-surface">
    <header class="flex h-10 shrink-0 items-center gap-1 border-b border-line px-3">
      <button
        class="icon-btn -ml-1 shrink-0"
        onclick={toggleNav}
        aria-label={navVisible ? "Hide folders" : "Show folders"}
        aria-controls="folders"
        aria-expanded={navVisible}
        title="{navVisible ? 'Hide' : 'Show'} folders (Ctrl B)"
      >
        {#if navVisible}<PanelLeftClose class="size-4" />{:else}<PanelLeftOpen class="size-4" />{/if}
      </button>
      <span class="mx-1 h-4 w-px shrink-0 bg-line" aria-hidden="true"></span>
      <nav aria-label="Breadcrumb" class="flex min-w-0 items-center gap-0.5 text-[13px]">
        <button class="shrink-0 rounded-md px-1.5 py-0.5 text-muted hover:bg-hover hover:text-ink" onclick={() => go("/")}>
          {title}
        </button>
        {#each crumbs as c, i (c.path)}
          <ChevronRight class="size-3.5 shrink-0 text-faint" />
          {#if i === crumbs.length - 1}
            <span class="truncate px-1.5 py-0.5 font-medium" aria-current="page">
              {c.name}{#if kind === "file" && app.dirty}<span class="ml-1.5 inline-block size-1.5 rounded-full bg-accent align-middle" aria-label="Unsaved"></span>{/if}
            </span>
          {:else}
            <button class="truncate rounded-md px-1.5 py-0.5 text-muted hover:bg-hover hover:text-ink" onclick={() => go(c.path)}>{c.name}</button>
          {/if}
        {/each}
      </nav>
      <div class="ml-auto flex shrink-0 items-center gap-1 pl-3 text-[12px] text-muted">
        {#if app.info.read_only}
          <span class="flex items-center gap-1.5 px-1.5" title="Files can be viewed but not changed">
            <Lock class="size-3.5" /><span class="max-sm:hidden">Read-only</span>
          </span>
        {:else}
          <span class="flex items-center gap-1.5 px-1.5" title="Files can be created, edited, moved and deleted">
            <span class="size-1.5 bg-ok"></span><span class="max-sm:hidden">Writable</span>
          </span>
        {/if}
        <span class="mx-0.5 h-4 w-px bg-line" aria-hidden="true"></span>
        <button class="icon-btn -mr-1" onclick={cycleTheme} aria-label="Theme: {theme}" title="Theme: {theme}">
          {#if theme === "light"}<Sun class="size-3.5" />{:else if theme === "dark"}<Moon class="size-3.5" />{:else}<Monitor class="size-3.5" />{/if}
        </button>
      </div>
    </header>

    {#if fatal}
      <div class="flex flex-1 flex-col items-start gap-2 p-8">
        <p class="font-medium">The file manager could not start</p>
        <p class="text-muted">{fatal}</p>
        <button class="btn mt-1" onclick={() => location.reload()}>Try again</button>
      </div>
    {:else if !ready || kind === "loading"}
      <div class="flex items-center gap-2 p-6 text-muted"><LoaderCircle class="size-4 animate-spin" /> Loading</div>
    {:else if kind === "dir"}
      {#key router.path}
        <FolderView path={router.path} />
      {/key}
    {:else if kind === "file"}
      {#key router.path}
        <FileView path={router.path} />
      {/key}
    {:else}
      <div class="flex flex-1 flex-col items-start gap-2 p-8">
        <p class="font-medium">Nothing at {router.path}</p>
        <p class="text-muted">It may have been moved or deleted.</p>
        <button class="btn mt-1" onclick={() => router.go(parentOf(router.path))}>Go to parent folder</button>
      </div>
    {/if}
  </main>
</div>

<Dialog />
<Toasts />

<style>
  .resize-handle::after {
    content: "";
    position: absolute;
    inset-block: 0;
    left: 2px;
    width: 2px;
    background: transparent;
    transition: background-color 120ms var(--ease-out);
  }
  .resize-handle:hover::after,
  .resize-handle:focus-visible::after,
  .resize-handle.active::after {
    background: var(--accent);
  }
  .resize-handle:focus-visible {
    outline: none;
  }
</style>
