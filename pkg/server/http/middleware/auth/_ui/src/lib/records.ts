import type { AnyRecord } from "./api";

/** Pretty-print a record the way the raw JSON exhibit shows it. */
export function pretty(value: unknown) {
  return JSON.stringify(value, null, 2);
}

export function cloneRecord(value: unknown): AnyRecord {
  if (!value || typeof value !== "object" || Array.isArray(value)) return {} as AnyRecord;
  return JSON.parse(JSON.stringify(value)) as AnyRecord;
}

/** Operators paste ID lists separated by commas or newlines; accept both. */
export function splitValues(value: string) {
  return value
    .split(/[\n,]+/)
    .map((item) => item.trim())
    .filter(Boolean);
}

export function joinValues(value: unknown) {
  if (Array.isArray(value)) return value.map(String).join(", ");
  if (typeof value === "string") return value;
  return "";
}

export function fieldText(value: unknown) {
  if (value === undefined || value === null) return "";
  return typeof value === "string" ? value.trim() : String(value).trim();
}

export function fieldList(value: unknown) {
  if (Array.isArray(value)) return value.map(fieldText).filter(Boolean);
  return splitValues(fieldText(value));
}

export function validateJSON(value: string) {
  try {
    JSON.parse(value);
    return "";
  } catch (err) {
    return err instanceof Error ? `Invalid JSON — ${err.message}` : "Invalid JSON";
  }
}

/**
 * Custody line for an instrument: the API stamps `updated_by` / `updated_at` on
 * every record, and this console treats that as first-class, not a footnote.
 */
export function custodyLine(item: Record<string, unknown> | null | undefined) {
  if (!item) return "";
  const by = fieldText(item.updated_by) || "unknown";
  const at = fieldText(item.updated_at);
  return at ? `${by} · ${formatStamp(at)}` : by;
}

/** Absolute, unambiguous, sortable. Operators correlate this with server logs. */
export function formatStamp(value: string) {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;

  const pad = (n: number) => String(n).padStart(2, "0");
  return (
    `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ` +
    `${pad(date.getHours())}:${pad(date.getMinutes())}`
  );
}
