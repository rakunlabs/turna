import { isResourceTab, nav, type Tab } from "../navigation";
import { session } from "./session.svelte";
import { registry } from "./registry.svelte";
import { editor } from "./editor.svelte";

/**
 * Hash routing, kept exactly as documented: `#account`, `#api-keys`, and the
 * non-hash `/ui/device` verification page that a device flow links to with a
 * `user_code` query parameter. These URLs are published in the reference docs,
 * so they are contract, not implementation detail.
 */
const SELF_SERVICE: Tab[] = ["account", "device"];

/** Pages that render from info/dashboard alone and must not trigger bulk loads. */
const CORE_ONLY: Tab[] = ["overview", "docs", "encryption"];

class Route {
  tab = $state<Tab>("overview");
  recordID = $state("");
  deviceUserCode = $state("");

  isSelfService(tab: Tab) {
    return SELF_SERVICE.includes(tab);
  }

  allows(tab: Tab) {
    return session.isAdmin || this.isSelfService(tab);
  }

  needsRegistry(tab: Tab) {
    return session.isAdmin && !this.isSelfService(tab) && !CORE_ONLY.includes(tab);
  }

  fallback(): Tab {
    return session.isAdmin ? "overview" : "account";
  }

  /** Read the landing tab from the URL before any request is made. */
  readLocation() {
    // Split on the first slash only, so an unencoded id containing "/" survives.
    const hash = window.location.hash.replace(/^#/, "");
    const slash = hash.indexOf("/");
    const rawTab = slash === -1 ? hash : hash.slice(0, slash);
    const rawRecordID = slash === -1 ? "" : hash.slice(slash + 1);
    const tab = rawTab as Tab;
    if (!rawTab) {
      this.tab = "overview";
      this.recordID = "";
    } else if (nav.some((item) => item.id === tab)) {
      this.tab = tab;
      try {
        this.recordID = this.isResource(tab) ? decodeURIComponent(rawRecordID) : "";
      } catch {
        this.recordID = "";
      }
    } else {
      this.recordID = "";
    }

    // The /ui/device?user_code= link is a landing URL. Consume it once and
    // rewrite to the plain console address, otherwise every later hash change
    // re-reads it and forces the device page again.
    const params = new URLSearchParams(window.location.search);
    const code = params.get("user_code") ?? "";
    const devicePath = /\/ui\/device\/?$/.test(window.location.pathname);
    if (devicePath || code) {
      this.deviceUserCode = code;
      this.tab = "device";
      this.recordID = "";
      params.delete("user_code");
      const query = params.toString();
      const path = devicePath ? window.location.pathname.replace(/device\/?$/, "") : window.location.pathname;
      history.replaceState(history.state, "", `${path}${query ? `?${query}` : ""}#device`);
    }
  }

  /** Drop to an allowed page when capabilities say this one is not ours. */
  enforce() {
    if (this.allows(this.tab)) return;
    this.go(this.fallback());
  }

  go(tab: Tab, onEnter?: (tab: Tab) => void) {
    if (!this.allows(tab)) return;

    this.tab = tab;
    this.recordID = "";
    onEnter?.(tab);

    if (this.needsRegistry(tab)) void registry.ensureLoaded();

    const next = tab === "overview" ? "" : tab;
    if (window.location.hash.replace(/^#/, "") !== next) {
      window.location.hash = next;
    }
  }

  /**
   * The navigation every caller should use. Landing on a record page always
   * shows its register, never a half-filled editor carried over from the page
   * the operator just left.
   */
  select(tab: Tab) {
    this.go(tab, (next) => {
      editor.open = false;
      if (this.isResource(next)) editor.reset(next);
    });
  }

  isResource(tab: Tab) {
    return isResourceTab(tab);
  }
}

export const route = new Route();
