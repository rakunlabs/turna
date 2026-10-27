import { settingTemplates, type AnyRecord, type ApiResponse, type SettingNamespace } from "../api";
import { cloneRecord, joinValues, splitValues } from "../records";
import { sameRecord, withoutSecretValues } from "../settings-record";
import { recordRevision, revisionHeaders } from "../revisions";
import { docket, errorTextOf, messageOf, session, statusOf } from "./session.svelte";

/**
 * Keep stored values separate from drafts. Background reads update live state
 * without silently discarding edits, and writes report success independently
 * from any subsequent refresh of the console.
 */
class Settings {
  byNamespace = $state<Partial<Record<SettingNamespace, AnyRecord>>>({});
  liveByNamespace = $state<Partial<Record<SettingNamespace, AnyRecord>>>({});
  liveRevisions: Partial<Record<SettingNamespace, string>> = {};
  draftRevisions: Partial<Record<SettingNamespace, string>> = {};
  conflicts = $state<Partial<Record<SettingNamespace, boolean>>>({});
  #needsRead = new Set<SettingNamespace>();
  #loadSeq = new Map<SettingNamespace, number>();
  #defaults = new Map<SettingNamespace, AnyRecord>();
  /**
   * Namespaces whose live value was actually read. Until then record() falls
   * back to the template, and committing that would overwrite the real
   * configuration with defaults, so saves refuse unloaded namespaces.
   */
  loaded = $state<Partial<Record<SettingNamespace, true>>>({});

  isLoaded(namespace: SettingNamespace) {
    return this.loaded[namespace] === true;
  }

  #markLoaded(namespace: SettingNamespace) {
    if (!this.loaded[namespace]) this.loaded = { ...this.loaded, [namespace]: true };
  }

  #requireLoaded(namespaces: readonly SettingNamespace[]) {
    const missing = namespaces.filter((namespace) => !this.isLoaded(namespace));
    if (missing.length > 0) {
      throw new Error(`${missing.join(", ")} settings were not loaded; reload before committing`);
    }
    if (namespaces.some((namespace) => this.#needsRead.has(namespace))) throw new Error("The last save succeeded, but its revision could not be read. Re-read before saving again.");
    if (namespaces.some((namespace) => this.conflicts[namespace])) throw new Error("A record changed on the server. Re-read, compare your draft, then discard/reapply edits before saving.");
  }

  default(namespace: SettingNamespace) {
    return cloneRecord(settingTemplates[namespace]);
  }

  record(namespace: SettingNamespace) {
    if (!this.#defaults.has(namespace)) this.#defaults.set(namespace, this.default(namespace));
    return this.byNamespace[namespace] ?? this.#defaults.get(namespace)!;
  }

  liveRecord(namespace: SettingNamespace) {
    return this.liveByNamespace[namespace] ?? this.default(namespace);
  }

  isDirty(namespace: SettingNamespace) {
    return !sameRecord(this.record(namespace), this.liveRecord(namespace));
  }

  get dirtyNamespaces() {
    return (Object.keys(settingTemplates) as SettingNamespace[]).filter((namespace) => this.isDirty(namespace));
  }

  discardDrafts() {
    for (const namespace of this.dirtyNamespaces) {
      this.set(namespace, cloneRecord(this.liveRecord(namespace)));
      this.draftRevisions[namespace] = this.liveRevisions[namespace];
      this.conflicts = { ...this.conflicts, [namespace]: false };
    }
  }

  /** Row forms omit empty optional fields. Normalize a clean baseline alongside
   * the form so merely editing and reverting a field does not create a draft. */
  normalizeFormBaseline(namespace: SettingNamespace, normalized: AnyRecord) {
    if (!this.isLoaded(namespace) || this.isDirty(namespace)) return;
    this.liveByNamespace = { ...this.liveByNamespace, [namespace]: cloneRecord(normalized) };
    this.set(namespace, cloneRecord(normalized));
  }

  set(namespace: SettingNamespace, value: AnyRecord) {
    this.byNamespace = { ...this.byNamespace, [namespace]: value };
  }

  pathValue(namespace: SettingNamespace, path: string[], live = false) {
    let value: unknown = live ? this.liveRecord(namespace) : this.record(namespace);
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
    const seq = (this.#loadSeq.get(namespace) ?? 0) + 1;
    this.#loadSeq.set(namespace, seq);
    const res = await fetch(`${session.apiBase}/settings/${encodeURIComponent(namespace)}`, {
      headers: { "Content-Type": "application/json" },
    });

    // A namespace that was never written answers 404; the built-in default is
    // the live value in that case, not an error.
    if (res.status !== 404 && !res.ok) throw new Error(await errorTextOf(res));
    const body = res.status === 404 ? null : (await res.json()) as ApiResponse<{ value?: unknown; revision?: string }>;
    if (seq !== this.#loadSeq.get(namespace)) return;
    const value = cloneRecord(body?.payload?.value ?? settingTemplates[namespace]);
    // Test against the old baseline before replacing the committed state.
    const dirty = this.isDirty(namespace);
    const revision = res.status === 404 ? "absent" : recordRevision(body?.payload);
    this.liveRevisions[namespace] = revision;
    this.liveByNamespace = { ...this.liveByNamespace, [namespace]: value };
    if (!dirty) {
      this.set(namespace, cloneRecord(value));
      this.draftRevisions[namespace] = revision;
      this.conflicts = { ...this.conflicts, [namespace]: false };
    }
    this.#needsRead.delete(namespace);
    this.#markLoaded(namespace);
  }

  /** Loads every namespace; one failing does not keep the others from loading. */
  async loadAll() {
    const namespaces = Object.keys(settingTemplates) as SettingNamespace[];
    const results = await Promise.allSettled(namespaces.map((namespace) => this.load(namespace)));
    const failed = namespaces.filter((_, i) => results[i].status === "rejected");
    if (failed.length > 0) {
      throw new Error(`settings could not be loaded: ${failed.join(", ")}`);
    }
  }

  #committed(namespace: SettingNamespace, submitted: AnyRecord, draftAtStart: AnyRecord) {
    this.#loadSeq.set(namespace, (this.#loadSeq.get(namespace) ?? 0) + 1);
    const clean = withoutSecretValues(submitted);
    if (sameRecord(this.record(namespace), draftAtStart)) this.set(namespace, cloneRecord(clean));
    this.liveByNamespace = { ...this.liveByNamespace, [namespace]: clean };
    this.#needsRead.add(namespace);
  }

  async save(namespace: SettingNamespace, value?: AnyRecord) {
    let committed = false;
    await session.run(async () => {
      this.#requireLoaded([namespace]);
      const draftAtStart = cloneRecord(this.record(namespace));
      const submitted = cloneRecord(value ?? draftAtStart);
      try {
        await session.request(`settings/${encodeURIComponent(namespace)}`, {
          method: "PUT",
          headers: revisionHeaders(this.draftRevisions[namespace]),
          body: JSON.stringify({ value: submitted }),
        });
      } catch (err) {
        if (statusOf(err) === 412) this.conflicts = { ...this.conflicts, [namespace]: true };
        throw err;
      }
      committed = true;
      this.#committed(namespace, submitted, draftAtStart);
      docket.commit(`${namespace} settings committed`);
      try {
        if (namespace === "admin") {
          await session.loadCapabilities();
          if (!session.isAdmin) return;
        }
        await this.load(namespace);
        await session.loadCore();
      } catch (err) {
        docket.reject(`Saved ${namespace}, but the console could not be refreshed: ${messageOf(err)}`);
      }
    });
    return committed;
  }

  async saveMany(namespaces: readonly SettingNamespace[]) {
    const ok = await session.run(async () => {
      this.#requireLoaded(namespaces);
      const drafts = new Map(namespaces.map((namespace) => [namespace, cloneRecord(this.record(namespace))]));
      const written: SettingNamespace[] = [];
      try {
        for (const namespace of namespaces) {
          try {
            await session.request(`settings/${encodeURIComponent(namespace)}`, {
              method: "PUT",
              headers: revisionHeaders(this.draftRevisions[namespace]),
              body: JSON.stringify({ value: drafts.get(namespace) }),
            });
          } catch (err) {
            if (statusOf(err) === 412) this.conflicts = { ...this.conflicts, [namespace]: true };
            const reason = err instanceof Error ? err.message : String(err);
            const done = written.length > 0 ? ` (already committed: ${written.join(", ")})` : "";
            throw new Error(`${namespace}: ${reason}${done}`);
          }
          written.push(namespace);
          this.#committed(namespace, drafts.get(namespace)!, drafts.get(namespace)!);
        }
      } finally {
        // re-read what landed so the page shows the stored state either way
        const results = await Promise.allSettled(written.map((namespace) => this.load(namespace)));
        results.forEach((result, i) => {
          if (result.status === "rejected") docket.reject(`Saved ${written[i]}, but it could not be re-read: ${messageOf(result.reason)}`);
        });
      }
      try { await session.loadCore(); }
      catch (err) { docket.reject(`Settings saved, but the console could not be refreshed: ${messageOf(err)}`); }
    }, "Settings could not be committed");

    if (ok) docket.commit("OAuth2 settings committed");
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

export function getSettingValue(namespace: SettingNamespace, path: string[]) {
  return settings.pathValue(namespace, path);
}

export function setSettingValue(namespace: SettingNamespace, path: string[], value: unknown) {
  settings.setPathValue(namespace, path, value);
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

export function getLiveSettingBool(namespace: SettingNamespace, path: string[], fallback = false) {
  const value = settings.pathValue(namespace, path, true);
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
  // An emptied field means "use the default", not zero; dropping the key lets
  // the backend default apply. Unparseable text keeps the stored value.
  if (value.trim() === "") {
    settings.setPathValue(namespace, path, undefined);
    return;
  }
  const parsed = Number(value);
  if (Number.isFinite(parsed)) settings.setPathValue(namespace, path, parsed);
}

export function getSettingList(namespace: SettingNamespace, path: string[]) {
  return joinValues(settings.pathValue(namespace, path));
}

export function setSettingList(namespace: SettingNamespace, path: string[], value: string) {
  settings.setPathValue(namespace, path, splitValues(value));
}

export function saveSetting(namespace: SettingNamespace, value?: AnyRecord) {
  return settings.save(namespace, value);
}

export function saveSettings(namespaces: readonly SettingNamespace[]) {
  return settings.saveMany(namespaces);
}
