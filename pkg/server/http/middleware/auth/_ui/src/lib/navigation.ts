import type { ResourceKind } from "./api";

export type Tab =
  | "overview"
  | "check"
  | "flows"
  | "oauth2-overview"
  | "docs"
  | "account"
  | "api-keys"
  | "device"
  | "email"
  | "magic-link"
  | "signup"
  | "mtls"
  | "encryption"
  | "admin"
  | "cache"
  | "device-settings"
  | "token-exchange"
  | "totp"
  | "custom-info"
  | "session-providers"
  | ResourceKind;

export type NavItem = { id: Tab; label: string };

export type NavGroup = { label: string; items: NavItem[] };

/**
 * The seven groups are the operator's existing map and stay exactly as they
 * were. Labels are set in sentence case because the index renders them as text
 * at reading size; the engraved caps register belongs to the group headings.
 */
export const navGroups: NavGroup[] = [
  {
    label: "Control",
    items: [
      { id: "overview", label: "Overview" },
      { id: "flows", label: "Flows" },
      { id: "check", label: "Access check" },
    ],
  },
  {
    label: "Self service",
    items: [
      { id: "account", label: "My account" },
      { id: "device", label: "Device login" },
    ],
  },
  {
    label: "IAM",
    items: [
      { id: "users", label: "Users" },
      { id: "service-accounts", label: "Service accounts" },
      { id: "roles", label: "Roles" },
      { id: "permissions", label: "Permissions" },
    ],
  },
  {
    label: "LDAP",
    items: [
      { id: "lmaps", label: "Group maps" },
      { id: "ldap", label: "LDAP configs" },
    ],
  },
  {
    label: "Federation",
    items: [
      { id: "oauth2-overview", label: "OAuth2 overview" },
      { id: "clients", label: "Server clients" },
      { id: "providers", label: "OAuth providers" },
      { id: "saml", label: "SAML providers" },
      { id: "session-providers", label: "Session providers" },
    ],
  },
  {
    label: "System",
    items: [
      { id: "api-keys", label: "API keys" },
      { id: "email", label: "Email" },
      { id: "magic-link", label: "Magic link" },
      { id: "signup", label: "Signup" },
      { id: "mtls", label: "mTLS" },
      { id: "totp", label: "TOTP" },
      { id: "custom-info", label: "Custom info" },
      { id: "device-settings", label: "Device flow" },
      { id: "token-exchange", label: "Token exchange" },
    ],
  },
  {
    label: "Platform",
    items: [
      { id: "admin", label: "Admin" },
      { id: "cache", label: "Cache" },
      { id: "encryption", label: "Encryption" },
      { id: "docs", label: "Docs" },
    ],
  },
];

export const nav = navGroups.flatMap((group) => group.items);

const FIXED_TABS: Tab[] = [
  "overview",
  "check",
  "flows",
  "oauth2-overview",
  "docs",
  "account",
  "api-keys",
  "device",
  "email",
  "magic-link",
  "signup",
  "mtls",
  "encryption",
  "admin",
  "cache",
  "device-settings",
  "token-exchange",
  "totp",
  "custom-info",
  "session-providers",
];

/** Resource tabs are the ones backed by a generic record list plus editor. */
export function isResourceTab(tab: Tab): tab is ResourceKind {
  return !FIXED_TABS.includes(tab);
}

export function labelOf(tab: Tab) {
  return nav.find((item) => item.id === tab)?.label ?? tab;
}
