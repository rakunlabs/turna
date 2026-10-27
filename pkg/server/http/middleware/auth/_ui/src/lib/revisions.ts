import type { AnyRecord } from "./api";

export function recordRevision(payload: AnyRecord | null | undefined): string {
  const revision = payload?.revision;
  return typeof revision === "string" && /^rev-[0-9]+$/.test(revision) ? revision : "";
}

// Undefined means an older server with no revision support. "absent" is a
// confirmed 404: creating the namespace must not replace a concurrent create.
export function revisionHeaders(revision: string | undefined): Record<string, string> {
  if (revision === "absent") return { "If-None-Match": "*" };
  return revision ? { "If-Match": `"${revision}"` } : {};
}

export function apiKeyRevisionHeaders(revision: number | undefined): Record<string, string> {
  return Number.isSafeInteger(revision) && revision! >= 0 ? { "If-Match": `"keyrev-${revision}"` } : {};
}
