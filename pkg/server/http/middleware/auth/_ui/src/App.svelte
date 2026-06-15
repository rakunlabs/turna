<script lang="ts">
  import { onMount } from "svelte";
  import { fly } from "svelte/transition";
  import AppHeader from "./components/AppHeader.svelte";
  import NavRail from "./components/NavRail.svelte";
  import OverviewTab from "./components/OverviewTab.svelte";
  import OAuthOverview from "./components/OAuthOverview.svelte";
  import AccessCheckTab from "./components/AccessCheckTab.svelte";
  import ResourcePage from "./components/ResourcePage.svelte";
  import RecordEditor from "./components/RecordEditor.svelte";
  import LdapGroupsPanel from "./components/LdapGroupsPanel.svelte";
  import AuthFlowGuide from "./components/AuthFlowGuide.svelte";
  import AccountTab from "./components/AccountTab.svelte";
  import APIKeysTab from "./components/APIKeysTab.svelte";
  import DeviceTab from "./components/DeviceTab.svelte";
  import EmailTab from "./components/EmailTab.svelte";
  import MagicLinkTab from "./components/MagicLinkTab.svelte";
  import SignupTab from "./components/SignupTab.svelte";
  import MTLSTab from "./components/MTLSTab.svelte";
  import EncryptionTab from "./components/EncryptionTab.svelte";
  import AdminTab from "./components/AdminTab.svelte";
  import CacheTab from "./components/CacheTab.svelte";
  import DeviceSettingsTab from "./components/DeviceSettingsTab.svelte";
  import TokenExchangeTab from "./components/TokenExchangeTab.svelte";
  import TotpTab from "./components/TotpTab.svelte";
  import FlowsTab from "./components/FlowsTab.svelte";
  import { editableSettingNamespaces, kindSpecs, permissionPresets, rowFromItem, settingTemplates } from "./lib/api";
  import { isResourceTab, nav, navGroups } from "./lib/navigation";
  import type { AnyRecord, ApiResponse, Dashboard, InfoPayload, KindSpec, ResourceKind, Row, SettingNamespace } from "./lib/api";
  import type { Tab } from "./lib/navigation";

  type ThemeMode = "system" | "dark" | "light";
  type Capabilities = {
    is_admin: boolean;
    anonymous_admin: boolean;
    bootstrap_admin: boolean;
    self_service: boolean;
    admin_permission: string;
    admin_permission_configured: boolean;
    allow_missing_x_user: boolean;
    x_user?: string;
    authorization_error?: string;
  };

  let activeTab: Tab = "overview";
  let apiBase = "/auth/v1";
  let info: InfoPayload | null = null;
  let dashboard: Dashboard | null = null;
  let capabilities: Capabilities | null = null;
  let loading = true;
  let busy = false;
  let error = "";
  let notice = "";
  let deviceUserCode = "";
  let themeMode: ThemeMode = "system";
  // self-service tabs work without the admin data set; load it lazily
  let adminLoaded = false;

  let rowsByKind: Partial<Record<ResourceKind, Row[]>> = {};
  let settingsByNamespace: Partial<Record<SettingNamespace, AnyRecord>> = {};
  let settingsRevision = 0;
  let jwksKeys: AnyRecord[] = [];

  let editorKind: ResourceKind = "settings";
  let editorOpen = false;
  let editorID = "";
  let editorLoadedID = "";
  let editorEnabled = true;
  let editorJSON = pretty(kindSpecs.settings.example);
  let advancedMode = false;
  let tempAccessRoleIDs = "";
  let tempAccessPermissionIDs = "";
  let tempAccessStartsAt = "";
  let tempAccessExpiresIn = "1h";
  let tempAccessExpiresAt = "";

  $: editorSpec = kindSpecs[editorKind];
  $: editorJSONError = validateJSON(editorJSON);
  $: simpleFormAvailable = hasSimpleForm(editorKind, editorID, editorLoadedID);
  $: settingsNamespaceAllowed = editorKind !== "settings" || Boolean(editorLoadedID) || isReservedSettingsNamespace(editorID);
  $: editorRequirementError = requiredEditorFieldsError(editorKind, editorLoadedID, editorJSON, editorJSONError);
  $: canCommit = !busy && !editorJSONError && !editorRequirementError && settingsNamespaceAllowed && (advancedMode || simpleFormAvailable) && (editorSpec.body === "raw" || Boolean(editorID.trim()));
  $: tempAccessIDsSelected = splitValues(tempAccessRoleIDs).length > 0 || splitValues(tempAccessPermissionIDs).length > 0;
  $: canGrantTemporaryAccess = !busy && Boolean(editorLoadedID) && tempAccessIDsSelected && Boolean(tempAccessExpiresIn.trim() || tempAccessExpiresAt.trim());
  $: canRemoveTemporaryAccess = !busy && Boolean(editorLoadedID) && tempAccessIDsSelected;
  $: oauthBase = apiBase.replace(/\/v1$/, "");
  // live inputs for the dynamic Flows page
  $: ldapActive = (rowsByKind.ldap ?? []).some((row) => row.enabled);
  $: providerCount = (rowsByKind.providers ?? []).length;
  $: samlCount = (rowsByKind.saml ?? []).length;
  $: jwksKey = jwksKeys[0] ?? ({} as AnyRecord);
  $: isAdmin = capabilities?.is_admin === true;
  // depend on isAdmin explicitly so the nav recomputes when capabilities load
  $: visibleNavGroups = navGroups
    .map((group) => ({ ...group, items: group.items.filter((item) => isAdmin || isSelfServiceTab(item.id)) }))
    .filter((group) => group.items.length > 0);
  $: visibleNav = visibleNavGroups.flatMap((group) => group.items);

  function pretty(value: unknown) {
    return JSON.stringify(value, null, 2);
  }

  function validateJSON(value: string) {
    try {
      JSON.parse(value);
      return "";
    } catch (err) {
      const message = err instanceof Error ? err.message : "INVALID JSON";
      return `INVALID JSON: ${message}`;
    }
  }

  function templateFor(spec: KindSpec, id: string, loadedID: string) {
    const namespaceTemplate = !loadedID ? spec.namespaceExamples?.[id.trim()] : undefined;
    return namespaceTemplate ?? spec.example;
  }

  function hasSimpleForm(kind: ResourceKind, id: string, loadedID: string) {
    if (kind !== "settings") return true;
    if (loadedID) return isReservedSettingsNamespace(loadedID);
    return isReservedSettingsNamespace(id);
  }

  function isReservedSettingsNamespace(id: string) {
    return Boolean(kindSpecs.settings.namespaceExamples?.[id.trim()]);
  }

  function fieldText(value: unknown) {
    if (value === undefined || value === null) return "";
    return typeof value === "string" ? value.trim() : String(value).trim();
  }

  function fieldList(value: unknown) {
    if (Array.isArray(value)) return value.map(fieldText).filter(Boolean);
    return splitValues(fieldText(value));
  }

  function requiredEditorFieldsError(kind: ResourceKind, loadedID: string, json: string, jsonError: string) {
    if (loadedID) return "";
    if (jsonError) return "";

    let record = {} as AnyRecord;
    try {
      const parsed = JSON.parse(json);
      if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
        record = parsed as AnyRecord;
      }
    } catch {
      return "";
    }

    const detailsValue = record.details;
    const details = detailsValue && typeof detailsValue === "object" && !Array.isArray(detailsValue) ? (detailsValue as AnyRecord) : ({} as AnyRecord);

    if (kind === "users") {
      if (fieldList(record.alias).length === 0) return "ALIAS IS REQUIRED FOR LOGIN";
      if (record.local !== false && fieldText(details.name) === "") return "NAME IS REQUIRED FOR LOCAL USERS";
      if (record.local !== false && fieldText(details.password) === "") return "PASSWORD IS REQUIRED FOR LOCAL USERS";
    }

    if (kind === "service-accounts") {
      if (fieldList(record.alias).length === 0) return "CLIENT ID ALIAS IS REQUIRED";
      if (fieldText(details.name) === "") return "NAME IS REQUIRED FOR SERVICE ACCOUNTS";
      if (fieldText(details.secret) === "" && fieldText(details.cert_fingerprint) === "" && fieldText(details.cert_subject) === "") {
        return "CLIENT SECRET OR MTLS CERTIFICATE IS REQUIRED";
      }
    }

    return "";
  }

  function splitValues(value: string) {
    return value
      .split(/[\n,]+/)
      .map((item) => item.trim())
      .filter(Boolean);
  }

  function joinValues(value: unknown) {
    if (Array.isArray(value)) return value.map(String).join(", ");
    if (typeof value === "string") return value;
    return "";
  }

  function cloneRecord(value: unknown) {
    if (!value || typeof value !== "object" || Array.isArray(value)) return {} as AnyRecord;
    return JSON.parse(JSON.stringify(value)) as AnyRecord;
  }

  function defaultSetting(namespace: SettingNamespace) {
    return cloneRecord(settingTemplates[namespace]);
  }

  function settingRecord(namespace: SettingNamespace) {
    return settingsByNamespace[namespace] ?? defaultSetting(namespace);
  }

  function setSettingRecord(namespace: SettingNamespace, value: AnyRecord) {
    settingsByNamespace = { ...settingsByNamespace, [namespace]: value };
    settingsRevision += 1;
  }

  function settingPathValue(namespace: SettingNamespace, path: string[]) {
    let value: unknown = settingRecord(namespace);
    for (const key of path) {
      if (!value || typeof value !== "object" || Array.isArray(value)) return undefined;
      value = (value as AnyRecord)[key];
    }

    return value;
  }

  function setSettingPathValue(namespace: SettingNamespace, path: string[], value: unknown) {
    const next = cloneRecord(settingRecord(namespace));
    let cursor = next;

    for (const key of path.slice(0, -1)) {
      const current = cursor[key];
      const child = current && typeof current === "object" && !Array.isArray(current) ? { ...(current as AnyRecord) } : ({} as AnyRecord);
      cursor[key] = child;
      cursor = child;
    }

    cursor[path[path.length - 1]] = value;
    setSettingRecord(namespace, next);
  }

  function getSettingString(namespace: SettingNamespace, path: string[]) {
    const value = settingPathValue(namespace, path);
    if (value === undefined || value === null) return "";
    return typeof value === "string" ? value : String(value);
  }

  function setSettingString(namespace: SettingNamespace, path: string[], value: string) {
    setSettingPathValue(namespace, path, value);
  }

  function getSettingBool(namespace: SettingNamespace, path: string[], fallback = false) {
    const value = settingPathValue(namespace, path);
    return typeof value === "boolean" ? value : fallback;
  }

  function setSettingBool(namespace: SettingNamespace, path: string[], value: boolean) {
    setSettingPathValue(namespace, path, value);
  }

  function getSettingNumber(namespace: SettingNamespace, path: string[], fallback = 0) {
    const value = settingPathValue(namespace, path);
    if (typeof value === "number") return value;
    if (typeof value === "string") {
      const parsed = Number(value);
      if (Number.isFinite(parsed)) return parsed;
    }

    return fallback;
  }

  function setSettingNumber(namespace: SettingNamespace, path: string[], value: string) {
    const parsed = Number(value);
    setSettingPathValue(namespace, path, Number.isFinite(parsed) ? parsed : 0);
  }

  function getSettingList(namespace: SettingNamespace, path: string[]) {
    return joinValues(settingPathValue(namespace, path));
  }

  function setSettingList(namespace: SettingNamespace, path: string[], value: string) {
    setSettingPathValue(namespace, path, splitValues(value));
  }

  function resetTemporaryAccessForm() {
    tempAccessRoleIDs = "";
    tempAccessPermissionIDs = "";
    tempAccessStartsAt = "";
    tempAccessExpiresIn = "1h";
    tempAccessExpiresAt = "";
  }

  function temporaryAccessItems(key: "tmp_role_ids" | "tmp_permission_ids", json: string) {
    let record = {} as AnyRecord;
    try {
      const parsed = JSON.parse(json);
      if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) record = parsed as AnyRecord;
    } catch {
      return [] as AnyRecord[];
    }

    const value = record[key];
    if (!Array.isArray(value)) return [] as AnyRecord[];

    return value
      .filter((item) => item && typeof item === "object" && !Array.isArray(item))
      .map((item) => item as AnyRecord);
  }

  function parseEditorValue() {
    try {
      return JSON.parse(editorJSON);
    } catch {
      return templateFor(editorSpec, editorID, editorLoadedID);
    }
  }

  function editorRecord() {
    const value = parseEditorValue();
    if (!value || typeof value !== "object" || Array.isArray(value)) return {} as AnyRecord;
    return { ...(value as AnyRecord) };
  }

  function setEditorRecord(next: AnyRecord) {
    editorJSON = pretty(next);
    error = "";
  }

  function getStringField(key: string) {
    const value = editorRecord()[key];
    if (value === undefined || value === null) return "";
    return typeof value === "string" ? value : String(value);
  }

  function setStringField(key: string, value: string) {
    const next = editorRecord();
    next[key] = value;
    setEditorRecord(next);
  }

  function getBoolField(key: string, fallback = false) {
    const value = editorRecord()[key];
    return typeof value === "boolean" ? value : fallback;
  }

  function setBoolField(key: string, value: boolean) {
    const next = editorRecord();
    next[key] = value;
    setEditorRecord(next);
  }

  function setLocalUser(value: boolean) {
    const next = editorRecord();
    next.local = value;

    if (!value) {
      const details = nestedRecord("details");
      delete details.password;
      next.details = details;
    }

    setEditorRecord(next);
  }

  function getListField(key: string) {
    return joinValues(editorRecord()[key]);
  }

  function setListField(key: string, value: string) {
    const next = editorRecord();
    next[key] = splitValues(value);
    setEditorRecord(next);
  }

  function getJSONField(key: string) {
    const value = editorRecord()[key];
    if (value === undefined || value === null) return "{}";
    return pretty(value);
  }

  function setJSONField(key: string, value: string) {
    try {
      const next = editorRecord();
      next[key] = JSON.parse(value.trim() || "{}");
      setEditorRecord(next);
    } catch (err) {
      error = err instanceof Error ? `INVALID ${key.toUpperCase()} JSON: ${err.message}` : `INVALID ${key.toUpperCase()} JSON`;
    }
  }

  function editorPathValue(path: string[]) {
    let value: unknown = editorRecord();
    for (const key of path) {
      if (!value || typeof value !== "object" || Array.isArray(value)) return undefined;
      value = (value as AnyRecord)[key];
    }

    return value;
  }

  function setEditorPathValue(path: string[], value: unknown) {
    const next = editorRecord();
    let cursor = next;

    for (const key of path.slice(0, -1)) {
      const current = cursor[key];
      const child = current && typeof current === "object" && !Array.isArray(current) ? { ...(current as AnyRecord) } : ({} as AnyRecord);
      cursor[key] = child;
      cursor = child;
    }

    cursor[path[path.length - 1]] = value;
    setEditorRecord(next);
  }

  function getPathString(path: string[]) {
    const value = editorPathValue(path);
    if (value === undefined || value === null) return "";
    return typeof value === "string" ? value : String(value);
  }

  function setPathString(path: string[], value: string) {
    setEditorPathValue(path, value);
  }

  function setPathBool(path: string[], value: boolean) {
    setEditorPathValue(path, value);
  }

  function getPathBool(path: string[], fallback = false) {
    const value = editorPathValue(path);
    return typeof value === "boolean" ? value : fallback;
  }

  function getPathNumber(path: string[], fallback = 0) {
    const value = editorPathValue(path);
    if (typeof value === "number") return value;
    if (typeof value === "string") {
      const parsed = Number(value);
      if (Number.isFinite(parsed)) return parsed;
    }

    return fallback;
  }

  function setPathNumber(path: string[], value: string) {
    const parsed = Number(value);
    setEditorPathValue(path, Number.isFinite(parsed) ? parsed : 0);
  }

  function getPathList(path: string[]) {
    return joinValues(editorPathValue(path));
  }

  function setPathList(path: string[], value: string) {
    setEditorPathValue(path, splitValues(value));
  }

  function nestedRecord(parent: string) {
    const value = editorRecord()[parent];
    if (!value || typeof value !== "object" || Array.isArray(value)) return {} as AnyRecord;
    return { ...(value as AnyRecord) };
  }

  function getNestedString(parent: string, key: string) {
    const value = nestedRecord(parent)[key];
    if (value === undefined || value === null) return "";
    return typeof value === "string" ? value : String(value);
  }

  function setNestedString(parent: string, key: string, value: string) {
    const next = editorRecord();
    next[parent] = { ...nestedRecord(parent), [key]: value };
    setEditorRecord(next);
  }

  function firstArrayRecord(parent: string) {
    const items = editorRecord()[parent];
    if (!Array.isArray(items)) return {} as AnyRecord;
    const first = items[0];
    if (!first || typeof first !== "object" || Array.isArray(first)) return {} as AnyRecord;
    return { ...(first as AnyRecord) };
  }

  function getFirstArrayString(parent: string, key: string) {
    const value = firstArrayRecord(parent)[key];
    if (value === undefined || value === null) return "";
    return typeof value === "string" ? value : String(value);
  }

  function setFirstArrayString(parent: string, key: string, value: string) {
    const next = editorRecord();
    const items = Array.isArray(next[parent]) ? [...(next[parent] as unknown[])] : [];
    items[0] = { ...firstArrayRecord(parent), [key]: value };
    next[parent] = items;
    setEditorRecord(next);
  }

  function getFirstArrayList(parent: string, key: string) {
    return joinValues(firstArrayRecord(parent)[key]);
  }

  function setFirstArrayList(parent: string, key: string, value: string) {
    const next = editorRecord();
    const items = Array.isArray(next[parent]) ? [...(next[parent] as unknown[])] : [];
    items[0] = { ...firstArrayRecord(parent), [key]: splitValues(value) };
    next[parent] = items;
    setEditorRecord(next);
  }

  function permissionResources() {
    const resources = editorRecord().resources;
    if (!Array.isArray(resources)) return [] as AnyRecord[];

    return resources.map((resource) => {
      if (!resource || typeof resource !== "object" || Array.isArray(resource)) return {} as AnyRecord;
      return { ...(resource as AnyRecord) };
    });
  }

  function resourceAt(index: number) {
    return permissionResources()[index] ?? ({} as AnyRecord);
  }

  function getResourceList(index: number, key: string) {
    return joinValues(resourceAt(index)[key]);
  }

  function setResourceList(index: number, key: string, value: string) {
    const next = editorRecord();
    const resources = permissionResources();
    resources[index] = { ...resourceAt(index), [key]: splitValues(value) };
    next.resources = resources;
    setEditorRecord(next);
  }

  function addPermissionResource() {
    const next = editorRecord();
    next.resources = [...permissionResources(), { hosts: [], paths: ["/api/**"], methods: ["GET"] }];
    setEditorRecord(next);
  }

  function removePermissionResource(index: number) {
    const next = editorRecord();
    next.resources = permissionResources().filter((_, resourceIndex) => resourceIndex !== index);
    setEditorRecord(next);
  }

  function setAdvancedMode(enabled: boolean) {
    if (!enabled && editorJSONError) {
      loadEditorTemplate();
    }

    advancedMode = enabled;
  }

  function formatEditorJSON() {
    try {
      editorJSON = pretty(JSON.parse(editorJSON));
      error = "";
    } catch (err) {
      error = err instanceof Error ? `INVALID JSON: ${err.message}` : "INVALID JSON";
    }
  }

  function loadEditorTemplate() {
    editorJSON = pretty(templateFor(editorSpec, editorID, editorLoadedID));
    error = "";
  }

  function deriveApiBase() {
    const path = window.location.pathname;
    const uiIndex = path.indexOf("/ui");
    if (uiIndex > -1) {
      return `${path.slice(0, uiIndex)}/v1`;
    }

    return "/auth/v1";
  }

  async function request<T>(path: string, init?: RequestInit): Promise<ApiResponse<T>> {
    const res = await fetch(`${apiBase}/${path}`, {
      headers: {
        "Content-Type": "application/json",
        ...(init?.headers ?? {}),
      },
      ...init,
    });

    if (!res.ok) {
      let message = res.statusText;
      try {
        const body = await res.json();
        message = body.message || body.error || message;
      } catch {
        // keep status text
      }

      throw new Error(message);
    }

    return res.json();
  }

  function flash(message: string) {
    notice = message;
    window.setTimeout(() => {
      if (notice === message) notice = "";
    }, 3000);
  }

  async function loadSetting(namespace: SettingNamespace) {
    const res = await fetch(`${apiBase}/settings/${encodeURIComponent(namespace)}`, {
      headers: { "Content-Type": "application/json" },
    });

    if (res.status === 404) {
      setSettingRecord(namespace, defaultSetting(namespace));
      return;
    }

    if (!res.ok) {
      let message = res.statusText;
      try {
        const body = await res.json();
        message = body.message || body.error || message;
      } catch {
        // keep status text
      }

      throw new Error(message);
    }

    const body = (await res.json()) as ApiResponse<{ value?: unknown }>;
    setSettingRecord(namespace, cloneRecord(body.payload?.value ?? settingTemplates[namespace]));
  }

  async function loadRuntimeSettings() {
    await Promise.all((Object.keys(settingTemplates) as SettingNamespace[]).map((namespace) => loadSetting(namespace)));
  }

  async function loadJWKS() {
    const res = await fetch(`${oauthBase}/oauth2/certs`);
    if (!res.ok) return;

    const body = (await res.json()) as { keys?: AnyRecord[] };
    jwksKeys = body.keys ?? [];
  }

  async function loadCapabilities() {
    const res = await request<Capabilities>("capabilities");
    capabilities = res.payload;
  }

  async function saveSetting(namespace: SettingNamespace) {
    busy = true;
    error = "";
    try {
      await request(`settings/${encodeURIComponent(namespace)}`, {
        method: "PUT",
        body: JSON.stringify({ value: settingRecord(namespace) }),
      });

      await Promise.all([loadSetting(namespace), loadKind("settings")]);
      if (namespace === "jwt") {
        await loadJWKS();
      }
      flash(`${namespace.toUpperCase()} SETTING SAVED`);
    } catch (err) {
      error = err instanceof Error ? err.message : "UNKNOWN ERROR";
    } finally {
      busy = false;
    }
  }

  async function rotateJWT() {
    if (!confirm("ROTATE JWT SIGNING KEY? All outstanding access and refresh tokens become invalid.")) return;

    busy = true;
    error = "";
    try {
      await request("jwt/rotate", { method: "POST", body: "{}" });

      await Promise.all([loadSetting("jwt"), loadJWKS(), loadKind("settings")]);
      flash("JWT SIGNING KEY ROTATED");
    } catch (err) {
      error = err instanceof Error ? err.message : "UNKNOWN ERROR";
    } finally {
      busy = false;
    }
  }

  async function loadKind(kind: ResourceKind) {
    const spec = kindSpecs[kind];
    const query = kind === "users" || kind === "service-accounts" ? "?add_roles=false&_limit=500" : "";
    const res = await request<Record<string, unknown>[]>(`${spec.listPath}${query}`);

    let rows = (res.payload ?? []).map((item) => rowFromItem(kind, item));
    if (kind === "settings") {
      rows = rows.filter((row) => (editableSettingNamespaces as readonly string[]).includes(row.id));
    }

    rowsByKind[kind] = rows;
    rowsByKind = rowsByKind;
  }

  async function refresh() {
    busy = true;
    error = "";
    try {
      await loadCapabilities();
      if (!capabilities?.is_admin) {
        adminLoaded = false;
        return;
      }

      const [infoRes, dashboardRes] = await Promise.all([
        request<InfoPayload>("info"),
        request<Dashboard>("dashboard"),
      ]);

      info = infoRes.payload;
      dashboard = dashboardRes.payload;

      await Promise.all((Object.keys(kindSpecs) as ResourceKind[]).map((kind) => loadKind(kind)));
      await Promise.all([loadRuntimeSettings(), loadJWKS()]);
      adminLoaded = true;
    } catch (err) {
      error = err instanceof Error ? err.message : "UNKNOWN ERROR";
    } finally {
      busy = false;
      loading = false;
    }
  }

  // resets the editor fields for a fresh record without opening the editor view
  function resetEditor(kind: ResourceKind) {
    editorKind = kind;
    editorLoadedID = "";
    editorID = kindSpecs[kind].idField === "namespace" ? Object.keys(kindSpecs[kind].namespaceExamples ?? {})[0] ?? "" : "";
    editorEnabled = true;
    advancedMode = false;
    editorJSON = pretty(kindSpecs[kind].example);
    resetTemporaryAccessForm();
    applyNamespaceExample();
  }

  // opens the editor view in create mode
  function startCreate(kind: ResourceKind) {
    resetEditor(kind);
    editorOpen = true;
    error = "";
  }

  // returns from the editor view back to the resource list
  function closeEditor() {
    editorOpen = false;
    error = "";
    resetEditor(editorKind);
  }

  // prefill the permission editor with a ready-made auth_admin / auth_user
  // template scoped to the live prefix; only when creating a new permission.
  function applyPermissionPreset(name: string) {
    if (editorKind !== "permissions" || editorLoadedID) return;

    const preset = permissionPresets(oauthBase)[name];
    if (!preset) return;

    advancedMode = false;
    editorJSON = pretty(preset);
    error = "";
  }

  // prefill the editor with the reserved-namespace template when creating settings
  function applyNamespaceExample(id = editorID) {
    const spec = kindSpecs[editorKind];
    const nextID = id.trim();
    const example = spec.namespaceExamples?.[nextID];
    if (!editorLoadedID && example !== undefined) {
      editorID = nextID;
      editorJSON = pretty(example);
    }
  }

  async function editResource(kind: ResourceKind, id: string) {
    busy = true;
    error = "";
    try {
      const spec = kindSpecs[kind];
      const res = await request<Record<string, unknown>>(`${spec.listPath}/${encodeURIComponent(id)}`);

      editorKind = kind;
      editorID = id;
      editorLoadedID = id;
      advancedMode = false;
      resetTemporaryAccessForm();

      if (spec.body === "value") {
        editorJSON = pretty((res.payload as Record<string, unknown>).value);
      } else if (spec.body === "config") {
        editorEnabled = Boolean((res.payload as Record<string, unknown>).enabled);
        editorJSON = pretty((res.payload as Record<string, unknown>).config);
      } else {
        editorJSON = pretty(res.payload);
      }

      editorOpen = true;
    } catch (err) {
      error = err instanceof Error ? err.message : "UNKNOWN ERROR";
    } finally {
      busy = false;
    }
  }

  async function saveResource() {
    const spec = kindSpecs[editorKind];

    let parsed: unknown;
    try {
      parsed = JSON.parse(editorJSON);
    } catch (err) {
      error = err instanceof Error ? err.message : "INVALID JSON";
      return;
    }

    if (editorRequirementError) {
      error = editorRequirementError;
      return;
    }

    busy = true;
    error = "";
    try {
      if (spec.body === "raw") {
        if (editorLoadedID) {
          await request(`${spec.listPath}/${encodeURIComponent(editorLoadedID)}`, {
            method: "PUT",
            body: JSON.stringify(parsed),
          });
        } else {
          await request(spec.listPath, {
            method: "POST",
            body: JSON.stringify(parsed),
          });
        }
      } else {
        if (!editorID.trim()) {
          error = "ID IS REQUIRED";
          busy = false;
          return;
        }

        if (editorKind === "settings" && !editorLoadedID && !isReservedSettingsNamespace(editorID)) {
          error = "SETTINGS NAMESPACE MUST BE RESERVED";
          busy = false;
          return;
        }

        const body = spec.body === "value" ? { value: parsed } : { enabled: editorEnabled, config: parsed };
        await request(`${spec.listPath}/${encodeURIComponent(editorID.trim())}`, {
          method: "PUT",
          body: JSON.stringify(body),
        });
      }

      await refresh();
      editorOpen = false;
      flash("RECORD COMMITTED");
    } catch (err) {
      error = err instanceof Error ? err.message : "UNKNOWN ERROR";
    } finally {
      busy = false;
    }
  }

  async function patchTemporaryAccess(remove = false) {
    if (!editorLoadedID || (editorKind !== "users" && editorKind !== "service-accounts")) return;

    const roleIDs = splitValues(tempAccessRoleIDs);
    const permissionIDs = splitValues(tempAccessPermissionIDs);
    if (roleIDs.length === 0 && permissionIDs.length === 0) {
      error = "ROLE IDS OR PERMISSION IDS ARE REQUIRED";
      return;
    }

    if (!remove && !tempAccessExpiresIn.trim() && !tempAccessExpiresAt.trim()) {
      error = "EXPIRES IN OR EXPIRES AT IS REQUIRED";
      return;
    }

    const body: AnyRecord = {
      role_ids: roleIDs,
      permission_ids: permissionIDs,
    };

    if (!remove) {
      if (tempAccessStartsAt.trim()) body.starts_at = tempAccessStartsAt.trim();
      if (tempAccessExpiresIn.trim()) {
        body.expires_in = tempAccessExpiresIn.trim();
      } else if (tempAccessExpiresAt.trim()) {
        body.expires_at = tempAccessExpiresAt.trim();
      }
    }

    busy = true;
    error = "";
    try {
      await request(`${editorSpec.listPath}/${encodeURIComponent(editorLoadedID)}/access`, {
        method: "POST",
        body: JSON.stringify(body),
      });

      await loadKind(editorKind);
      await editResource(editorKind, editorLoadedID);
      flash(remove ? "TEMPORARY ACCESS REMOVED" : "TEMPORARY ACCESS UPDATED");
    } catch (err) {
      error = err instanceof Error ? err.message : "UNKNOWN ERROR";
    } finally {
      busy = false;
    }
  }

  async function deleteResource(kind: ResourceKind, id: string) {
    if (!confirm(`DELETE ${id}?`)) return;

    busy = true;
    error = "";
    try {
      const spec = kindSpecs[kind];
      await request(`${spec.listPath}/${encodeURIComponent(id)}`, { method: "DELETE" });

      await refresh();
      flash("RECORD PURGED");
    } catch (err) {
      error = err instanceof Error ? err.message : "UNKNOWN ERROR";
    } finally {
      busy = false;
    }
  }

  async function ldapSyncNow() {
    busy = true;
    error = "";
    try {
      await request("ldap/sync", { method: "POST", body: JSON.stringify({ force: false }) });
      await refresh();
      flash("LDAP SYNC COMPLETE");
    } catch (err) {
      error = err instanceof Error ? err.message : "UNKNOWN ERROR";
    } finally {
      busy = false;
    }
  }

  function isSelfServiceTab(tab: Tab) {
    return tab === "account" || tab === "device";
  }

  function currentIsAdmin() {
    return capabilities?.is_admin === true;
  }

  function canUseTab(tab: Tab) {
    return currentIsAdmin() || isSelfServiceTab(tab);
  }

  function fallbackTab() {
    return currentIsAdmin() ? "overview" : "account";
  }

  function ensureAllowedTab() {
    if (canUseTab(activeTab)) return;

    activeTab = fallbackTab();
    window.location.hash = activeTab === "overview" ? "" : activeTab;
  }

  function isThemeMode(value: string | null): value is ThemeMode {
    return value === "system" || value === "dark" || value === "light";
  }

  function resolvedTheme(mode: ThemeMode) {
    if (mode !== "system") return mode;
    return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
  }

  function applyThemeMode(mode: ThemeMode) {
    document.documentElement.dataset.themeMode = mode;
    document.documentElement.dataset.theme = resolvedTheme(mode);
  }

  function setThemeMode(mode: ThemeMode) {
    themeMode = mode;
    localStorage.setItem("turna-auth-theme", mode);
    applyThemeMode(mode);
  }

  function initThemeMode() {
    const stored = localStorage.getItem("turna-auth-theme");
    themeMode = isThemeMode(stored) ? stored : "system";
    applyThemeMode(themeMode);

    const media = window.matchMedia("(prefers-color-scheme: dark)");
    const onSystemThemeChange = () => {
      if (themeMode === "system") applyThemeMode("system");
    };

    media.addEventListener("change", onSystemThemeChange);

    return () => media.removeEventListener("change", onSystemThemeChange);
  }

  function selectTab(tab: Tab) {
    if (!canUseTab(tab)) return;

    activeTab = tab;
    // switching tabs always lands on the resource list, never the editor
    editorOpen = false;
    if (isResourceTab(tab)) {
      resetEditor(tab);
    }

    // admin data is skipped when landing on a self-service tab; load it on demand
    if (currentIsAdmin() && !isSelfServiceTab(tab) && !adminLoaded && !busy) {
      void refresh();
    }

    window.location.hash = tab === "overview" ? "" : tab;
  }

  async function boot() {
    const hash = window.location.hash.replace(/^#/, "") as Tab;
    if (hash && nav.some((item) => item.id === hash)) {
      activeTab = hash;
    }

    // /ui/device is the RFC 8628 verification_uri; ?user_code= prefills the form
    const params = new URLSearchParams(window.location.search);
    deviceUserCode = params.get("user_code") ?? "";
    if (/\/ui\/device\/?$/.test(window.location.pathname) || deviceUserCode) {
      activeTab = "device";
    }

    try {
      await loadCapabilities();
    } catch (err) {
      error = err instanceof Error ? err.message : "UNKNOWN ERROR";
      loading = false;
      return;
    }

    ensureAllowedTab();

    if (isResourceTab(activeTab)) {
      resetEditor(activeTab);
    }

    if (currentIsAdmin() && !isSelfServiceTab(activeTab)) {
      await refresh();
    } else {
      loading = false;
    }
  }

  onMount(() => {
    const cleanupThemeMode = initThemeMode();
    apiBase = deriveApiBase();

    void boot();

    return cleanupThemeMode;
  });
</script>

<svelte:head>
  <title>TURNA // AUTH</title>
</svelte:head>

<main class="grid h-screen grid-rows-[auto,1fr] overflow-hidden bg-crt font-mono text-fg">
  <AppHeader {info} {busy} {themeMode} onRefresh={refresh} onThemeMode={setThemeMode} />

  <div class="grid min-h-0 grid-rows-[auto,1fr] overflow-hidden lg:grid-cols-[230px,1fr] lg:grid-rows-1">
    <NavRail {activeTab} navGroups={visibleNavGroups} nav={visibleNav} {info} {busy} onSelect={selectTab} onRefresh={refresh} />

    <section class="min-h-0 min-w-0 overflow-y-auto overscroll-contain">
      {#if loading}
        <div class="grid min-h-[60vh] place-items-center">
          <p class="text-sm uppercase tracking-[0.3em] text-dim">
            ESTABLISHING LINK <span class="animate-pulse text-fg">&#9608;</span>
          </p>
        </div>
      {:else}
        {#if capabilities?.anonymous_admin}
          <div class="flex items-center gap-3 border-b border-line bg-panel px-4 py-2">
            <span class="bg-alert px-2 py-0.5 text-[10px] font-bold uppercase tracking-[0.15em] text-white">BREAK-GLASS</span>
            <span class="text-xs uppercase tracking-[0.05em] text-alert">X-USER MISSING; ADMIN ACCESS ALLOWED</span>
          </div>
        {:else if capabilities?.bootstrap_admin}
          <div class="flex items-center gap-3 border-b border-line bg-panel px-4 py-2">
            <span class="bg-alert px-2 py-0.5 text-[10px] font-bold uppercase tracking-[0.15em] text-white">BOOTSTRAP</span>
            <span class="text-xs uppercase tracking-[0.05em] text-alert">ADMIN PERMISSION NOT CONFIGURED</span>
          </div>
        {/if}

        {#if activeTab === "account"}
          <AccountTab {apiBase} />
        {:else if activeTab === "api-keys"}
          <APIKeysTab
            {apiBase}
            {busy}
            {settingsRevision}
            {getSettingBool}
            {setSettingBool}
            {getSettingString}
            {setSettingString}
            {saveSetting}
          />
        {:else if activeTab === "device"}
          <DeviceTab {apiBase} initialUserCode={deviceUserCode} />
        {:else if activeTab === "email"}
          <EmailTab
            {apiBase}
            {busy}
            {settingsRevision}
            {settingRecord}
            {getSettingBool}
            {setSettingBool}
            {getSettingString}
            {setSettingString}
            {getSettingNumber}
            {setSettingNumber}
            {saveSetting}
          />
        {:else if activeTab === "magic-link"}
          <MagicLinkTab
            {apiBase}
            {busy}
            {settingsRevision}
            {settingRecord}
            {getSettingBool}
            {setSettingBool}
            {getSettingString}
            {setSettingString}
            {saveSetting}
          />
        {:else if activeTab === "signup"}
          <SignupTab
            {apiBase}
            {busy}
            {settingsRevision}
            {getSettingBool}
            {setSettingBool}
            {getSettingString}
            {setSettingString}
            {getSettingList}
            {setSettingList}
            {getSettingNumber}
            {setSettingNumber}
            {saveSetting}
          />
        {:else if activeTab === "mtls"}
          <MTLSTab
            {apiBase}
            {busy}
            {settingsRevision}
            {getSettingBool}
            {setSettingBool}
            {getSettingString}
            {setSettingString}
            {saveSetting}
            onSelect={selectTab}
          />
        {:else if activeTab === "encryption"}
          <EncryptionTab {apiBase} {busy} />
        {:else if activeTab === "admin"}
          <AdminTab
            {busy}
            {settingsRevision}
            {getSettingBool}
            {setSettingBool}
            {getSettingString}
            {setSettingString}
            {saveSetting}
          />
        {:else if activeTab === "cache"}
          <CacheTab
            {busy}
            {settingsRevision}
            {getSettingBool}
            {setSettingBool}
            {getSettingString}
            {setSettingString}
            {getSettingList}
            {setSettingList}
            {saveSetting}
          />
        {:else if activeTab === "device-settings"}
          <DeviceSettingsTab
            {busy}
            {settingsRevision}
            {getSettingBool}
            {setSettingBool}
            {getSettingString}
            {setSettingString}
            {getSettingNumber}
            {setSettingNumber}
            {saveSetting}
          />
        {:else if activeTab === "token-exchange"}
          <TokenExchangeTab
            {busy}
            {settingsRevision}
            {getSettingBool}
            {setSettingBool}
            {saveSetting}
          />
        {:else if activeTab === "totp"}
          <TotpTab
            {busy}
            {settingsRevision}
            {getSettingBool}
            {setSettingBool}
            {getSettingString}
            {setSettingString}
            {getSettingNumber}
            {setSettingNumber}
            {saveSetting}
          />
        {:else if activeTab === "flows"}
          <FlowsTab {settingsRevision} {getSettingBool} {ldapActive} {providerCount} {samlCount} />
        {:else if activeTab === "overview"}
          <OverviewTab {dashboard} {apiBase} {busy} onLdapSync={ldapSyncNow} />
        {:else if activeTab === "oauth2-overview"}
          <OAuthOverview
            {busy}
            {settingsRevision}
            {oauthBase}
            {jwksKey}
            {getSettingString}
            {setSettingString}
            {getSettingBool}
            {setSettingBool}
            {getSettingList}
            {setSettingList}
            {saveSetting}
            {rotateJWT}
          />
        {:else if activeTab === "check"}
          <AccessCheckTab
            {apiBase}
            {busy}
            {settingsRevision}
            {getSettingBool}
            {setSettingBool}
            {getSettingList}
            {setSettingList}
            {saveSetting}
          />
        {:else if activeTab === "docs"}
          <div class="grid gap-px bg-line p-px">
            <AuthFlowGuide {apiBase} />
          </div>
        {:else if isResourceTab(activeTab)}
          {#if !editorOpen}
            <div class="grid gap-px bg-line p-px">
              <ResourcePage
                kind={activeTab}
                rows={rowsByKind[activeTab] ?? []}
                onCreate={startCreate}
                onEdit={editResource}
                onDelete={deleteResource}
              />

              {#if activeTab === "lmaps"}
                <LdapGroupsPanel {apiBase} />
              {/if}
            </div>
          {:else}
            <div class="grid gap-px bg-line p-px">
            <RecordEditor
              {closeEditor}
              {editorKind}
              {editorSpec}
              {apiBase}
              bind:editorID
              {editorLoadedID}
              bind:editorEnabled
              bind:editorJSON
              {advancedMode}
              {editorJSONError}
              {editorRequirementError}
              {canCommit}
              {simpleFormAvailable}
              {busy}
              bind:tempAccessRoleIDs
              bind:tempAccessPermissionIDs
              bind:tempAccessStartsAt
              bind:tempAccessExpiresIn
              bind:tempAccessExpiresAt
              {canGrantTemporaryAccess}
              {canRemoveTemporaryAccess}
              {setAdvancedMode}
              {formatEditorJSON}
              {loadEditorTemplate}
              {saveResource}
              {applyNamespaceExample}
              {applyPermissionPreset}
              {getStringField}
              {setStringField}
              {getBoolField}
              {setBoolField}
              {setLocalUser}
              {getListField}
              {setListField}
              {getNestedString}
              {setNestedString}
              {getFirstArrayString}
              {setFirstArrayString}
              {getFirstArrayList}
              {setFirstArrayList}
              {getJSONField}
              {setJSONField}
              {getPathString}
              {setPathString}
              {setPathBool}
              {getPathBool}
              {getPathNumber}
              {setPathNumber}
              {getPathList}
              {setPathList}
              {permissionResources}
              {addPermissionResource}
              {removePermissionResource}
              {getResourceList}
              {setResourceList}
              {temporaryAccessItems}
              {patchTemporaryAccess}
            />
            </div>
          {/if}
        {/if}
      {/if}
    </section>
  </div>

  <!-- transient toasts (FAULT / OK) float over the canvas instead of pushing content -->
  <div class="pointer-events-none fixed bottom-4 right-4 z-[100] flex w-[min(360px,calc(100vw-2rem))] flex-col gap-2">
    {#if error}
      <div
        class="pointer-events-auto flex items-start gap-3 border border-line border-l-2 border-l-alert bg-panel px-3 py-2 shadow-[0_8px_30px_rgb(0_0_0_/_0.45)]"
        role="alert"
        transition:fly={{ x: 16, duration: 160 }}
      >
        <span class="mt-px shrink-0 bg-alert px-2 py-0.5 text-[10px] font-bold uppercase tracking-[0.15em] text-white">FAULT</span>
        <span class="min-w-0 flex-1 break-words text-xs uppercase leading-4 tracking-[0.05em] text-alert">{error}</span>
        <button class="shrink-0 text-sm leading-none text-dim hover:text-fg" on:click={() => (error = "")} aria-label="dismiss fault">×</button>
      </div>
    {/if}

    {#if notice}
      <div
        class="pointer-events-auto flex items-start gap-3 border border-line border-l-2 border-l-phosphor bg-panel px-3 py-2 shadow-[0_8px_30px_rgb(0_0_0_/_0.45)]"
        role="status"
        transition:fly={{ x: 16, duration: 160 }}
      >
        <span class="mt-px shrink-0 bg-phosphor px-2 py-0.5 text-[10px] font-bold uppercase tracking-[0.15em] text-crt">OK</span>
        <span class="min-w-0 flex-1 break-words text-xs uppercase leading-4 tracking-[0.05em]">{notice}</span>
        <button class="shrink-0 text-sm leading-none text-dim hover:text-fg" on:click={() => (notice = "")} aria-label="dismiss notice">×</button>
      </div>
    {/if}
  </div>
</main>
