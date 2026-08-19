import { settingTemplates, type AnyRecord, type ApiResponse, type SettingNamespace } from "../api";
import { cloneRecord, joinValues, splitValues } from "../records";
import { docket, errorTextOf, session } from "./session.svelte";

/**
 * Runtime settings live encrypted in PostgreSQL and apply without a restart, so
 * the console never holds a private draft of them: every accessor reads the
 * live record and every write is followed by a reload of the same namespace.
 */
class Settings {
  byNamespace = $state<Partial<Record<SettingNamespace, AnyRecord>>>({});

  default(namespace: SettingNamespace) {
    return cloneRecord(settingTemplates[namespace]);
  }

  record(namespace: SettingNamespace) {
    return this.byNamespace[namespace] ?? this.default(namespace);
  }

  set(namespace: SettingNamespace, value: AnyRecord) {
    this.byNamespace = { ...this.byNamespace, [namespace]: value };
  }

  pathValue(namespace: SettingNamespace, path: string[]) {
    let value: unknown = this.record(namespace);
    for (const key of path) {
      if (!value || typeof value !== "object" || Array.isArray(value)) return undefined;
      value = (value as AnyRecord)[key];
    }

    return value;
  }

  setPathValue(namespace: SettingNamespace, path: string[], value: unknown) {
    const next = cloneRecord(this.record(namespace));
    let cursor = next;

    for (const key of path.slice(0, -1)) {
      const current = cursor[key];
      const child =
        current && typeof current === "object" && !Array.isArray(current)
          ? { ...(current as AnyRecord) }
          : ({} as AnyRecord);
      cursor[key] = child;
      cursor = child;
    }

    cursor[path[path.length - 1]] = value;
    this.set(namespace, next);
  }

  async load(namespace: SettingNamespace) {
    const res = await fetch(`${session.apiBase}/settings/${encodeURIComponent(namespace)}`, {
      headers: { "Content-Type": "application/json" },
    });

    // A namespace that was never written answers 404; the built-in default is
    // the live value in that case, not an error.
    if (res.status === 404) {
      this.set(namespace, this.default(namespace));
      return;
    }

    if (!res.ok) throw new Error(await errorTextOf(res));

    const body = (await res.json()) as ApiResponse<{ value?: unknown }>;
    this.set(namespace, cloneRecord(body.payload?.value ?? settingTemplates[namespace]));
  }

  async loadAll() {
    const namespaces = Object.keys(settingTemplates) as SettingNamespace[];
    await Promise.all(namespaces.map((namespace) => this.load(namespace)));
  }

  async save(namespace: SettingNamespace) {
    const ok = await session.run(async () => {
      await session.request(`settings/${encodeURIComponent(namespace)}`, {
        method: "PUT",
        body: JSON.stringify({ value: this.record(namespace) }),
      });

      await this.load(namespace);
      await session.loadCore();
    });

    if (ok) docket.commit(`${namespace} settings committed`);
    return ok;
  }
}

export const settings = new Settings();

/* ---------------------------------------------------------------------------
   Typed accessors. Reading a rune-backed field inside a template or $derived
   tracks it, so pages bind straight to these and never receive them as props.
   --------------------------------------------------------------------------- */

export function settingRecord(namespace: SettingNamespace) {
  return settings.record(namespace);
}

export function setSettingRecord(namespace: SettingNamespace, value: AnyRecord) {
  settings.set(namespace, value);
}

export function getSettingString(namespace: SettingNamespace, path: string[]) {
  const value = settings.pathValue(namespace, path);
  if (value === undefined || value === null) return "";
  return typeof value === "string" ? value : String(value);
}

export function setSettingString(namespace: SettingNamespace, path: string[], value: string) {
  settings.setPathValue(namespace, path, value);
}

export function getSettingBool(namespace: SettingNamespace, path: string[], fallback = false) {
  const value = settings.pathValue(namespace, path);
  return typeof value === "boolean" ? value : fallback;
}

export function setSettingBool(namespace: SettingNamespace, path: string[], value: boolean) {
  settings.setPathValue(namespace, path, value);
}

export function getSettingNumber(namespace: SettingNamespace, path: string[], fallback = 0) {
  const value = settings.pathValue(namespace, path);
  if (typeof value === "number") return value;
  if (typeof value === "string") {
    const parsed = Number(value);
    if (Number.isFinite(parsed)) return parsed;
  }

  return fallback;
}

export function setSettingNumber(namespace: SettingNamespace, path: string[], value: string) {
  const parsed = Number(value);
  settings.setPathValue(namespace, path, Number.isFinite(parsed) ? parsed : 0);
}

export function getSettingList(namespace: SettingNamespace, path: string[]) {
  return joinValues(settings.pathValue(namespace, path));
}

export function setSettingList(namespace: SettingNamespace, path: string[], value: string) {
  settings.setPathValue(namespace, path, splitValues(value));
}

export function saveSetting(namespace: SettingNamespace) {
  return settings.save(namespace);
}
