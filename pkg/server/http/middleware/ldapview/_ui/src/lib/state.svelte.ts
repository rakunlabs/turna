import { api, type Child, type Info, type SearchRequest, type Session } from "./api";
import type { Flavor } from "./presets";

type Toast = { id: number; kind: "ok" | "error"; text: string };

let toastId = 0;

export const defaultQuery = (baseDN = ""): SearchRequest => ({
  base_dn: baseDN,
  scope: "sub",
  filter: "(objectClass=*)",
  attributes: [],
  size_limit: 100,
  time_limit: 0,
  deref: "never",
  types_only: false,
  page_size: 0,
});

export type HistoryItem = SearchRequest & { at: number };

const HISTORY_KEY = "turna-lv-history";

class AppState {
  info = $state<Info | null>(null);
  session = $state<Session | null>(null);
  toasts = $state<Toast[]>([]);

  /** Tree cache: children by parent DN ("" holds the naming contexts). */
  children = $state<Record<string, Child[]>>({});
  expanded = $state<Record<string, boolean>>({});
  loading = $state<Record<string, boolean>>({});

  query = $state<SearchRequest>(defaultQuery());
  flavor = $state<Flavor>("generic");
  /** Default search base: the session base DN or the first naming context. */
  baseDN = $state("");
  /** Show decoded values (dates, flags, SIDs) instead of raw ones. */
  formatted = $state(localStorage.getItem("turna-lv-raw") !== "1");

  toggleFormatted() {
    this.formatted = !this.formatted;
    localStorage.setItem("turna-lv-raw", this.formatted ? "0" : "1");
  }
  history = $state<HistoryItem[]>(loadHistory());

  /** Bumped to ask the search panel to run the current query. */
  runToken = $state(0);

  async loadChildren(dn: string, force = false) {
    if (!force && this.children[dn]) return;
    this.loading[dn] = true;
    try {
      const res = await api.children(dn);
      this.children[dn] = res.children;
      if (res.truncated) this.toast(`Only the first ${res.children.length} children of ${dn || "root"} are shown`, "error");
    } catch (e) {
      this.toast(String((e as Error).message), "error");
    } finally {
      delete this.loading[dn];
    }
  }

  resetTree() {
    this.children = {};
    this.expanded = {};
  }

  remember(q: SearchRequest) {
    const key = queryKey(q);
    this.history = [{ ...q, at: Date.now() }, ...this.history.filter((h) => queryKey(h) !== key)].slice(0, 30);
    localStorage.setItem(HISTORY_KEY, JSON.stringify(this.history));
  }

  clearHistory() {
    this.history = [];
    localStorage.removeItem(HISTORY_KEY);
  }

  run(q: Partial<SearchRequest>) {
    this.query = { ...this.query, ...q };
    this.runToken++;
  }

  toast(text: string, kind: Toast["kind"] = "ok") {
    const id = ++toastId;
    this.toasts.push({ id, kind, text });
    setTimeout(() => this.dismiss(id), kind === "error" ? 6000 : 2600);
  }

  dismiss(id: number) {
    this.toasts = this.toasts.filter((t) => t.id !== id);
  }
}

function queryKey(q: SearchRequest): string {
  const { base_dn, scope, filter, attributes, size_limit, time_limit, deref, types_only, page_size } = q;
  return JSON.stringify([base_dn, scope, filter, attributes, size_limit, time_limit, deref, types_only, page_size]);
}

function loadHistory(): HistoryItem[] {
  try {
    const v = JSON.parse(localStorage.getItem(HISTORY_KEY) ?? "[]");
    return Array.isArray(v) ? v : [];
  } catch {
    return [];
  }
}

export const app = new AppState();
