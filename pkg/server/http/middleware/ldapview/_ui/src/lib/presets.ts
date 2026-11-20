import type { Entry } from "./api";

export type Flavor = "ad" | "openldap" | "generic";

export type Preset = { label: string; filter: string; attributes?: string[]; hint?: string };

export function detectFlavor(rootDSE: Entry | undefined): Flavor {
  if (!rootDSE) return "generic";
  const has = (name: string) => rootDSE.attributes.some((a) => a.name.toLowerCase() === name.toLowerCase());
  const values = (name: string) => rootDSE.attributes.find((a) => a.name.toLowerCase() === name.toLowerCase())?.values ?? [];
  if (has("forestFunctionality") || has("domainControllerFunctionality") || values("supportedCapabilities").includes("1.2.840.113556.1.4.800")) return "ad";
  if (values("objectClass").includes("OpenLDAProotDSE")) return "openldap";
  return "generic";
}

export const flavorLabel: Record<Flavor, string> = { ad: "Active Directory", openldap: "OpenLDAP", generic: "LDAP" };

const common: Preset[] = [
  { label: "Everything", filter: "(objectClass=*)" },
  { label: "OUs", filter: "(|(objectClass=organizationalUnit)(objectClass=container))" },
];

export const presets: Record<Flavor, Preset[]> = {
  generic: [
    ...common,
    { label: "People", filter: "(|(objectClass=person)(objectClass=inetOrgPerson)(objectClass=posixAccount))", attributes: ["uid", "cn", "mail"] },
    { label: "Groups", filter: "(|(objectClass=groupOfNames)(objectClass=groupOfUniqueNames)(objectClass=posixGroup))", attributes: ["cn", "description"] },
    { label: "Empty groups", filter: "(&(objectClass=groupOfNames)(!(member=*)))" },
  ],
  openldap: [
    ...common,
    { label: "People", filter: "(|(objectClass=person)(objectClass=inetOrgPerson)(objectClass=posixAccount))", attributes: ["uid", "cn", "mail"] },
    { label: "Groups", filter: "(|(objectClass=groupOfNames)(objectClass=groupOfUniqueNames)(objectClass=posixGroup))", attributes: ["cn", "description"] },
    { label: "POSIX accounts", filter: "(objectClass=posixAccount)", attributes: ["uid", "uidNumber", "gidNumber", "homeDirectory", "loginShell"] },
    { label: "Locked (ppolicy)", filter: "(pwdAccountLockedTime=*)", attributes: ["uid", "pwdAccountLockedTime"] },
    { label: "Changed today", filter: "(modifyTimestamp>={{today}})", attributes: ["modifyTimestamp", "modifiersName"], hint: "Uses operational attributes" },
  ],
  ad: [
    ...common,
    { label: "Users", filter: "(&(objectCategory=person)(objectClass=user))", attributes: ["sAMAccountName", "displayName", "mail", "userAccountControl"] },
    { label: "Groups", filter: "(objectCategory=group)", attributes: ["cn", "groupType", "description"] },
    { label: "Computers", filter: "(objectCategory=computer)", attributes: ["cn", "dNSHostName", "operatingSystem", "lastLogonTimestamp"] },
    { label: "Disabled users", filter: "(&(objectCategory=person)(objectClass=user)(userAccountControl:1.2.840.113556.1.4.803:=2))", attributes: ["sAMAccountName", "userAccountControl"] },
    { label: "Locked out", filter: "(&(objectCategory=person)(objectClass=user)(lockoutTime>=1))", attributes: ["sAMAccountName", "lockoutTime"] },
    { label: "Password never expires", filter: "(&(objectCategory=person)(objectClass=user)(userAccountControl:1.2.840.113556.1.4.803:=65536))", attributes: ["sAMAccountName", "pwdLastSet"] },
    { label: "Admins (adminCount)", filter: "(&(objectCategory=person)(objectClass=user)(adminCount=1))", attributes: ["sAMAccountName", "memberOf"] },
    { label: "With SPN", filter: "(&(objectCategory=person)(objectClass=user)(servicePrincipalName=*))", attributes: ["sAMAccountName", "servicePrincipalName"] },
    { label: "No pre-auth", filter: "(&(objectCategory=person)(objectClass=user)(userAccountControl:1.2.840.113556.1.4.803:=4194304))", attributes: ["sAMAccountName"] },
    { label: "Domain controllers", filter: "(&(objectCategory=computer)(userAccountControl:1.2.840.113556.1.4.803:=8192))", attributes: ["cn", "dNSHostName"] },
    { label: "GPOs", filter: "(objectClass=groupPolicyContainer)", attributes: ["displayName", "gPCFileSysPath"] },
  ],
};

/** Expands {{today}} into a generalized time at local midnight. */
export function expand(filter: string): string {
  const d = new Date();
  d.setHours(0, 0, 0, 0);
  const p = (n: number) => String(n).padStart(2, "0");
  const today = `${d.getUTCFullYear()}${p(d.getUTCMonth() + 1)}${p(d.getUTCDate())}${p(d.getUTCHours())}${p(d.getUTCMinutes())}00Z`;
  return filter.replaceAll("{{today}}", today);
}

/** Filter for the header's quick find box. */
export function quickFind(flavor: Flavor, text: string, escape: (s: string) => string): string {
  const q = escape(text.trim());
  const attrs =
    flavor === "ad"
      ? ["sAMAccountName", "cn", "displayName", "mail", "userPrincipalName"]
      : ["uid", "cn", "mail", "sn", "givenName", "ou", "displayName"];
  return `(|${attrs.map((a) => `(${a}=*${q}*)`).join("")})`;
}
