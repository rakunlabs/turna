export type BindMode = "simple" | "anonymous" | "unauthenticated";

export type ServerInfo = {
  name: string;
  addr: string;
  base_dn?: string;
  start_tls?: boolean;
};

export type Session = {
  server: string;
  addr: string;
  mode: BindMode;
  bind_dn?: string;
  base_dn?: string;
  start_tls?: boolean;
  insecure?: boolean;
  custom_ca?: boolean;
  since: string;
};

export type Info = {
  title: string;
  servers: ServerInfo[];
  bind_modes: BindMode[];
  max_size_limit: number;
  max_time_limit: number;
  session?: Session;
};

export type LoginRequest = {
  server?: string;
  addr?: string;
  start_tls?: boolean;
  insecure_skip_verify?: boolean;
  ca?: string;
  base_dn?: string;
  mode: BindMode;
  username?: string;
  password?: string;
};

export type Scope = "base" | "one" | "sub" | "children";
export type Deref = "never" | "search" | "find" | "always";

export type SearchRequest = {
  base_dn: string;
  scope: Scope;
  filter: string;
  attributes: string[];
  size_limit: number;
  time_limit: number;
  deref: Deref;
  types_only: boolean;
  page_size: number;
  max_values?: number;
};

export type Attribute = { name: string; values: string[]; binary?: boolean; total?: number };
export type Entry = { dn: string; attributes: Attribute[] };

export type SearchResult = {
  entries: Entry[];
  referrals?: string[];
  duration_ms: number;
  truncated?: boolean;
  warning?: string;
};

export type Child = { dn: string; rdn: string; has_children?: boolean; object_class?: string[] };

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

const base = new URL("api/", document.baseURI);

function url(endpoint: string, params: Record<string, string | undefined> = {}): string {
  const u = new URL(endpoint, base);
  for (const [k, v] of Object.entries(params)) if (v !== undefined) u.searchParams.set(k, v);
  return u.toString();
}

/** Called on 401 so the app can drop back to the login screen. */
export let onUnauthorized = () => {};
export function setUnauthorized(fn: () => void) {
  onUnauthorized = fn;
}

async function check(res: Response): Promise<Response> {
  if (res.ok) return res;
  let message = res.statusText || `Request failed (${res.status})`;
  try {
    const body = await res.clone().json();
    if (body?.error) message = body.error;
  } catch {
    const text = await res.text().catch(() => "");
    if (text.trim()) message = text.trim();
  }
  throw new ApiError(res.status, message);
}

async function json<T>(req: Promise<Response>, auth = true): Promise<T> {
  try {
    return (await check(await req)).json();
  } catch (e) {
    if (auth && e instanceof ApiError && e.status === 401) onUnauthorized();
    throw e;
  }
}

const post = (endpoint: string, body: unknown) =>
  fetch(url(endpoint), { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });

export const api = {
  info: () => json<Info>(fetch(url("info"))),
  login: (req: LoginRequest) => json<Session>(post("login", req), false),
  logout: () => post("logout", {}).then(check),
  search: (req: SearchRequest) => json<SearchResult>(post("search", req)),
  entry: (dn: string, operational = false) =>
    json<SearchResult>(fetch(url("entry", { dn, operational: operational ? "1" : undefined }))),
  children: (dn: string) => json<{ children: Child[]; truncated?: boolean }>(fetch(url("children", { dn }))),
  rootDSE: () => json<SearchResult>(fetch(url("rootdse"))),
  whoami: () => json<{ authz_id: string }>(fetch(url("whoami"))),
  verify: (req: VerifyRequest) => json<VerifyResponse>(post("verify", req)),
};

export type VerifyStep = { name: string; ok: boolean; detail?: string; code?: number; duration_ms: number };
export type VerifyResponse = { ok: boolean; bind_dn?: string; steps: VerifyStep[]; entry?: Entry };
export type VerifyRequest = { username: string; password: string; filter?: string; base_dn?: string; attributes?: string[] };
