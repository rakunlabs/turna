import { editableSettingNamespaces, kindSpecs, rowFromItem, type AnyRecord, type ResourceKind, type Row } from "../api";
import { docket, session } from "./session.svelte";
import { settings } from "./settings.svelte";

/** Rows fetched per page on the paginated (IAM) registers. */
export const PAGE_SIZE = 50;

/** Where a paginated register currently stands: which page, which search, how many records exist. */
export type ListStanding = {
  offset: number;
  search: string;
  /** Extra server-side filters (param → value), e.g. path/method/description on permissions. */
  filters: Record<string, string>;
  total: number;
};

const restingStanding: ListStanding = { offset: 0, search: "", filters: {}, total: 0 };

/** Two filter sets ask the server the same question iff every param and value agrees. */
const sameFilters = (a: Record<string, string>, b: Record<string, string>) => {
  const aKeys = Object.keys(a);
  return aKeys.length === Object.keys(b).length && aKeys.every((key) => a[key] === b[key]);
};

/**
 * The registry is the console's index of issued instruments: one row list per
 * record kind, plus the published signing keys. Admin bulk data is deferred
 * until a page needs it so a self-service visit stays quiet.
 *
 * The IAM registers (users, service accounts, roles, permissions) can hold
 * thousands of records, so they are read one page at a time and searched on
 * the server; everything else is short enough to fetch whole.
 */
class Registry {
  rowsByKind = $state<Partial<Record<ResourceKind, Row[]>>>({});
  standingByKind = $state<Partial<Record<ResourceKind, ListStanding>>>({});
  jwks = $state<AnyRecord[]>([]);
  loaded = $state(false);

  rows(kind: ResourceKind) {
    return this.rowsByKind[kind] ?? [];
  }

  standing(kind: ResourceKind): ListStanding {
    return this.standingByKind[kind] ?? restingStanding;
  }

  signingKey = $derived(this.jwks[0] ?? ({} as AnyRecord));
  ldapActive = $derived(this.rows("ldap").some((row) => row.enabled));
  providerCount = $derived(this.rows("providers").length);
  samlCount = $derived(this.rows("saml").length);

  async loadKind(kind: ResourceKind) {
    const spec = kindSpecs[kind];
    let query = "";

    if (spec.paginated) {
      const standing = this.standing(kind);
      const params = new URLSearchParams();

      // Identity lists skip role expansion so the index stays light.
      if (kind === "users" || kind === "service-accounts") params.set("add_roles", "false");
      params.set("_limit", String(PAGE_SIZE));
      if (standing.offset > 0) params.set("_offset", String(standing.offset));
      if (standing.search) params.set("search", standing.search);
      for (const [param, value] of Object.entries(standing.filters ?? {})) {
        if (value) params.set(param, value);
      }

      query = `?${params.toString()}`;
    } else if (kind === "lmaps") {
      query = "?_limit=0";
    }

    const res = await session.request<Record<string, unknown>[]>(`${spec.listPath}${query}`);

    let rows = (res.payload ?? []).map((item) => rowFromItem(kind, item));
    if (kind === "settings") {
      rows = rows.filter((row) => (editableSettingNamespaces as readonly string[]).includes(row.id));
    }

    this.rowsByKind = { ...this.rowsByKind, [kind]: rows };

    if (spec.paginated) {
      const standing = this.standing(kind);
      const total = res.meta?.total_item_count ?? 0;

      // A delete can strand the offset past the end (an empty page with
      // records still on file). Step back onto the last real page.
      if (total > 0 && standing.offset >= total) {
        const offset = Math.floor((total - 1) / PAGE_SIZE) * PAGE_SIZE;
        this.standingByKind = { ...this.standingByKind, [kind]: { ...standing, offset, total } };
        await this.loadKind(kind);
        return;
      }

      this.standingByKind = { ...this.standingByKind, [kind]: { ...standing, total } };
    }
  }

  /** Turn to a page of a paginated register. Offsets are clamped at zero. */
  async turnPage(kind: ResourceKind, offset: number) {
    const standing = this.standing(kind);
    this.standingByKind = {
      ...this.standingByKind,
      [kind]: { ...standing, offset: Math.max(0, offset) },
    };

    await session.run(() => this.loadKind(kind));
  }

  /** Apply a register's server-side search and filters together; resets to page one. */
  async applyQuery(kind: ResourceKind, search: string, filters: Record<string, string>) {
    const standing = this.standing(kind);
    if (
      standing.search === search &&
      sameFilters(standing.filters ?? {}, filters) &&
      standing.offset === 0
    ) {
      return;
    }

    this.standingByKind = {
      ...this.standingByKind,
      [kind]: { ...standing, search, filters, offset: 0 },
    };
    await session.run(() => this.loadKind(kind));
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
