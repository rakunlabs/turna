import { editableSettingNamespaces, kindSpecs, rowFromItem, type AnyRecord, type ResourceKind, type Row } from "../api";
import { docket, session } from "./session.svelte";
import { settings } from "./settings.svelte";

/**
 * The registry is the console's index of issued instruments: one row list per
 * record kind, plus the published signing keys. Admin bulk data is deferred
 * until a page needs it so a self-service visit stays quiet.
 */
class Registry {
  rowsByKind = $state<Partial<Record<ResourceKind, Row[]>>>({});
  jwks = $state<AnyRecord[]>([]);
  loaded = $state(false);

  rows(kind: ResourceKind) {
    return this.rowsByKind[kind] ?? [];
  }

  signingKey = $derived(this.jwks[0] ?? ({} as AnyRecord));
  ldapActive = $derived(this.rows("ldap").some((row) => row.enabled));
  providerCount = $derived(this.rows("providers").length);
  samlCount = $derived(this.rows("saml").length);

  async loadKind(kind: ResourceKind) {
    const spec = kindSpecs[kind];
    // Identity lists are the only ones that can be large; skip role expansion
    // and cap the page so the index stays responsive on a populated instance.
    const query = kind === "users" || kind === "service-accounts" ? "?add_roles=false&_limit=500" : "";
    const res = await session.request<Record<string, unknown>[]>(`${spec.listPath}${query}`);

    let rows = (res.payload ?? []).map((item) => rowFromItem(kind, item));
    if (kind === "settings") {
      rows = rows.filter((row) => (editableSettingNamespaces as readonly string[]).includes(row.id));
    }

    this.rowsByKind = { ...this.rowsByKind, [kind]: rows };
  }

  async loadJWKS() {
    const res = await fetch(`${session.oauthBase}/oauth2/certs`);
    if (!res.ok) return;

    const body = (await res.json()) as { keys?: AnyRecord[] };
    this.jwks = body.keys ?? [];
  }

  async loadAll() {
    const kinds = Object.keys(kindSpecs) as ResourceKind[];
    await Promise.all(kinds.map((kind) => this.loadKind(kind)));
    await Promise.all([settings.loadAll(), this.loadJWKS()]);
    this.loaded = true;
  }

  /** Load the bulk admin data once, on demand. */
  async ensureLoaded() {
    if (this.loaded || session.busy) return;
    await session.run(() => this.loadAll());
  }

  async rotateSigningKey() {
    const ok = await session.run(async () => {
      await session.request("jwt/rotate", { method: "POST", body: "{}" });
      await Promise.all([settings.load("jwt"), this.loadJWKS(), this.loadKind("settings")]);
    });

    if (ok) docket.commit("Signing key rotated — outstanding tokens are now invalid");
    return ok;
  }

  /** `force` re-reads every group and member instead of honouring the interval. */
  async syncLdap(force = false) {
    const ok = await session.run(async () => {
      await session.request("ldap/sync", { method: "POST", body: JSON.stringify({ force }) });
      await Promise.all([this.loadKind("users"), this.loadKind("roles"), this.loadKind("lmaps")]);
      await session.loadCore();
    }, "LDAP sync failed");

    if (ok) docket.commit("LDAP sync complete");
    return ok;
  }
}

export const registry = new Registry();
