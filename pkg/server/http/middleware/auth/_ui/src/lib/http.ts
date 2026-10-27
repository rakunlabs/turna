/** Preserve caller headers without losing JSON defaults (Headers and tuples work too). */
export function jsonRequestInit(init: RequestInit = {}): RequestInit {
  const headers = new Headers(init.headers);
  if (!headers.has("Content-Type")) headers.set("Content-Type", "application/json");
  return { ...init, headers };
}
