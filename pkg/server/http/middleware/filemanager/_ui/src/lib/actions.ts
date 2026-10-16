import { api, ApiError, type Entry } from "./api";
import { dialog } from "./dialog.svelte";
import { app, router } from "./state.svelte";
import { baseName, joinPath, parentOf } from "./format";

function validName(v: string): string | null {
  if (!v) return "Enter a name";
  if (v.split("/").some((s) => s === ".." || s === ".")) return "Use a plain path without . or ..";
  return null;
}

function validPath(v: string): string | null {
  if (!v || v === "/") return "Enter a path";
  return validName(v.replace(/^\/+/, ""));
}

function normalize(v: string): string {
  return "/" + v.replace(/^\/+/, "").replace(/\/+$/, "").replace(/\/{2,}/g, "/");
}

export async function newFile(dir: string) {
  const name = await dialog.prompt({
    title: "New file",
    description: `Created in ${dir}. Include folders in the name to create them too.`,
    label: "File name",
    placeholder: "config.yaml",
    confirm: "Create file",
    validate: validName,
  });
  if (!name) return;
  const path = normalize(joinPath(dir, name));
  try {
    await api.write(path, "", { create: true });
    await app.refresh(parentOf(path));
    app.expandTo(parentOf(path));
    router.go(path);
  } catch (e) {
    app.error(e);
  }
}

export async function newFolder(dir: string) {
  const name = await dialog.prompt({
    title: "New folder",
    description: `Created in ${dir}.`,
    label: "Folder name",
    placeholder: "staging",
    confirm: "Create folder",
    validate: validName,
  });
  if (!name) return;
  const path = normalize(joinPath(dir, name));
  try {
    await api.mkdir(path);
    await app.refresh(parentOf(path));
    app.expandTo(path);
    router.go(path);
  } catch (e) {
    app.error(e);
  }
}

/** Rename and move are the same operation: edit the full path. */
export async function move(entry: Pick<Entry, "path" | "dir">) {
  const name = baseName(entry.path);
  const dot = entry.dir ? -1 : name.lastIndexOf(".");
  const start = entry.path.length - name.length;
  const end = dot > 0 ? start + dot : entry.path.length;
  const to = await dialog.prompt({
    title: entry.dir ? "Move or rename folder" : "Move or rename file",
    description: "Edit the path. Missing folders are created.",
    label: "New path",
    value: entry.path,
    select: [start, end],
    confirm: "Move",
    validate: validPath,
  });
  if (!to) return;
  await moveTo(entry, normalize(to));
}

export async function moveTo(entry: Pick<Entry, "path" | "dir">, to: string, overwrite = false): Promise<boolean> {
  if (to === entry.path) return false;
  if (!(await leaveDirty(entry.path))) return false;
  try {
    const res = await api.move(entry.path, to, overwrite);
    await app.refresh(entry.path, res.path);
    app.toast(`Moved to ${res.path}`);
    const cur = router.path;
    if (cur === entry.path || cur.startsWith(entry.path + "/")) {
      app.dirty = false;
      router.go(res.path + cur.slice(entry.path.length));
    }
    return true;
  } catch (e) {
    if (e instanceof ApiError && e.status === 409 && !entry.dir && !overwrite) {
      const ok = await dialog.confirm({
        title: "Replace existing file?",
        description: `${to} already exists. Moving will replace it.`,
        confirm: "Replace",
        danger: true,
      });
      if (ok) return moveTo(entry, to, true);
      return false;
    }
    app.error(e);
    return false;
  }
}

export async function moveInto(entry: Pick<Entry, "path" | "dir">, dir: string) {
  if (parentOf(entry.path) === dir) return;
  if (dir === entry.path || dir.startsWith(entry.path + "/")) {
    app.toast("A folder cannot be moved into itself", "error");
    return;
  }
  await moveTo(entry, joinPath(dir, baseName(entry.path)));
}

export async function remove(entry: Pick<Entry, "path" | "dir">) {
  const ok = await dialog.confirm({
    title: entry.dir ? "Delete folder?" : "Delete file?",
    description: entry.dir
      ? `${entry.path} and everything inside it will be permanently deleted.`
      : `${entry.path} will be permanently deleted.`,
    confirm: "Delete",
    danger: true,
  });
  if (!ok) return;
  try {
    await api.remove(entry.path);
    const cur = router.path;
    if (cur === entry.path || cur.startsWith(entry.path + "/")) {
      app.dirty = false;
      router.go(parentOf(entry.path));
    }
    await app.refresh(entry.path);
    app.toast(`Deleted ${baseName(entry.path)}`);
  } catch (e) {
    app.error(e);
  }
}

export type UploadItem = { file: File; name: string };

export async function upload(dir: string, items: UploadItem[]) {
  if (!items.length) return;
  const total = items.reduce((n, i) => n + i.file.size, 0);
  if (app.info.max_upload_size && total > app.info.max_upload_size) {
    app.toast(`Upload is larger than the ${Math.round(app.info.max_upload_size / 1048576)} MB limit`, "error");
    return;
  }
  const send = async (overwrite: boolean) => {
    const res = await api.upload(dir, items, overwrite);
    await app.refresh(dir);
    app.toast(res.saved.length === 1 ? `Uploaded ${baseName(res.saved[0])}` : `Uploaded ${res.saved.length} files`);
  };
  try {
    await send(false);
  } catch (e) {
    if (e instanceof ApiError && e.status === 409) {
      const ok = await dialog.confirm({
        title: "Replace existing files?",
        description: `${e.message}. Uploading will replace files with the same name.`,
        confirm: "Replace",
        danger: true,
      });
      if (ok) await send(true).catch((err) => app.error(err));
      return;
    }
    app.error(e);
  }
}

/** Collect files from a drop, keeping folder structure when folders are dropped. */
export async function collectDrop(dt: DataTransfer): Promise<UploadItem[]> {
  const entries = [...dt.items]
    .filter((i) => i.kind === "file")
    .map((i) => i.webkitGetAsEntry?.())
    .filter((e): e is FileSystemEntry => !!e);

  if (!entries.length) return [...dt.files].map((file) => ({ file, name: file.name }));

  const out: UploadItem[] = [];
  const walk = async (entry: FileSystemEntry, prefix: string): Promise<void> => {
    if (entry.isFile) {
      const file = await new Promise<File>((res, rej) => (entry as FileSystemFileEntry).file(res, rej));
      out.push({ file, name: prefix + entry.name });
    } else if (entry.isDirectory) {
      const reader = (entry as FileSystemDirectoryEntry).createReader();
      let batch: FileSystemEntry[];
      do {
        batch = await new Promise((res, rej) => reader.readEntries(res, rej));
        for (const child of batch) await walk(child, `${prefix}${entry.name}/`);
      } while (batch.length);
    }
  };
  for (const e of entries) await walk(e, "");
  return out;
}

export async function leaveDirty(target?: string): Promise<boolean> {
  if (!app.dirty) return true;
  const cur = router.path;
  if (target && !(cur === target || cur.startsWith(target + "/"))) return true;
  const ok = await dialog.confirm({
    title: "Discard unsaved changes?",
    description: `Changes to ${baseName(cur)} have not been saved.`,
    confirm: "Discard",
    danger: true,
  });
  if (ok) app.dirty = false;
  return ok;
}

export const DRAG_TYPE = "application/x-turna-entry";

export function dragData(e: DragEvent): Pick<Entry, "path" | "dir"> | null {
  const raw = e.dataTransfer?.getData(DRAG_TYPE);
  if (!raw) return null;
  try {
    return JSON.parse(raw);
  } catch {
    return null;
  }
}
