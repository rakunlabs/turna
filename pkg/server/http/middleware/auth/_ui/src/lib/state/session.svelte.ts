import type { ApiResponse, Capabilities, Dashboard, InfoPayload } from "../api";

/**
 * The docket is the console's record of what just happened. Entries are stamped
 * onto the sheet rather than floated as generic toasts: a rejection is a
 * standing entry until dismissed, a commit clears itself.
 */
export type DocketKind = "committed" | "rejected";

export type DocketEntry = {
  id: number;
  kind: DocketKind;
  text: string;
};

class Docket {
  entries = $state<DocketEntry[]>([]);
  #seq = 0;

  #push(kind: DocketKind, text: string, ttl: number) {
    const id = ++this.#seq;
    this.entries = [...this.entries, { id, kind, text }];

    if (ttl > 0) {
      window.setTimeout(() => this.dismiss(id), ttl);
    }

    return id;
  }

  /** A committed change: self-clearing, the operator already saw the result. */
  commit(text: string) {
    return this.#push("committed", text, 3200);
  }

  /** A rejection: stays until dismissed. Losing an error is losing the reason. */
  reject(text: string) {
    return this.#push("rejected", text, 0);
  }

  dismiss(id: number) {
    this.entries = this.entries.filter((entry) => entry.id !== id);
  }

  clearRejections() {
    this.entries = this.entries.filter((entry) => entry.kind !== "rejected");
  }
}

export const docket = new Docket();

/**
 * Carries the status alongside the message so a caller can tell a meaningful
 * standing (424: no LDAP config enabled) from an actual failure.
 */
export class HttpError extends Error {
  status: number;

  constructor(status: number, message: string) {
    super(message);
    this.name = "HttpError";
    this.status = status;
  }
}

export function messageOf(err: unknown, fallback = "Unknown error") {
  if (err instanceof Error && err.message.trim()) return err.message;
  return fallback;
}

export function statusOf(err: unknown) {
  return err instanceof HttpError ? err.status : 0;
}

/**
 * Session holds what the instance currently is: where its API lives, what this
 * request is allowed to do, and the live auth version every write advances.
 */
class Session {
  apiBase = $state("/auth/v1");
  info = $state<InfoPayload | null>(null);
  dashboard = $state<Dashboard | null>(null);
  capabilities = $state<Capabilities | null>(null);
  loading = $state(true);
  busy = $state(false);

  oauthBase = $derived(this.apiBase.replace(/\/v1$/, ""));
  isAdmin = $derived(this.capabilities?.is_admin === true);
  version = $derived(this.info?.version ?? null);
  linked = $derived(this.info !== null);

  /** Derive the API base from the served path so `prefix_path` stays free. */
  deriveApiBase() {
    const path = window.location.pathname;
    const uiIndex = path.indexOf("/ui");
    this.apiBase = uiIndex > -1 ? `${path.slice(0, uiIndex)}/v1` : "/auth/v1";
  }

  async request<T>(path: string, init?: RequestInit): Promise<ApiResponse<T>> {
    const res = await fetch(`${this.apiBase}/${path}`, {
      headers: { "Content-Type": "application/json", ...(init?.headers ?? {}) },
      ...init,
    });

    if (!res.ok) throw new HttpError(res.status, await errorTextOf(res));

    return res.json();
  }

  /**
   * Some endpoints answer a bare document rather than the `{payload}` envelope
   * (`POST /v1/check` is the notable one). This returns the body as-is.
   */
  async raw<T>(path: string, init?: RequestInit): Promise<T> {
    const res = await fetch(`${this.apiBase}/${path}`, {
      headers: { "Content-Type": "application/json", ...(init?.headers ?? {}) },
      ...init,
    });

    if (!res.ok) throw new HttpError(res.status, await errorTextOf(res));

    return res.json();
  }

  async loadCapabilities() {
    const res = await this.request<Capabilities>("capabilities");
    this.capabilities = res.payload;
  }

  async loadCore() {
    const [infoRes, dashboardRes] = await Promise.all([
      this.request<InfoPayload>("info"),
      this.request<Dashboard>("dashboard"),
    ]);

    this.info = infoRes.payload;
    this.dashboard = dashboardRes.payload;
  }

  /**
   * Run a write with the shared busy flag and one docket entry on failure.
   * Returns true when the work completed, so callers can chain a success step.
   *
   * `fallback` is the page's own wording for a failure that arrives without a
   * readable message — better than a generic "Unknown error" the operator
   * cannot act on. Nested calls are safe: only the outermost one clears busy.
   */
  async run(work: () => Promise<void>, fallback = "Unknown error"): Promise<boolean> {
    const outermost = !this.busy;
    if (outermost) {
      this.busy = true;
      docket.clearRejections();
    }

    try {
      await work();
      return true;
    } catch (err) {
      docket.reject(messageOf(err, fallback));
      return false;
    } finally {
      if (outermost) this.busy = false;
    }
  }
}

export const session = new Session();

/**
 * The API answers errors as `{message: {text, error}}`, but older handlers still
 * return a bare string or `{error}`. Reduce every shape to one readable line so
 * an operator never sees "[object Object]" where the reason should be.
 */
export async function errorTextOf(res: Response) {
  try {
    const body = (await res.json()) as Record<string, unknown>;
    const message = body.message;

    if (typeof message === "string" && message.trim()) return message;

    if (message && typeof message === "object") {
      const detail = message as Record<string, unknown>;
      for (const key of ["error", "text"]) {
        const value = detail[key];
        if (typeof value === "string" && value.trim()) return value;
      }
    }

    if (typeof body.error === "string" && body.error.trim()) return body.error;
  } catch {
    // fall through to the status line
  }

  return res.statusText || `HTTP ${res.status}`;
}
