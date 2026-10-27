import type { AnyRecord } from "./api";
import { cloneRecord } from "./records";

const secretFields = new Set(["password", "secret", "client_secret", "private_key", "registration_token"]);

/** A masked credential can only be retained at its existing record/path. */
export function missingStoredSecrets(draft: AnyRecord, live: AnyRecord, prefix = ""): string[] {
  const missing: string[] = [];
  for (const [key, value] of Object.entries(draft)) {
    const path = prefix ? `${prefix}.${key}` : key;
    if (key.endsWith("_set") && value === true) {
      const field = key.slice(0, -4);
      if (secretFields.has(field.toLowerCase()) && !draft[field] && live[key] !== true) missing.push(prefix ? `${prefix}.${field}` : field);
    } else if (value && typeof value === "object" && !Array.isArray(value)) {
      const previous = live[key];
      missing.push(...missingStoredSecrets(value as AnyRecord, previous && typeof previous === "object" && !Array.isArray(previous) ? previous as AnyRecord : {}, path));
    }
  }
  return missing;
}

export function sameRecord(a: unknown, b: unknown): boolean {
  if (Object.is(a, b)) return true;
  if (Array.isArray(a) && Array.isArray(b)) {
    return a.length === b.length && a.every((item, i) => sameRecord(item, b[i]));
  }
  if (a && b && typeof a === "object" && typeof b === "object" && !Array.isArray(a) && !Array.isArray(b)) {
    const left = a as AnyRecord;
    const right = b as AnyRecord;
    const keys = Object.keys(left).filter((key) => left[key] !== undefined);
    const other = Object.keys(right).filter((key) => right[key] !== undefined);
    return keys.length === other.length && keys.every((key) => Object.prototype.hasOwnProperty.call(right, key) && sameRecord(left[key], right[key]));
  }
  return false;
}

/** Do not keep freshly entered credentials in the form after a successful write. */
export function withoutSecretValues(value: AnyRecord): AnyRecord {
  const record = cloneRecord(value);
  function visit(value: unknown) {
    if (!value || typeof value !== "object") return;
    if (Array.isArray(value)) { value.forEach(visit); return; }
    const node = value as AnyRecord;
    for (const [key, child] of Object.entries(node)) {
      if (secretFields.has(key.toLowerCase()) && typeof child === "string") {
        if (child) node[`${key}_set`] = true;
        node[key] = "";
      } else visit(child);
    }
  }
  visit(record);
  return record;
}
