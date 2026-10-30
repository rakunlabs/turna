import { api, type Entry, type Info } from "./api";

type Toast = { id: number; kind: "ok" | "error"; text: string };

let toastId = 0;

class AppState {
  info = $state<Info>({ title: "", read_only: false, prefix: "", raw_path: "/raw", max_upload_size: 0 });
  /** Directory listings cache by path, used by both the tree and the folder view. */
  dirs = $state<Record<string, Entry[]>>({});
  expanded = $state<Record<string, boolean>>({ "/": true });
  loading = $state<Record<string, boolean>>({});
  toasts = $state<Toast[]>([]);
  dirty = $state(false);
  /** Bumped after mutations and manual reloads so open views can refresh. */
  version = $state(0);

  rawURL(path: string): string {
    return `${location.origin}${this.info.prefix}${this.info.raw_path}${encodeURI(path)}`;
  }

  async loadDir(path: string, force = false): Promise<Entry[]> {
    if (!force && this.dirs[path]) return this.dirs[path];
    this.loading[path] = true;
    try {
      const res = await api.list(path);
      const prev = this.dirs[path];
      // keep the same array when nothing changed so lists and the tree do not re-render
      if (!prev || !sameEntries(prev, res.entries)) this.dirs[path] = res.entries;
      return this.dirs[path];
    } finally {
      delete this.loading[path];
    }
  }

  /**
   * Reload every cached directory affected by a change at path.
   * notify bumps version so open views re-check their content; a save of the
   * open file does not need it, the editor already has the new text.
   */
  async refresh(...paths: string[]) {
    return this.refreshDirs(paths, true);
  }

  async refreshQuiet(...paths: string[]) {
    return this.refreshDirs(paths, false);
  }

  private async refreshDirs(paths: string[], notify: boolean) {
    const targets = new Set<string>();
    for (const p of paths) {
      targets.add(p);
      const parent = p.lastIndexOf("/") <= 0 ? "/" : p.slice(0, p.lastIndexOf("/"));
      targets.add(parent);
    }
    // drop caches below removed or moved paths
    for (const key of Object.keys(this.dirs)) {
      for (const p of paths) {
        if (p !== "/" && key.startsWith(p + "/")) delete this.dirs[key];
      }
    }
    await Promise.all(
      [...targets].filter((p) => this.dirs[p]).map((p) => this.loadDir(p, true).catch(() => delete this.dirs[p])),
    );
    if (notify) this.version++;
  }

  expandTo(path: string) {
    const parts = path.split("/").filter(Boolean);
    let cur = "";
    this.expanded["/"] = true;
    for (const part of parts) {
      cur += "/" + part;
      this.expanded[cur] = true;
    }
  }

  toast(text: string, kind: Toast["kind"] = "ok") {
    const id = ++toastId;
    this.toasts.push({ id, kind, text });
    setTimeout(() => this.dismiss(id), kind === "error" ? 6000 : 2600);
  }

  dismiss(id: number) {
    this.toasts = this.toasts.filter((t) => t.id !== id);
  }

  error(e: unknown) {
    this.toast(e instanceof Error ? e.message : String(e), "error");
  }
}

export const app = new AppState();

function sameEntries(a: Entry[], b: Entry[]): boolean {
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) {
    const x = a[i];
    const y = b[i];
    if (x.path !== y.path || x.dir !== y.dir || x.size !== y.size || x.mod_time !== y.mod_time || x.type !== y.type) return false;
  }
  return true;
}

/** Hash router: #/path/to/file */
class Router {
  path = $state(readHash());

  constructor() {
    window.addEventListener("hashchange", () => {
      this.path = readHash();
    });
  }

  go(path: string) {
    const next = "#" + encodeURI(path || "/");
    if (location.hash !== next) location.hash = next;
    else this.path = readHash();
  }
}

function readHash(): string {
  const raw = location.hash.replace(/^#/, "");
  let p: string;
  try {
    p = decodeURI(raw);
  } catch {
    p = raw;
  }
  if (!p.startsWith("/")) p = "/" + p;
  if (p.length > 1) p = p.replace(/\/+$/, "");
  return p;
}

export const router = new Router();
