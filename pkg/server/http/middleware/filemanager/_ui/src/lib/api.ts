export type Entry = {
  name: string;
  path: string;
  dir: boolean;
  size: number;
  mod_time: string;
  type?: string;
};

export type Info = {
  title: string;
  read_only: boolean;
  prefix: string;
  raw_path: string;
  max_upload_size: number;
};

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

const base = new URL("api/", document.baseURI);

function url(endpoint: string, params: Record<string, string | boolean | undefined> = {}): string {
  const u = new URL(endpoint, base);
  for (const [k, v] of Object.entries(params)) {
    if (v === undefined || v === false) continue;
    u.searchParams.set(k, v === true ? "" : v);
  }
  return u.toString();
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

async function json<T>(res: Promise<Response>): Promise<T> {
  return (await check(await res)).json();
}

export const api = {
  info: () => json<Info>(fetch(url("info"))),

  stat: (path: string) => json<Entry>(fetch(url("stat", { path }))),

  list: (path: string) => json<{ path: string; entries: Entry[] }>(fetch(url("list", { path }))),

  async read(path: string): Promise<{ data: ArrayBuffer; etag: string }> {
    const res = await check(await fetch(url("file", { path })));
    return { data: await res.arrayBuffer(), etag: res.headers.get("ETag") ?? "" };
  },

  fileURL: (path: string) => url("file", { path }),

  /** First bytes of a file, to tell text from binary without downloading it all. */
  async head(path: string, bytes = 8192): Promise<ArrayBuffer> {
    const res = await check(await fetch(url("file", { path }), { headers: { Range: `bytes=0-${bytes - 1}` } }));
    return res.arrayBuffer();
  },

  downloadURL: (path: string) => url("file", { path, download: true }),

  async write(path: string, body: BodyInit, opts: { etag?: string; create?: boolean } = {}) {
    const headers: Record<string, string> = {};
    if (opts.etag) headers["If-Match"] = opts.etag;
    const res = await check(
      await fetch(url("file", { path, create: opts.create }), { method: "PUT", body, headers }),
    );
    return { entry: (await res.json()) as Entry, etag: res.headers.get("ETag") ?? "" };
  },

  async remove(path: string) {
    await check(await fetch(url("file", { path }), { method: "DELETE" }));
  },

  mkdir: (path: string) =>
    json<{ path: string }>(
      fetch(url("mkdir"), {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ path }),
      }),
    ),

  move: (from: string, to: string, overwrite = false) =>
    json<{ path: string }>(
      fetch(url("move"), {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ from, to, overwrite }),
      }),
    ),

  upload(dir: string, files: { file: File; name: string }[], overwrite = false) {
    const form = new FormData();
    for (const f of files) form.append("file", f.file, f.name);
    return json<{ saved: string[] }>(fetch(url("upload", { path: dir, overwrite }), { method: "POST", body: form }));
  },
};
