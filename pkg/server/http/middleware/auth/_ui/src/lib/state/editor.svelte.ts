import { kindSpecs, permissionPresets, type AnyRecord, type KindSpec, type ResourceKind } from "../api";
import {
  cloneRecord,
  custodyLine,
  fieldList,
  fieldText,
  joinValues,
  pretty,
  splitValues,
  validateJSON,
} from "../records";
import { docket, session } from "./session.svelte";
import { registry } from "./registry.svelte";

/**
 * The editor drafts one instrument at a time. The draft is always the raw JSON
 * document — the simple form is a set of views onto it — so the raw exhibit
 * stays an escape hatch that can never disagree with what will be committed.
 */
class Editor {
  kind = $state<ResourceKind>("settings");
  open = $state(false);
  id = $state("");
  loadedID = $state("");
  enabled = $state(true);
  json = $state(pretty(kindSpecs.settings.example));
  advanced = $state(false);
  /** Who last amended the loaded record, and when. Empty for a new draft. */
  custody = $state("");

  temp = $state({
    roleIDs: "",
    permissionIDs: "",
    startsAt: "",
    expiresIn: "1h",
    expiresAt: "",
  });

  spec = $derived<KindSpec>(kindSpecs[this.kind]);
  jsonError = $derived(validateJSON(this.json));
  isNew = $derived(!this.loadedID);

  simpleFormAvailable = $derived(
    this.kind !== "settings" ? true : this.isReservedNamespace(this.loadedID || this.id),
  );

  namespaceAllowed = $derived(
    this.kind !== "settings" || Boolean(this.loadedID) || this.isReservedNamespace(this.id),
  );

  requirementError = $derived(this.#requirementError());

  canCommit = $derived(
    !session.busy &&
      !this.jsonError &&
      !this.requirementError &&
      this.namespaceAllowed &&
      (this.advanced || this.simpleFormAvailable) &&
      (this.spec.body === "raw" || Boolean(this.id.trim())),
  );

  tempSelected = $derived(
    splitValues(this.temp.roleIDs).length > 0 || splitValues(this.temp.permissionIDs).length > 0,
  );

  canGrantTemp = $derived(
    !session.busy &&
      Boolean(this.loadedID) &&
      this.tempSelected &&
      Boolean(this.temp.expiresIn.trim() || this.temp.expiresAt.trim()),
  );

  canRemoveTemp = $derived(!session.busy && Boolean(this.loadedID) && this.tempSelected);

  isReservedNamespace(id: string) {
    return Boolean(kindSpecs.settings.namespaceExamples?.[id.trim()]);
  }

  /* --- draft document ---------------------------------------------------- */

  record(): AnyRecord {
    try {
      const parsed = JSON.parse(this.json);
      if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) return { ...(parsed as AnyRecord) };
    } catch {
      // fall through to the template
    }

    return cloneRecord(this.#template());
  }

  setRecord(next: AnyRecord) {
    this.json = pretty(next);
  }

  #template() {
    const namespaceTemplate = this.isNew ? this.spec.namespaceExamples?.[this.id.trim()] : undefined;
    return namespaceTemplate ?? this.spec.example;
  }

  /**
   * Fields the API will reject anyway, caught here so the operator learns the
   * problem beside the field instead of as a 400 after commit.
   */
  #requirementError() {
    if (this.loadedID || this.jsonError) return "";

    let record: AnyRecord;
    try {
      const parsed = JSON.parse(this.json);
      if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return "";
      record = parsed as AnyRecord;
    } catch {
      return "";
    }

    const detailsValue = record.details;
    const details =
      detailsValue && typeof detailsValue === "object" && !Array.isArray(detailsValue)
        ? (detailsValue as AnyRecord)
        : ({} as AnyRecord);

    if (this.kind === "users") {
      if (fieldList(record.alias).length === 0) return "An alias is required — it is what the user logs in with";
      if (record.local !== false && fieldText(details.name) === "") return "Local users need a name";
      if (record.local !== false && fieldText(details.password) === "") return "Local users need a password";
    }

    if (this.kind === "service-accounts") {
      if (fieldList(record.alias).length === 0) return "An alias is required — the first one becomes the client ID";
      if (fieldText(details.name) === "") return "Service accounts need a name";
      if (
        fieldText(details.secret) === "" &&
        fieldText(details.cert_fingerprint) === "" &&
        fieldText(details.cert_subject) === ""
      ) {
        return "Give the account a client secret or an mTLS certificate binding";
      }
    }

    return "";
  }

  /* --- flat fields ------------------------------------------------------- */

  getString(key: string) {
    const value = this.record()[key];
    if (value === undefined || value === null) return "";
    return typeof value === "string" ? value : String(value);
  }

  setString(key: string, value: string) {
    this.setRecord({ ...this.record(), [key]: value });
  }

  getBool(key: string, fallback = false) {
    const value = this.record()[key];
    return typeof value === "boolean" ? value : fallback;
  }

  setBool(key: string, value: boolean) {
    this.setRecord({ ...this.record(), [key]: value });
  }

  getList(key: string) {
    return joinValues(this.record()[key]);
  }

  setList(key: string, value: string) {
    this.setRecord({ ...this.record(), [key]: splitValues(value) });
  }

  getJSON(key: string) {
    const value = this.record()[key];
    if (value === undefined || value === null) return "{}";
    return pretty(value);
  }

  setJSON(key: string, value: string) {
    try {
      this.setRecord({ ...this.record(), [key]: JSON.parse(value.trim() || "{}") });
    } catch (err) {
      docket.reject(err instanceof Error ? `Invalid ${key} JSON — ${err.message}` : `Invalid ${key} JSON`);
    }
  }

  /** A non-local user authenticates upstream; a stored password would be a lie. */
  setLocalUser(value: boolean) {
    const next = this.record();
    next.local = value;

    if (!value) {
      const details = this.nested("details");
      delete details.password;
      next.details = details;
    }

    this.setRecord(next);
  }

  /* --- nested paths ------------------------------------------------------ */

  pathValue(path: string[]) {
    let value: unknown = this.record();
    for (const key of path) {
      if (!value || typeof value !== "object" || Array.isArray(value)) return undefined;
      value = (value as AnyRecord)[key];
    }

    return value;
  }

  setPathValue(path: string[], value: unknown) {
    const next = this.record();
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
    this.setRecord(next);
  }

  getPathString(path: string[]) {
    const value = this.pathValue(path);
    if (value === undefined || value === null) return "";
    return typeof value === "string" ? value : String(value);
  }

  getPathBool(path: string[], fallback = false) {
    const value = this.pathValue(path);
    return typeof value === "boolean" ? value : fallback;
  }

  getPathNumber(path: string[], fallback = 0) {
    const value = this.pathValue(path);
    if (typeof value === "number") return value;
    if (typeof value === "string") {
      const parsed = Number(value);
      if (Number.isFinite(parsed)) return parsed;
    }

    return fallback;
  }

  setPathNumber(path: string[], value: string) {
    const parsed = Number(value);
    this.setPathValue(path, Number.isFinite(parsed) ? parsed : 0);
  }

  getPathList(path: string[]) {
    return joinValues(this.pathValue(path));
  }

  setPathList(path: string[], value: string) {
    this.setPathValue(path, splitValues(value));
  }

  /* --- nested objects and first-of-array shortcuts ----------------------- */

  nested(parent: string): AnyRecord {
    const value = this.record()[parent];
    if (!value || typeof value !== "object" || Array.isArray(value)) return {} as AnyRecord;
    return { ...(value as AnyRecord) };
  }

  getNestedString(parent: string, key: string) {
    const value = this.nested(parent)[key];
    if (value === undefined || value === null) return "";
    return typeof value === "string" ? value : String(value);
  }

  setNestedString(parent: string, key: string, value: string) {
    this.setRecord({ ...this.record(), [parent]: { ...this.nested(parent), [key]: value } });
  }

  firstOf(parent: string): AnyRecord {
    const items = this.record()[parent];
    if (!Array.isArray(items)) return {} as AnyRecord;
    const first = items[0];
    if (!first || typeof first !== "object" || Array.isArray(first)) return {} as AnyRecord;
    return { ...(first as AnyRecord) };
  }

  getFirstString(parent: string, key: string) {
    const value = this.firstOf(parent)[key];
    if (value === undefined || value === null) return "";
    return typeof value === "string" ? value : String(value);
  }

  #setFirst(parent: string, key: string, value: unknown) {
    const next = this.record();
    const items = Array.isArray(next[parent]) ? [...(next[parent] as unknown[])] : [];
    items[0] = { ...this.firstOf(parent), [key]: value };
    next[parent] = items;
    this.setRecord(next);
  }

  setFirstString(parent: string, key: string, value: string) {
    this.#setFirst(parent, key, value);
  }

  getFirstList(parent: string, key: string) {
    return joinValues(this.firstOf(parent)[key]);
  }

  setFirstList(parent: string, key: string, value: string) {
    this.#setFirst(parent, key, splitValues(value));
  }

  /* --- permission resources ---------------------------------------------- */

  resources(): AnyRecord[] {
    const resources = this.record().resources;
    if (!Array.isArray(resources)) return [];

    return resources.map((resource) =>
      resource && typeof resource === "object" && !Array.isArray(resource)
        ? { ...(resource as AnyRecord) }
        : ({} as AnyRecord),
    );
  }

  resourceAt(index: number) {
    return this.resources()[index] ?? ({} as AnyRecord);
  }

  getResourceList(index: number, key: string) {
    const resource = this.resourceAt(index);
    const value = joinValues(resource[key]);

    if (key === "paths" && !value.trim()) return fieldText(resource.path);

    return value;
  }

  setResourceList(index: number, key: string, value: string) {
    const next = this.record();
    const resources = this.resources();
    const resource = { ...this.resourceAt(index), [key]: splitValues(value) };

    if (key === "paths") delete resource.path;

    resources[index] = resource;
    next.resources = resources;
    this.setRecord(next);
  }

  addResource() {
    this.setRecord({
      ...this.record(),
      resources: [...this.resources(), { hosts: [], paths: ["/api/**"], methods: ["GET"] }],
    });
  }

  removeResource(index: number) {
    this.setRecord({
      ...this.record(),
      resources: this.resources().filter((_, i) => i !== index),
    });
  }

  /* --- temporary access -------------------------------------------------- */

  resetTemp() {
    this.temp = { roleIDs: "", permissionIDs: "", startsAt: "", expiresIn: "1h", expiresAt: "" };
  }

  temporaryItems(key: "tmp_role_ids" | "tmp_permission_ids"): AnyRecord[] {
    const value = this.record()[key];
    if (!Array.isArray(value)) return [];

    return value
      .filter((item) => item && typeof item === "object" && !Array.isArray(item))
      .map((item) => item as AnyRecord);
  }

  async patchTemporaryAccess(remove = false) {
    if (!this.loadedID || (this.kind !== "users" && this.kind !== "service-accounts")) return;

    const roleIDs = splitValues(this.temp.roleIDs);
    const permissionIDs = splitValues(this.temp.permissionIDs);
    if (roleIDs.length === 0 && permissionIDs.length === 0) {
      docket.reject("Name at least one role or permission to grant");
      return;
    }

    if (!remove && !this.temp.expiresIn.trim() && !this.temp.expiresAt.trim()) {
      docket.reject("Temporary access needs an expiry — either a duration or an exact time");
      return;
    }

    const body: AnyRecord = { role_ids: roleIDs, permission_ids: permissionIDs };

    if (!remove) {
      if (this.temp.startsAt.trim()) body.starts_at = new Date(this.temp.startsAt).toISOString();
      if (this.temp.expiresIn.trim()) body.expires_in = this.temp.expiresIn.trim();
      else if (this.temp.expiresAt.trim()) body.expires_at = new Date(this.temp.expiresAt).toISOString();
    }

    const kind = this.kind;
    const id = this.loadedID;

    const ok = await session.run(async () => {
      await session.request(`${this.spec.listPath}/${encodeURIComponent(id)}/access`, {
        method: "POST",
        body: JSON.stringify(body),
      });

      await registry.loadKind(kind);
    });

    if (ok) {
      await this.load(kind, id);
      docket.commit(remove ? "Temporary access withdrawn" : "Temporary access granted");
    }
  }

  /* --- lifecycle ---------------------------------------------------------- */

  reset(kind: ResourceKind) {
    const spec = kindSpecs[kind];
    this.kind = kind;
    this.loadedID = "";
    this.id = spec.idField === "namespace" ? (Object.keys(spec.namespaceExamples ?? {})[0] ?? "") : "";
    this.enabled = true;
    this.advanced = false;
    this.custody = "";
    this.json = pretty(spec.example);
    this.resetTemp();
    this.applyNamespaceExample();
  }

  startCreate(kind: ResourceKind) {
    this.reset(kind);
    this.open = true;
    docket.clearRejections();
  }

  close() {
    this.open = false;
    docket.clearRejections();
    this.reset(this.kind);
  }

  setAdvanced(enabled: boolean) {
    // Leaving raw mode with unparseable JSON would strand the form on a document
    // it cannot read; restore the template instead of silently discarding.
    if (!enabled && this.jsonError) this.loadTemplate();
    this.advanced = enabled;
  }

  loadTemplate() {
    this.json = pretty(this.#template());
    docket.clearRejections();
  }

  formatJSON() {
    try {
      this.json = pretty(JSON.parse(this.json));
      docket.clearRejections();
    } catch (err) {
      docket.reject(err instanceof Error ? `Invalid JSON — ${err.message}` : "Invalid JSON");
    }
  }

  applyNamespaceExample(id = this.id) {
    const nextID = id.trim();
    const example = this.spec.namespaceExamples?.[nextID];
    if (this.isNew && example !== undefined) {
      this.id = nextID;
      this.json = pretty(example);
    }
  }

  /** Ready-made permission documents scoped to this instance's live prefix. */
  applyPermissionPreset(name: string) {
    if (this.kind !== "permissions" || this.loadedID) return;

    const preset = permissionPresets(session.oauthBase)[name];
    if (!preset) return;

    this.advanced = false;
    this.json = pretty(preset);
    docket.clearRejections();
  }

  async load(kind: ResourceKind, id: string) {
    const spec = kindSpecs[kind];

    const ok = await session.run(async () => {
      const res = await session.request<Record<string, unknown>>(
        `${spec.listPath}/${encodeURIComponent(id)}`,
      );

      const payload = res.payload as Record<string, unknown>;

      this.kind = kind;
      this.id = id;
      this.loadedID = id;
      this.advanced = false;
      this.custody = custodyLine(payload);
      this.resetTemp();

      if (spec.body === "value") {
        this.json = pretty(payload.value);
      } else if (spec.body === "config") {
        this.enabled = Boolean(payload.enabled);
        this.json = pretty(payload.config);
      } else {
        this.json = pretty(payload);
      }
    });

    if (ok) this.open = true;
  }

  async commit(onCommitted: () => Promise<void>) {
    const spec = this.spec;

    let parsed: unknown;
    try {
      parsed = JSON.parse(this.json);
    } catch (err) {
      docket.reject(err instanceof Error ? `Invalid JSON — ${err.message}` : "Invalid JSON");
      return;
    }

    if (this.requirementError) {
      docket.reject(this.requirementError);
      return;
    }

    if (spec.body !== "raw" && !this.id.trim()) {
      docket.reject(`${spec.primaryLabel} is required`);
      return;
    }

    if (this.kind === "settings" && this.isNew && !this.isReservedNamespace(this.id)) {
      docket.reject("Settings namespaces are reserved — pick one of the known namespaces");
      return;
    }

    const created = this.isNew;

    const ok = await session.run(async () => {
      if (spec.body === "raw") {
        await session.request(
          this.loadedID ? `${spec.listPath}/${encodeURIComponent(this.loadedID)}` : spec.listPath,
          { method: this.loadedID ? "PUT" : "POST", body: JSON.stringify(parsed) },
        );
      } else {
        const body = spec.body === "value" ? { value: parsed } : { enabled: this.enabled, config: parsed };
        await session.request(`${spec.listPath}/${encodeURIComponent(this.id.trim())}`, {
          method: "PUT",
          body: JSON.stringify(body),
        });
      }

      await onCommitted();
    });

    if (ok) {
      this.open = false;
      docket.commit(created ? `${spec.title} record issued` : `${spec.title} record amended`);
    }
  }

  async remove(kind: ResourceKind, id: string, onRemoved: () => Promise<void>) {
    const ok = await session.run(async () => {
      await session.request(`${kindSpecs[kind].listPath}/${encodeURIComponent(id)}`, { method: "DELETE" });
      await onRemoved();
    });

    if (ok) docket.commit(`${id} revoked`);
  }
}

export const editor = new Editor();
