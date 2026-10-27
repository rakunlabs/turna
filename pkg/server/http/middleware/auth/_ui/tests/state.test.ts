import assert from "node:assert/strict";
import { afterEach, beforeEach, test } from "node:test";
import { flushSync } from "svelte";
import { jsonRequestInit } from "../src/lib/http";
import { validateJSON } from "../src/lib/records";
import { userPatch } from "../src/lib/user-patch";
import { sameRecord, missingStoredSecrets } from "../src/lib/settings-record";
import { settings } from "../src/lib/state/settings.svelte";
import { registry } from "../src/lib/state/registry.svelte";
import { editor } from "../src/lib/state/editor.svelte";
import { docket, errorTextOf, session } from "../src/lib/state/session.svelte";
import { FormFixture } from "./form-fixture.svelte";
import { runMutation } from "../src/lib/mutation";
import { issuedSecrets } from "../src/lib/state/issued-secrets.svelte";
import { apiKeyRevisionHeaders } from "../src/lib/revisions";
import { deviceLookupCurrent, normalizeDeviceCode } from "../src/lib/device-code";
import { getLiveSettingBool } from "../src/lib/state/settings.svelte";

const originalFetch = globalThis.fetch;
const core = session.loadCore;
const capabilities = session.loadCapabilities;
const json = (value: unknown, status = 200) => new Response(JSON.stringify({ payload: { value } }), { status });
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => { resolve = done; });
  return { promise, resolve };
}

beforeEach(() => {
  // Docket timeouts and location derivation need no DOM.
  globalThis.window = { setTimeout: () => 0, confirm: () => true, location: { pathname: "/auth/ui/" } } as unknown as Window & typeof globalThis;
  settings.byNamespace = {};
  settings.liveByNamespace = {};
  settings.loaded = {};
  settings.liveRevisions = {};
  settings.draftRevisions = {};
  settings.conflicts = {};
  issuedSecrets.clear();
  issuedSecrets.bindPrincipal("test-user");
  registry.rowsByKind = {};
  registry.standingByKind = {};
  editor.close();
  docket.entries = [];
  session.apiBase = "/auth/v1";
  session.loadCore = async () => {};
  session.loadCapabilities = async () => {};
});
afterEach(() => {
  globalThis.fetch = originalFetch;
  session.loadCore = core;
  session.loadCapabilities = capabilities;
});

test("custom Headers and tuples preserve JSON defaults", () => {
  const init = jsonRequestInit({ method: "POST", headers: new Headers({ "X-Test": "yes" }) });
  const headers = new Headers(init.headers);
  assert.equal(headers.get("Content-Type"), "application/json");
  assert.equal(headers.get("X-Test"), "yes");
  assert.equal(new Headers(jsonRequestInit({ headers: [["Content-Type", "text/plain"]] }).headers).get("Content-Type"), "text/plain");
});

test("OAuth errors expose the reason, not just the error code", async () => {
  assert.equal(await errorTextOf(new Response(JSON.stringify({ error: "invalid_grant", error_description: "already used" }))), "invalid_grant: already used");
});

test("API prefix containing ui does not get truncated", () => {
  window.location.pathname = "/uiform/identity/ui/";
  session.deriveApiBase();
  assert.equal(session.apiBase, "/uiform/identity/v1");
});

test("refresh updates live settings but preserves dirty drafts", async () => {
  globalThis.fetch = async () => json({ smtp: { host: "original" } });
  await settings.load("email");
  settings.setPathValue("email", ["smtp", "host"], "unsaved");
  globalThis.fetch = async () => json({ smtp: { host: "changed elsewhere" } });
  await settings.loadAll();
  assert.equal(settings.pathValue("email", ["smtp", "host"]), "unsaved");
  assert.equal(settings.pathValue("email", ["smtp", "host"], true), "changed elsewhere");
  assert.ok(settings.dirtyNamespaces.includes("email"));
});

test("out-of-order settings reads cannot overwrite a newer response", async () => {
  const first = deferred<Response>();
  let count = 0;
  globalThis.fetch = async () => ++count === 1 ? first.promise : json({ issuer: "new" });
  const old = settings.load("totp");
  await settings.load("totp");
  first.resolve(json({ issuer: "old" }));
  await old;
  assert.equal(settings.record("totp").issuer, "new");
});

test("a failed settings load blocks writing template defaults", async () => {
  let writes = 0;
  globalThis.fetch = async (_url, init) => {
    if (init?.method === "PUT") writes++;
    return new Response("down", { status: 503 });
  };
  await assert.rejects(settings.load("email"));
  assert.equal(await settings.save("email"), false);
  assert.equal(writes, 0);
});

test("successful save stays successful if refresh fails, and clears entered secrets", async () => {
  globalThis.fetch = async () => json({ smtp: { host: "mail" } });
  await settings.load("email");
  settings.setPathValue("email", ["smtp", "password"], "entered-secret");
  globalThis.fetch = async (_url, init) => init?.method === "PUT" ? json({}) : new Response("down", { status: 503 });
  assert.equal(await settings.save("email"), true);
  assert.equal(settings.pathValue("email", ["smtp", "password"]), "");
  assert.equal(settings.pathValue("email", ["smtp", "password_set"]), true);
  assert.equal(settings.isDirty("email"), false);
  assert.ok(docket.entries.some((entry) => entry.kind === "committed"));
  assert.ok(docket.entries.some((entry) => entry.text.includes("Saved email")));
});

test("rejected saves preserve the entered draft", async () => {
  globalThis.fetch = async () => json({ issuer: "original" });
  await settings.load("totp");
  settings.setPathValue("totp", ["issuer"], "draft");
  globalThis.fetch = async () => new Response(JSON.stringify({ message: "invalid setting" }), { status: 400 });
  assert.equal(await settings.save("totp"), false);
  assert.equal(settings.record("totp").issuer, "draft");
  assert.equal(settings.isDirty("totp"), true);
});

test("edits made during save survive the post-save read", async () => {
  globalThis.fetch = async () => json({ issuer: "original" });
  await settings.load("totp");
  settings.setPathValue("totp", ["issuer"], "submitted");
  const write = deferred<Response>();
  globalThis.fetch = async (_url, init) => init?.method === "PUT" ? write.promise : json({ issuer: "submitted" });
  const save = settings.save("totp");
  settings.setPathValue("totp", ["issuer"], "typed later");
  write.resolve(json({}));
  assert.equal(await save, true);
  assert.equal(settings.record("totp").issuer, "typed later");
  assert.equal(settings.isDirty("totp"), true);
});

test("partial bulk save reports which settings landed", async () => {
  globalThis.fetch = async () => json({});
  await settings.load("token");
  await settings.load("oauth2");
  globalThis.fetch = async (url, init) => {
    if (init?.method === "PUT" && String(url).endsWith("/oauth2")) return new Response("bad", { status: 400 });
    return json({});
  };
  assert.equal(await settings.saveMany(["token", "oauth2"]), false);
  assert.ok(docket.entries.some((entry) => entry.text.includes("already committed: token")));
});

test("a canceled record load cannot reopen a closed editor", async () => {
  const read = deferred<Response>();
  globalThis.fetch = async () => read.promise;
  const pending = editor.load("users", "old");
  editor.close();
  read.resolve(new Response(JSON.stringify({ payload: { id: "old", alias: ["old"] } })));
  await pending;
  assert.equal(editor.open, false);
  assert.notEqual(editor.loadedID, "old");
});

test("record enabled and identifier changes count as unsaved edits", async () => {
  globalThis.fetch = async () => new Response(JSON.stringify({ payload: { id: "client", enabled: true, config: {} } }));
  await editor.load("clients", "client");
  assert.equal(editor.dirty, false);
  editor.enabled = false;
  assert.equal(editor.dirty, true);
  editor.enabled = true;
  editor.id = "renamed";
  assert.equal(editor.dirty, true);
});

test("user editor patches only changed fields", async () => {
  const baseline = { id: "user", alias: ["user"], local: false, description: "old", details: { name: "User" }, role_ids: ["role"], sync_role_ids: ["ldap-role"] };
  globalThis.fetch = async () => new Response(JSON.stringify({ payload: baseline }));
  await editor.load("users", "user");
  assert.equal(editor.record().id, undefined);
  editor.setString("description", "changed");
  let method: string | undefined;
  let body: unknown;
  globalThis.fetch = async (_url, init) => { method = init?.method; body = JSON.parse(String(init?.body)); return json({}); };
  await editor.commit(async () => {});
  assert.equal(method, "PATCH");
  assert.deepEqual(body, { description: "changed" });
});

test("profile patch does not replace concurrent LDAP details or expose credentials", () => {
  const before = { details: { name: "Old", uid: "ldap", password_set: true }, role_ids: ["role"] };
  const after = { ...before, details: { ...before.details, name: "New", password: "" } };
  assert.deepEqual(userPatch(before, after), { details_patch: { name: "New" } });
});

test("late list results do not replace the latest search", async () => {
  const old = deferred<Response>();
  let count = 0;
  globalThis.fetch = async () => ++count === 1 ? old.promise : new Response(JSON.stringify({ payload: [{ id: "new", alias: ["new"] }], meta: { total_item_count: 1 } }));
  const first = registry.loadKind("users");
  await registry.applyQuery("users", "new", {});
  old.resolve(new Response(JSON.stringify({ payload: [{ id: "old", alias: ["old"] }], meta: { total_item_count: 100 } })));
  await first;
  assert.equal(registry.rows("users")[0].id, "new");
  assert.equal(registry.standing("users").total, 1);
});

test("record JSON must be an object and equality ignores property order", () => {
  assert.ok(validateJSON("null"));
  assert.ok(validateJSON("[]"));
  assert.ok(validateJSON("42"));
  assert.equal(validateJSON("{}"), "");
  assert.equal(sameRecord({ a: 1, b: 2 }, { b: 2, a: 1 }), true);
});

test("row-based settings drafts survive leaving and returning to the form", () => {
  const first = new FormFixture("custom_info");
  flushSync();
  first.draft = { disabled: true, sets: { custom: { claims: { name: "draft" } } } };
  flushSync();
  assert.equal(settings.isDirty("custom_info"), true);
  first.destroy();
  const next = new FormFixture("custom_info");
  try {
    flushSync();
    assert.equal(next.draft.disabled, true);
    assert.deepEqual(next.draft.sets, { custom: { claims: { name: "draft" } } });
  } finally { next.destroy(); }
});

test("refreshing a dirty row-based form does not reset its fields", async () => {
  globalThis.fetch = async () => json({ disabled: false, sets: {} });
  await settings.load("custom_info");
  const form = new FormFixture("custom_info");
  try {
    flushSync();
    form.draft = { disabled: true, sets: {} };
    flushSync();
    await settings.load("custom_info");
    flushSync();
    assert.equal(form.draft.disabled, true);
    assert.equal(settings.liveRecord("custom_info").disabled, false);
  } finally { form.destroy(); }
});

test("discarding settings drafts restores the latest committed baseline", async () => {
  globalThis.fetch = async () => json({ issuer: "live" });
  await settings.load("totp");
  settings.setPathValue("totp", ["issuer"], "draft");
  settings.discardDrafts();
  assert.equal(settings.record("totp").issuer, "live");
  assert.equal(settings.dirtyNamespaces.length, 0);
});

test("temporary grants cannot be silently edited through raw user JSON", () => {
  assert.throws(() => userPatch({ tmp_role_ids: [] }, { tmp_role_ids: [{ id: "role" }] }), /Temporary access/);
});

test("existing accounts retain write-only credential validation", async () => {
  globalThis.fetch = async () => new Response(JSON.stringify({ payload: { id: "user", alias: ["user"], local: true, details: { name: "User", password_set: true } } }));
  await editor.load("users", "user");
  assert.equal(editor.requirementError, "");
  editor.setList("alias", "");
  assert.match(editor.requirementError, /alias/i);
  editor.setList("alias", "user");
  editor.setRecord({ ...editor.record(), details: { name: "User", password_set: false } });
  assert.match(editor.requirementError, /password/i);
});

test("moving a masked provider requires re-entering its credential", () => {
  const provider = { oauth2: { client_secret: "", client_secret_set: true } };
  const live = { providers: { original: provider } };
  assert.deepEqual(missingStoredSecrets(live, live), []);
  assert.deepEqual(missingStoredSecrets({ providers: { renamed: provider } }, live), ["providers.renamed.oauth2.client_secret"]);
  assert.deepEqual(missingStoredSecrets({ providers: { renamed: { oauth2: { client_secret: "replacement", client_secret_set: true } } } }, live), []);
});

test("normalizing a clean row form avoids false dirty state after a revert", async () => {
  globalThis.fetch = async () => json({ providers: { remote: { oauth2: { client_secret: "", client_secret_set: true } } } });
  await settings.load("session_providers");
  const normalized = { providers: { remote: { oauth2: { client_secret_set: true } } } };
  settings.normalizeFormBaseline("session_providers", normalized);
  settings.set("session_providers", { providers: { renamed: { oauth2: { client_secret_set: true } } } });
  assert.equal(settings.isDirty("session_providers"), true);
  settings.set("session_providers", normalized);
  assert.equal(settings.isDirty("session_providers"), false);
});

test("dirty settings keep their original revision across background reloads", async () => {
  globalThis.fetch = async () => new Response(JSON.stringify({ payload: { value: { issuer: "old" }, revision: "rev-100" } }));
  await settings.load("totp");
  settings.setPathValue("totp", ["issuer"], "my draft");
  globalThis.fetch = async () => new Response(JSON.stringify({ payload: { value: { issuer: "other admin" }, revision: "rev-101" } }));
  await settings.load("totp");
  let match = "";
  globalThis.fetch = async (_url, init) => {
    match = new Headers(init?.headers).get("If-Match") ?? "";
    return new Response(JSON.stringify({ message: "record changed" }), { status: 412 });
  };
  assert.equal(await settings.save("totp"), false);
  assert.equal(match, '"rev-100"');
  assert.equal(settings.record("totp").issuer, "my draft");
  assert.equal(settings.conflicts.totp, true);
  settings.discardDrafts();
  assert.equal(settings.record("totp").issuer, "other admin");
  assert.equal(settings.draftRevisions.totp, "rev-101");
  assert.equal(settings.conflicts.totp, false);
});

test("a namespace loaded as absent uses create-only precondition", async () => {
  globalThis.fetch = async () => new Response("", { status: 404 });
  await settings.load("totp");
  let none = "";
  globalThis.fetch = async (_url, init) => {
    none = new Headers(init?.headers).get("If-None-Match") ?? "";
    return new Response("changed", { status: 412 });
  };
  assert.equal(await settings.save("totp"), false);
  assert.equal(none, "*");
});

test("stale user edits preserve the draft and block repeat writes", async () => {
  globalThis.fetch = async () => new Response(JSON.stringify({ payload: { id:"user", revision:"rev-12", local:false, alias:["user"], description:"original", details:{} } }));
  await editor.load("users", "user");
  editor.setString("description", "my edit");
  let writes = 0;
  let match = "";
  globalThis.fetch = async (_url, init) => {
    writes++;
    match = new Headers(init?.headers).get("If-Match") ?? "";
    return new Response(JSON.stringify({ message:"record changed" }), { status:412 });
  };
  await editor.commit(async () => {});
  assert.equal(match, '"rev-12"');
  assert.equal(editor.open, true);
  assert.equal(editor.conflict, true);
  assert.equal(editor.getString("description"), "my edit");
  await editor.commit(async () => {});
  assert.equal(writes, 1);
});

test("failed post-save read cannot fall back to an unconditional settings write", async () => {
  globalThis.fetch = async () => new Response(JSON.stringify({ payload:{value:{issuer:"original"},revision:"rev-20"} }));
  await settings.load("totp");
  settings.setPathValue("totp",["issuer"],"changed");
  let writes=0;
  globalThis.fetch=async (_url,init)=>{
    if(init?.method==="PUT"){writes++;return json({});}
    return new Response("down",{status:503});
  };
  assert.equal(await settings.save("totp"),true);
  settings.setPathValue("totp",["issuer"],"another change");
  assert.equal(await settings.save("totp"),false);
  assert.equal(writes,1);
});

test("temporary access refresh adopts a revision only when other fields are unchanged", async () => {
  const baseline={id:"user",revision:"rev-30",local:false,alias:["user"],description:"original",details:{name:"User"},tmp_role_ids:[],tmp_permission_ids:[]};
  globalThis.fetch=async()=>new Response(JSON.stringify({payload:baseline}));
  await editor.load("users","user");
  editor.setString("description","draft");
  editor.temp.roleIDs="temporary";
  globalThis.fetch=async(url,init)=>{
    if (init?.method==="POST") return json({});
    if(String(url).includes("/users/user")) return new Response(JSON.stringify({payload:{...baseline,revision:"rev-31",tmp_role_ids:[{id:"temporary"}]}}));
    return new Response(JSON.stringify({payload:[],meta:{total_item_count:0}}));
  };
  await editor.patchTemporaryAccess();
  assert.equal(editor.revision,"rev-31");
  assert.equal(editor.getString("description"),"draft");
  editor.temp.roleIDs="second";
  globalThis.fetch=async(url,init)=>{
    if(init?.method==="POST")return json({});
    if(String(url).includes("/users/user"))return new Response(JSON.stringify({payload:{...baseline,description:"another admin",revision:"rev-33",tmp_role_ids:[{id:"second"}]}}));
    return new Response(JSON.stringify({payload:[],meta:{total_item_count:0}}));
  };
  await editor.patchTemporaryAccess();
  assert.equal(editor.revision,"rev-31");
  assert.equal(editor.getString("description"),"draft");
});

test("invalid raw JSON cannot be replaced by temporary-access merging", async () => {
  globalThis.fetch=async()=>new Response(JSON.stringify({payload:{id:"user",local:false,alias:["user"],details:{}}}));
  await editor.load("users","user");
  editor.json="{ broken draft";
  editor.temp.roleIDs="role";
  let requests=0;
  globalThis.fetch=async()=>{requests++;return json({});};
  await editor.patchTemporaryAccess();
  assert.equal(requests,0);
  assert.equal(editor.json,"{ broken draft");
});

test("deleting a list record sends its displayed revision", async () => {
  registry.rowsByKind={users:[{id:"user",sub:"User",enabled:true,updated:"",revision:"rev-40"}]};
  let match="";
  let removed=false;
  globalThis.fetch=async(_url,init)=>{
    match=new Headers(init?.headers).get("If-Match")??"";
    return new Response(JSON.stringify({message:"record changed"}),{status:412});
  };
  await editor.remove("users","user",async()=>{removed=true;});
  assert.equal(match,'"rev-40"');
  assert.equal(removed,false);
});

test("a refresh error cannot turn a successful key issuance into a failed mutation", async () => {
  let writes=0;
  const ok=await runMutation(async()=>{writes++;issuedSecrets.adminKey="shown-once";},async()=>{throw new Error("list offline");},"API key issued","Create failed");
  assert.equal(ok,true);
  assert.equal(writes,1);
  assert.equal(issuedSecrets.adminKey,"shown-once");
  assert.ok(docket.entries.some(entry=>entry.kind==="committed"));
  assert.ok(docket.entries.some(entry=>entry.text.includes("list offline")));
});

test("a rejected mutation never refreshes or reports success",async()=>{
  let refreshed=false;
  assert.equal(await runMutation(async()=>{throw new Error("stale key");},async()=>{refreshed=true;},"Updated","Update failed"),false);
  assert.equal(refreshed,false);
  assert.equal(docket.entries.some(entry=>entry.kind==="committed"),false);
});

test("one-time credentials survive page lifecycles but clear when identity changes",()=>{
  issuedSecrets.adminKey="admin-key";
  issuedSecrets.ownKey="personal-key";
  issuedSecrets.recoveryCodes=["recovery"];
  issuedSecrets.totpSecret="enrollment-secret";
  issuedSecrets.bindPrincipal("test-user");
  assert.equal(issuedSecrets.pending,true);
  assert.equal(issuedSecrets.adminKey,"admin-key");
  assert.equal(issuedSecrets.recoveryCodes[0],"recovery");
  issuedSecrets.bindPrincipal("different-user");
  assert.equal(issuedSecrets.pending,false);
  assert.equal(issuedSecrets.totpSecret,"");
});

test("device response is accepted only for the code still on screen",()=>{
  assert.equal(normalizeDeviceCode("abcd efgh"),"ABCD-EFGH");
  assert.equal(deviceLookupCurrent("ABCD-EFGH","abcd-efgh"),true);
  assert.equal(deviceLookupCurrent("ABCD-EFGH","WXYZ-1234"),false);
});

test("API key operations use committed settings, not an unsaved disable switch",async()=>{
  globalThis.fetch=async()=>json({disabled:false,max_lifetime:"720h"});
  await settings.load("api_key");
  settings.setPathValue("api_key",["disabled"],true);
  settings.setPathValue("api_key",["max_lifetime"],"1h");
  assert.equal(getLiveSettingBool("api_key",["disabled"]),false);
  assert.equal(settings.pathValue("api_key",["max_lifetime"],true),"720h");
  assert.equal(settings.isDirty("api_key"),true);
});

test("API key preconditions use their numeric revision namespace",()=>{
  assert.deepEqual(apiKeyRevisionHeaders(3),{"If-Match":'"keyrev-3"'});
  assert.deepEqual(apiKeyRevisionHeaders(undefined),{});
  assert.deepEqual(apiKeyRevisionHeaders(NaN),{});
});
