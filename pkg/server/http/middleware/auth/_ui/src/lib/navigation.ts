import type { ResourceKind } from "./api";

export type Tab = "overview" | "check" | "flows" | "oauth2-overview" | "docs" | "account" | "api-keys" | "device" | "email" | "magic-link" | "signup" | "mtls" | "encryption" | "admin" | "cache" | "device-settings" | "token-exchange" | "totp" | "custom-info" | ResourceKind;

export type NavItem = { id: Tab; label: string };

export type NavGroup = { label: string; items: NavItem[] };

export const navGroups: NavGroup[] = [
  {
    label: "CONTROL",
    items: [
      { id: "overview", label: "OVERVIEW" },
      { id: "flows", label: "FLOWS" },
      { id: "check", label: "ACCESS CHECK" },
    ],
  },
  {
    label: "SELF SERVICE",
    items: [
      { id: "account", label: "MY ACCOUNT" },
      { id: "device", label: "DEVICE LOGIN" },
    ],
  },
  {
    label: "IAM",
    items: [
      { id: "users", label: "USERS" },
      { id: "service-accounts", label: "SERVICE ACCTS" },
      { id: "roles", label: "ROLES" },
      { id: "permissions", label: "PERMISSIONS" },
    ],
  },
  {
    label: "LDAP",
    items: [
      { id: "lmaps", label: "GROUP MAPS" },
      { id: "ldap", label: "LDAP CONFIGS" },
    ],
  },
  {
    label: "FEDERATION",
    items: [
      { id: "oauth2-overview", label: "OAUTH2 OVERVIEW" },
      { id: "clients", label: "SERVER CLIENTS" },
      { id: "providers", label: "OAUTH PROVIDERS" },
      { id: "saml", label: "SAML PROVIDERS" },
    ],
  },
  {
    label: "SYSTEM",
    items: [
      { id: "api-keys", label: "API KEYS" },
      { id: "email", label: "EMAIL" },
      { id: "magic-link", label: "MAGIC LINK" },
      { id: "signup", label: "SIGNUP" },
      { id: "mtls", label: "MTLS" },
      { id: "totp", label: "TOTP" },
      { id: "custom-info", label: "CUSTOM INFO" },
      { id: "device-settings", label: "DEVICE FLOW" },
      { id: "token-exchange", label: "TOKEN EXCHANGE" },
    ],
  },
  {
    label: "PLATFORM",
    items: [
      { id: "admin", label: "ADMIN" },
      { id: "cache", label: "CACHE" },
      { id: "encryption", label: "ENCRYPTION" },
      { id: "docs", label: "DOCS" },
    ],
  },
];

export const nav = navGroups.flatMap((group) => group.items);

export function isResourceTab(tab: Tab): tab is ResourceKind {
  return !["overview", "check", "flows", "oauth2-overview", "docs", "account", "api-keys", "device", "email", "magic-link", "signup", "mtls", "encryption", "admin", "cache", "device-settings", "token-exchange", "totp", "custom-info"].includes(tab);
}
