import type { AnyRecord } from "./api";
import { sameRecord } from "./settings-record";

const fields = ["alias", "description", "role_ids", "permission_ids", "sync_role_ids", "is_active", "local"] as const;

/** Keep server-computed expansions and custody fields out of the editable JSON. */
export function editableUserRecord(record: AnyRecord): AnyRecord {
  const keys: string[] = [...fields, "details", "tmp_role_ids", "tmp_permission_ids"];
  return Object.fromEntries(keys.filter((key) => record[key] !== undefined).map((key) => [key, record[key]]));
}

/** Send only edited fields; never put a stale copy of LDAP or temporary grants back. */
export function userPatch(before: AnyRecord, after: AnyRecord): AnyRecord {
  for (const key of ["tmp_role_ids", "tmp_permission_ids"]) {
    if (!sameRecord(before[key], after[key])) throw new Error("Use the Temporary access panel to change time-limited grants");
  }
  const patch: AnyRecord = {};
  for (const key of fields) {
    if (!sameRecord(before[key], after[key])) {
      patch[key] = after[key] ?? (key.endsWith("_ids") || key === "alias" ? [] : key === "description" ? "" : false);
    }
  }
  const old = (before.details ?? {}) as AnyRecord;
  const next = (after.details ?? {}) as AnyRecord;
  const details: AnyRecord = {};
  for (const key of new Set([...Object.keys(old), ...Object.keys(next)])) {
    if (key === "password_set" || key === "secret_set") continue;
    if ((key === "password" || key === "secret") && !next[key]) continue;
    if (!sameRecord(old[key], next[key])) Object.defineProperty(details, key, { value: next[key] ?? null, enumerable: true });
  }
  if (Object.keys(details).length) patch.details_patch = details;
  return patch;
}
