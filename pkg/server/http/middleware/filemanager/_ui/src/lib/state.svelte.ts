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
  /** Bumped after mutations so open views can refresh. */
  version = $state(0);

  rawURL(path: string): string {
    return `${location.origin}${this.info.prefix}${this.info.raw_path}${encodeURI(path)}`;
  }

  async loadDir(path: string, force = false): Promise<Entry[]> {
    if (!force && this.dirs[path]) return this.dirs[path];
    this.loading[path] = true;
    try {
      const res = await api.list(path);
      this.dirs[path] = res.entries;
      return res.entries;
    } finally {
      delete this.loading[path];
    }
  }

  /** Reload every cached directory affected by a change at path. */
  async refresh(...paths: string[]) {
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
    this.version++;
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
