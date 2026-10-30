<script lang="ts">
  import { onMount, untrack } from "svelte";
  import { Save, ArrowRightLeft, Trash2, Download, LoaderCircle, FileWarning, RotateCcw, Lock, WrapText, FileAudio, PanelRightClose, PanelRightOpen, Wand } from "@lucide/svelte";
  import { api, ApiError, type Entry } from "../lib/api";
  import { app, router } from "../lib/state.svelte";
  import { dialog } from "../lib/dialog.svelte";
  import { createEditor, detectLanguage, languageLabels, type EditorHandle } from "../lib/editor";
  import { diagnose, formatText, lintLanguage, type Problem } from "../lib/lint";
  import { decodeText, formatDate, formatRelative, formatSize, kindLabel, parentOf, baseName, previewKind, looksLikeText, binaryGroup, MAX_EDIT_SIZE, MAX_TABLE_SIZE, type Preview } from "../lib/format";
  import { renderMarkdown } from "../lib/markdown";
  import { move, remove } from "../lib/actions";
  import RawURL from "./RawURL.svelte";
  import CsvTable from "./CsvTable.svelte";
  import FontPreview from "./FontPreview.svelte";

  let { path }: { path: string } = $props();

  let host = $state<HTMLDivElement>();
  let editor: EditorHandle | undefined;

  let meta = $state<Entry | null>(null);
  let status = $state<"loading" | "text" | "large" | "unplayable" | "error" | Exclude<Preview, "text" | "csv" | "unknown">>("loading");
  let errorText = $state("");
  let etag = "";
  let lang = $state("text");
  let saving = $state(false);
  let problems = $state<Problem[]>([]);
  let cursor = $state({ line: 1, col: 1 });
  let lines = $state(0);
  let wrap = $state(false);
  let now = $state(Date.now());
  let formatting = $state(false);
  const errors = $derived(problems.filter((p) => p.severity === "error"));
  const warnings = $derived(problems.length - errors.length);
  const firstProblem = $derived(errors[0] ?? problems[0] ?? null);
  const lintLang = $derived(lintLanguage(lang, path));
  // Wide screens: the panel sits beside the editor and the choice is remembered.
  // Narrow screens: it is an overlay that starts closed.
  let detailsHidden = $state(localStorage.getItem("turna-fm-details-hidden") === "1");
  let narrow = $state(matchMedia("(max-width: 1023px)").matches);
  let showInfo = $state(false);
  const detailsVisible = $derived(narrow ? showInfo : !detailsHidden);

  $effect(() => {
    localStorage.setItem("turna-fm-details-hidden", detailsHidden ? "1" : "0");
  });

  $effect(() => {
    const mq = matchMedia("(max-width: 1023px)");
    const update = () => (narrow = mq.matches);
    mq.addEventListener("change", update);
    return () => mq.removeEventListener("change", update);
  });

  function toggleDetails() {
    if (narrow) showInfo = !showInfo;
    else detailsHidden = !detailsHidden;
  }
  let mode = $state<"edit" | "split" | "preview">("edit");
  let isCsv = $state(false);
  let csvView = $state<"table" | "text">("table");
  let liveText = $state("");
  let token = 0;

  const readOnly = $derived(app.info.read_only);
  const etagHelp: Record<string, string> = {
    weak: "from modified time and size",
    strong: "from modified time and size, downloads can resume",
    hash: "SHA-256 of the content, downloads can resume",
  };
  // a compress middleware in front may add W/ to the header but not to the stat body
  const sameTag = (a?: string, b?: string) => !!a && !!b && a.replace(/^W\//, "") === b.replace(/^W\//, "");
  const isMarkdown = $derived(status === "text" && lang === "markdown");
  const showTable = $derived(status === "text" && isCsv && csvView === "table");
  const showEditor = $derived(status === "text" && !showTable && (!isMarkdown || mode !== "preview"));
  const showPreview = $derived(isMarkdown && mode !== "edit");
  const html = $derived(showPreview ? renderMarkdown(liveText) : "");
  const binaryTitle = $derived(
    {
      archive: "Archive",
      document: "Document",
      spreadsheet: "Spreadsheet",
      presentation: "Presentation",
      executable: "Executable or library",
      database: "Database or data file",
      font: "Font",
      image: "Image in a format browsers cannot show",
      data: "Binary data",
    }[binaryGroup(path) ?? "data"],
  );

  onMount(() => {
    editor = createEditor(host!, {
      onChange: () => {
        if (!editor) return;
        app.dirty = editor.isDirty();
        lines = editor.view.state.doc.lines;
        // only previews need the whole text; plain editing never serialises the document
        if (isMarkdown || isCsv) schedulePreview();
      },
      onSave: () => save(),
      onFormat: () => format(),
      onCursor: (line, col) => {
        if (cursor.line !== line || cursor.col !== col) cursor = { line, col };
      },
      onProblems: (list) => (problems = list),
    });
    const t = setInterval(() => (now = Date.now()), 30_000);
    return () => {
      clearInterval(t);
      clearTimeout(pTimer);
      editor?.destroy();
      app.dirty = false;
    };
  });

  $effect(() => {
    editor?.setReadOnly(readOnly);
  });

  // Opening another path is a full load. Later bumps of app.version (saves, moves, reloads)
  // only refresh in place so the editor never flashes or loses its cursor.
  let loadedPath = "";
  $effect(() => {
    void app.version;
    const p = path;
    untrack(() => {
      if (p !== loadedPath || status === "loading" || status === "error") load(p);
      else if (status === "text") refreshInPlace(p);
      else refreshMeta(p);
    });
  });

  /** Previews (video, images, PDF) restart when reloaded, so only reload them when the file really changed. */
  async function refreshMeta(p: string) {
    const my = ++token;
    try {
      const stat = await api.stat(p);
      if (my !== token) return;
      const changed = !sameTag(meta?.etag, stat.etag) || meta?.size !== stat.size;
      meta = stat;
      if (changed) {
        loadedPath = "";
        load(p);
      }
    } catch (e) {
      if (my !== token) return;
      if (e instanceof ApiError && e.status === 404) {
        errorText = "This file no longer exists.";
        status = "error";
      }
    }
  }

  $effect(() => {
    editor?.setWrap(wrap);
  });

  $effect(() => {
    const l = lintLang;
    untrack(() => editor?.setLint(status === "text" ? l : null));
  });

  let pTimer: ReturnType<typeof setTimeout>;
  function schedulePreview() {
    clearTimeout(pTimer);
    pTimer = setTimeout(() => {
      if (editor) liveText = editor.getText();
    }, 150);
  }

  // switching to a preview shows what is in the editor now, not what was loaded
  $effect(() => {
    if (showPreview || showTable) untrack(() => editor && status === "text" && (liveText = editor.getText()));
  });

  async function load(p: string) {
    if (app.dirty) return;
    const my = ++token;
    status = "loading";
    problems = [];
    try {
      const stat = await api.stat(p);
      if (my !== token) return;
      meta = stat;
      etag = "";
      lang = detectLanguage(p);

      const detected = previewKind(p);
      isCsv = detected === "csv";
      if (isCsv) csvView = stat.size <= MAX_TABLE_SIZE ? "table" : "text";
      let kind: Exclude<Preview, "csv"> = detected === "csv" ? "text" : detected;
      if (kind === "unknown") {
        kind = stat.size === 0 || looksLikeText(await api.head(p)) ? "text" : "binary";
        if (my !== token) return;
      }

      if (kind !== "text") {
        editor?.setLint(null);
        loadedPath = p;
        status = kind;
        return;
      }
      if (stat.size > MAX_EDIT_SIZE) {
        editor?.setLint(null);
        loadedPath = p;
        status = "large";
        return;
      }

      const file = await api.read(p);
      if (my !== token) return;
      etag = file.etag;
      const text = decodeText(file.data);
      if (text === null) {
        editor?.setLint(null);
        loadedPath = p;
        status = "binary";
        return;
      }
      liveText = text;
      editor?.setLint(lintLanguage(lang, p));
      await editor?.setDoc(text, lang);
      if (my !== token) return;
      lines = editor?.view.state.doc.lines ?? 0;
      app.dirty = false;
      loadedPath = p;
      status = "text";
      if (lang === "markdown") mode = readOnly ? "preview" : "split";
    } catch (e) {
      if (my !== token) return;
      if (e instanceof ApiError && e.status === 404) {
        // folder paths are handled by the parent view; a vanished file goes back up
        errorText = "This file no longer exists.";
      } else {
        errorText = e instanceof Error ? e.message : String(e);
      }
      status = "error";
    }
  }

  /** Picks up changes made elsewhere without a loading state; edits in progress are never replaced. */
  async function refreshInPlace(p: string) {
    if (saving) return;
    const my = ++token;
    try {
      const stat = await api.stat(p);
      if (my !== token) return;
      meta = stat;
      if (app.dirty || saving) return;
      // the stat tag already says whether the content moved; skip the download when it did not
      if (etag && sameTag(stat.etag, etag)) return;
      const file = await api.readIfChanged(p, etag);
      if (my !== token || !file || app.dirty || saving) return;
      if (file.etag && sameTag(file.etag, etag)) return;
      const text = decodeText(file.data);
      if (text === null) return load(p);
      etag = file.etag;
      liveText = text;
      editor?.replaceText(text);
      editor?.markClean();
      app.dirty = false;
    } catch (e) {
      if (my !== token) return;
      if (e instanceof ApiError && e.status === 404) {
        // keep unsaved edits on screen; saving will ask and then recreate the file
        if (app.dirty) {
          app.toast("This file was deleted on disk. Save to recreate it.", "error");
          return;
        }
        errorText = "This file no longer exists.";
        status = "error";
      }
    }
  }

  async function save(force = false) {
    if (status !== "text" || readOnly || saving || !editor) return;
    if (!app.dirty && !force) return;
    // snapshot the document object: typing during the request must stay unsaved
    const doc = editor.view.state.doc;
    const text = doc.toString();
    if (!force) {
      const found = (await diagnose(lintLang, text)).filter((p) => p.severity === "error");
      if (found.length) {
        const ok = await dialog.confirm({
          title: `Save invalid ${languageLabels[lang]}?`,
          description: `${found[0].message}. Clients reading this file may fail to parse it.`,
          confirm: "Save anyway",
          danger: true,
        });
        if (!ok) return;
      }
    }
    saving = true;
    let retry = false;
    try {
      const res = await api.write(path, text, { etag: force ? undefined : etag });
      etag = res.etag;
      meta = { ...meta, ...res.entry, dir: false, etag: res.etag };
      editor.markCleanAt(doc);
      app.dirty = editor.isDirty();
      app.toast("Saved");
      // only the listing changes; the open file is already up to date
      app.refreshQuiet(parentOf(path)).catch(() => {});
    } catch (e) {
      if (e instanceof ApiError && e.status === 412) {
        saving = false;
        retry = await dialog.confirm({
          title: "File changed on disk",
          description: "Someone else saved this file after you opened it. Overwrite their version with yours?",
          confirm: "Overwrite",
          danger: true,
        });
      } else {
        app.error(e);
      }
    } finally {
      saving = false;
    }
    if (retry) return save(true);
  }

  async function revert() {
    if (!app.dirty) return;
    const ok = await dialog.confirm({
      title: "Discard changes?",
      description: "The editor returns to the last saved version.",
      confirm: "Discard",
      danger: true,
    });
    if (!ok) return;
    app.dirty = false;
    // fetch again and swap the text in place; Ctrl Z can still bring the edits back
    etag = "";
    refreshInPlace(path);
  }

  async function changeLang(next: string) {
    lang = next;
    await editor?.setLanguage(next);
  }

  async function format() {
    if (!lintLang || readOnly || !editor || formatting) return;
    formatting = true;
    try {
      editor.replaceText(await formatText(lintLang, editor.getText()));
    } catch (e) {
      app.toast(`Cannot format: ${(e as Error).message}`, "error");
    } finally {
      formatting = false;
    }
  }

  function jumpToProblem() {
    if (!editor || !firstProblem) return;
    const view = editor.view;
    view.dispatch({ selection: { anchor: firstProblem.from }, scrollIntoView: true });
    view.focus();
  }

  function beforeUnload(e: BeforeUnloadEvent) {
    if (app.dirty) e.preventDefault();
  }
</script>

<svelte:window onbeforeunload={beforeUnload} />

<div class="relative flex min-h-0 flex-1">
  <section class="flex min-w-0 flex-1 flex-col" aria-label="Editor">
    <div class="flex h-12 shrink-0 items-center gap-2 overflow-x-auto border-b border-line px-4">
      {#if status === "text"}
        {#if isCsv}
          <div class="flex h-7 items-center rounded-md border border-line-strong p-0.5" role="group" aria-label="View">
            {#each [["table", "Table"], ["text", "Text"]] as [k, label]}
              <button
                class={[
                  "h-full rounded-[4px] px-2 text-[12px] font-medium transition-colors duration-100",
                  csvView === k ? "bg-hover text-ink" : "text-muted hover:text-ink",
                ]}
                aria-pressed={csvView === k}
                onclick={() => (csvView = k as typeof csvView)}
              >
                {label}
              </button>
            {/each}
          </div>
        {/if}
        {#if isMarkdown}
          <div class="flex h-7 items-center rounded-md border border-line-strong p-0.5" role="group" aria-label="View">
            {#each [["edit", "Edit"], ["split", "Split"], ["preview", "Preview"]] as [k, label]}
              <button
                class={[
                  "h-full rounded-[4px] px-2 text-[12px] font-medium transition-colors duration-100",
                  mode === k ? "bg-hover text-ink" : "text-muted hover:text-ink",
                  k === "split" && "max-md:hidden",
                ]}
                aria-pressed={mode === k}
                onclick={() => (mode = k as typeof mode)}
              >
                {label}
              </button>
            {/each}
          </div>
        {/if}
        <select
          class={["input h-7 w-auto pr-7 text-[12px]", showTable && "hidden"]}
          value={lang}
          onchange={(e) => changeLang(e.currentTarget.value)}
          aria-label="Syntax"
        >
          {#each Object.entries(languageLabels) as [k, v]}
            <option value={k}>{v}</option>
          {/each}
        </select>
        {#if lintLang && !readOnly}
          <button class="btn h-7 text-[12px]" onclick={format} disabled={formatting} title="Format document (Shift Alt F)">
            <Wand class="size-3.5" /> Format
          </button>
        {/if}
        <button
          class={["icon-btn", wrap && "bg-hover text-ink", showTable && "hidden"]}
          onclick={() => (wrap = !wrap)}
          aria-pressed={wrap}
          aria-label="Wrap lines"
          title="Wrap lines"
        >
          <WrapText class="size-3.5" />
        </button>
      {/if}

      <div class="ml-auto flex items-center gap-1.5">
        {#if readOnly}
          <span class="flex items-center gap-1.5 text-[12px] text-muted"><Lock class="size-3.5" /> Read-only</span>
        {:else if status !== "loading" && status !== "error"}
          {#if app.dirty}
            <button class="btn btn-ghost" onclick={revert}><RotateCcw class="size-3.5" /> Discard</button>
          {/if}
          <button class="btn" onclick={() => move({ path, dir: false })} aria-label="Move or rename" title="Move or rename">
            <ArrowRightLeft class="size-3.5" /><span class="max-sm:hidden">Move</span>
          </button>
          <button class="btn hover:!text-danger" onclick={() => remove({ path, dir: false })} aria-label="Delete" title="Delete">
            <Trash2 class="size-3.5" /><span class="max-sm:hidden">Delete</span>
          </button>
          {#if status === "text"}
            <button class="btn btn-primary min-w-[76px]" onclick={() => save()} disabled={!app.dirty || saving}>
              {#if saving}<LoaderCircle class="size-3.5 animate-spin" />{:else}<Save class="size-3.5" />{/if}
              Save
            </button>
          {/if}
        {/if}
        <span class="mx-0.5 h-4 w-px shrink-0 bg-line" aria-hidden="true"></span>
        <button
          class="icon-btn -mr-1 shrink-0"
          onclick={toggleDetails}
          aria-label={detailsVisible ? "Hide details" : "Show details"}
          aria-controls="file-details"
          aria-expanded={detailsVisible}
          title={detailsVisible ? "Hide details" : "Show details"}
        >
          {#if detailsVisible}<PanelRightClose class="size-4" />{:else}<PanelRightOpen class="size-4" />{/if}
        </button>
      </div>
    </div>

    <div class="relative flex min-h-0 flex-1">
      <div class={["relative min-w-0 flex-1", !showEditor && "hidden"]}>
        <div bind:this={host} class="editor-host absolute inset-0"></div>
      </div>

      {#if showTable}
        <div class="relative min-w-0 flex-1">
          <CsvTable text={liveText} {path} />
        </div>
      {/if}

      {#if showPreview}
        <article
          class={["markdown min-w-0 flex-1 overflow-y-auto px-8 py-6", showEditor && "border-l border-line"]}
          aria-label="Markdown preview"
        >
          {@html html}
        </article>
      {/if}

      {#if status === "loading"}
        <div class="absolute inset-0 flex items-center gap-2 p-6 text-muted"><LoaderCircle class="size-4 animate-spin" /> Opening</div>
      {:else if status === "error"}
        <div class="absolute inset-0 flex flex-col items-start gap-2 p-8">
          <p class="font-medium">Could not open this file</p>
          <p class="text-muted">{errorText}</p>
          <button class="btn mt-1" onclick={() => router.go(parentOf(path))}>Go to parent folder</button>
        </div>
      {:else if status === "image"}
        <div class="checker absolute inset-0 flex items-center justify-center overflow-auto p-8">
          <img src={api.fileURL(path)} alt={path} class="max-h-full max-w-full shadow-[var(--shadow)]" />
        </div>
      {:else if status === "video"}
        <div class="absolute inset-0 flex items-center justify-center bg-black p-4">
          <!-- svelte-ignore a11y_media_has_caption -->
          <video src={api.fileURL(path)} controls preload="metadata" class="max-h-full max-w-full" onerror={() => (status = "unplayable")}></video>
        </div>
      {:else if status === "audio"}
        <div class="absolute inset-0 flex flex-col items-center justify-center gap-4 p-8">
          <FileAudio class="size-10 text-faint" strokeWidth={1.25} />
          <p class="max-w-full truncate font-medium">{baseName(path)}</p>
          <audio src={api.fileURL(path)} controls preload="metadata" class="w-[min(520px,100%)]" onerror={() => (status = "unplayable")}></audio>
        </div>
      {:else if status === "font"}
        <FontPreview url={api.fileURL(path)} name={baseName(path)} />
      {:else if status === "pdf"}
        <iframe src={api.fileURL(path)} title={baseName(path)} class="absolute inset-0 size-full border-0 bg-sunk"></iframe>
      {:else if status === "binary" || status === "large" || status === "unplayable"}
        <div class="absolute inset-0 flex flex-col items-center justify-center gap-3 p-8 text-center">
          <FileWarning class="size-8 text-faint" strokeWidth={1.5} />
          <div>
            {#if status === "large"}
              <p class="font-medium">Too large to edit here</p>
              <p class="mt-0.5 max-w-[440px] text-muted">
                Files over {formatSize(MAX_EDIT_SIZE)} are not opened in the editor. Download it, or read it through its raw URL.
              </p>
            {:else if status === "unplayable"}
              <p class="font-medium">This browser cannot play this file</p>
              <p class="mt-0.5 max-w-[440px] text-muted">The format or codec is not supported here. Download it to play it locally.</p>
            {:else}
              <p class="font-medium">{binaryTitle}</p>
              <p class="mt-0.5 text-muted">This file cannot be previewed or edited here. It is still served at its raw URL.</p>
            {/if}
          </div>
          <a class="btn mt-1" href={api.downloadURL(path)}><Download class="size-3.5" /> Download {meta ? formatSize(meta.size) : ""}</a>
        </div>
      {/if}
    </div>

    {#if status === "text" && !showTable}
      <div class="code-status tnum flex h-7 shrink-0 items-center gap-4 px-4 text-[12px]">
        {#if firstProblem}
          <button
            class={[
              "flex min-w-0 items-center gap-1.5 hover:underline",
              errors.length ? "text-[var(--code-error)]" : "text-[var(--code-accent)]",
            ]}
            title="{firstProblem.message} (Ctrl Shift M lists all)"
            onclick={jumpToProblem}
          >
            <span class={["size-1.5 shrink-0 rounded-full", errors.length ? "bg-[var(--code-error)]" : "bg-[var(--code-accent)]"]}></span>
            <span class="whitespace-nowrap">
              {#if errors.length}{errors.length} {errors.length === 1 ? "error" : "errors"}{/if}{#if errors.length && warnings}, {/if}{#if warnings}{warnings} {warnings === 1 ? "warning" : "warnings"}{/if}
            </span>
            <span class="truncate opacity-80 max-sm:hidden">· {firstProblem.message}</span>
          </button>
          {#if app.dirty}<span class="whitespace-nowrap text-[var(--code-accent)] max-md:hidden">Unsaved</span>{/if}
        {:else if app.dirty}
          <span class="flex items-center gap-1.5 text-[var(--code-accent)]"><span class="size-1.5 rounded-full bg-[var(--code-accent)]"></span>Unsaved changes</span>
        {:else}
          <span>Saved</span>
        {/if}
        <span class="ml-auto whitespace-nowrap max-sm:hidden">Ln {cursor.line}, Col {cursor.col}</span>
        <span class="whitespace-nowrap max-sm:hidden">{lines} lines</span>
        <span class="whitespace-nowrap">{languageLabels[lang]}</span>
        {#if !readOnly}<span class="hidden whitespace-nowrap lg:inline"><span class="code-kbd">Ctrl S</span> to save</span>{/if}
      </div>
    {/if}
  </section>

  <aside
    id="file-details"
    class={[
      "w-[300px] shrink-0 flex-col gap-6 overflow-y-auto border-l border-line bg-surface p-4",
      detailsVisible ? "flex" : "hidden",
      "max-lg:absolute max-lg:inset-y-0 max-lg:right-0 max-lg:z-10 max-lg:w-[min(320px,100%)] max-lg:shadow-[var(--shadow)]",
    ]}
    aria-label="File details"
  >
    {#if meta}
      <dl class="grid grid-cols-[88px_1fr] gap-y-2.5 text-[13px]">
        <dt class="text-muted">Size</dt>
        <dd class="tnum">{formatSize(meta.size)}</dd>
        <dt class="text-muted">Modified</dt>
        <dd class="tnum" title={formatDate(meta.mod_time)}>{formatRelative(meta.mod_time, now)}</dd>
        <dt class="text-muted">Type</dt>
        <dd class="truncate font-mono text-[12px] leading-5" title={meta.type}>{meta.type?.split(";")[0] || kindLabel(path)}</dd>
        {#if meta.etag_mode}
          <dt class="text-muted">ETag</dt>
          <dd class="flex min-w-0 flex-col gap-0.5">
            <span class="w-fit rounded border border-line-strong px-1 font-mono text-[11px] leading-4">{meta.etag_mode}</span>
            <span class="text-[12px] text-muted">{etagHelp[meta.etag_mode]}</span>
            {#if meta.etag}
              <span class="truncate font-mono text-[11px] text-faint" title={meta.etag}>{meta.etag}</span>
            {/if}
          </dd>
        {/if}
      </dl>
      <RawURL {path} type={meta.type} />
      <a class="btn w-full justify-center" href={api.downloadURL(path)}><Download class="size-3.5" /> Download</a>
    {/if}
  </aside>
</div>

<style>
  .editor-host :global(.cm-editor) {
    height: 100%;
  }
  .code-status {
    background: var(--code-bg);
    color: var(--code-keyword);
    border-top: 1px solid var(--code-divider);
  }
  .code-kbd {
    font-family: var(--font-mono);
    font-size: 11px;
    color: var(--code-keyword);
    border: 1px solid var(--code-divider-strong);
    border-radius: 4px;
    padding: 0 4px;
  }
  .markdown {
    font-size: 14px;
    line-height: 1.7;
  }
  .markdown > :global(*) {
    max-width: 72ch;
  }
  .markdown :global(h1),
  .markdown :global(h2),
  .markdown :global(h3),
  .markdown :global(h4) {
    font-weight: 600;
    line-height: 1.25;
    letter-spacing: -0.015em;
    margin: 1.6em 0 0.5em;
    text-wrap: balance;
  }
  .markdown :global(h1) {
    font-size: 1.75em;
  }
  .markdown :global(h2) {
    font-size: 1.35em;
    padding-bottom: 0.3em;
    border-bottom: 1px solid var(--line);
  }
  .markdown :global(h3) {
    font-size: 1.12em;
  }
  .markdown > :global(:first-child) {
    margin-top: 0;
  }
  .markdown :global(p),
  .markdown :global(ul),
  .markdown :global(ol),
  .markdown :global(pre),
  .markdown :global(table),
  .markdown :global(blockquote) {
    margin: 0 0 1em;
  }
  .markdown :global(ul) {
    list-style: disc;
    padding-left: 1.5em;
  }
  .markdown :global(ol) {
    list-style: decimal;
    padding-left: 1.5em;
  }
  .markdown :global(li + li) {
    margin-top: 0.25em;
  }
  .markdown :global(a) {
    color: var(--accent-text);
    text-decoration: underline;
    text-underline-offset: 2px;
  }
  .markdown :global(code) {
    font-family: var(--font-mono);
    font-size: 0.88em;
    background: var(--surface-sunk);
    border: 1px solid var(--line);
    border-radius: 4px;
    padding: 0.1em 0.35em;
  }
  .markdown :global(pre) {
    background: var(--code-bg);
    color: var(--code-text);
    border: 1px solid var(--code-divider);
    border-radius: 8px;
    padding: 12px 14px;
    overflow-x: auto;
  }
  .markdown :global(pre code) {
    background: none;
    border: 0;
    padding: 0;
    color: inherit;
  }
  .markdown :global(blockquote) {
    color: var(--text-muted);
    padding-left: 1em;
    border-left: 1px solid var(--line-strong);
  }
  .markdown :global(table) {
    border-collapse: collapse;
    font-variant-numeric: tabular-nums;
  }
  .markdown :global(th),
  .markdown :global(td) {
    border: 1px solid var(--line);
    padding: 6px 10px;
    text-align: left;
  }
  .markdown :global(th) {
    background: var(--surface-sunk);
    font-weight: 600;
  }
  .markdown :global(img) {
    max-width: 100%;
  }
  .markdown :global(hr) {
    border: 0;
    border-top: 1px solid var(--line);
    margin: 2em 0;
  }
  .checker {
    background-color: var(--surface-sunk);
    background-image:
      linear-gradient(45deg, var(--hover) 25%, transparent 25%),
      linear-gradient(-45deg, var(--hover) 25%, transparent 25%),
      linear-gradient(45deg, transparent 75%, var(--hover) 75%),
      linear-gradient(-45deg, transparent 75%, var(--hover) 75%);
    background-size: 16px 16px;
    background-position:
      0 0,
      0 8px,
      8px -8px,
      -8px 0;
  }
</style>
