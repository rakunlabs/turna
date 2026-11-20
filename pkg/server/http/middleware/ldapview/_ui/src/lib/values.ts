// Human readable forms of common LDAP and Active Directory values.

const FILETIME_ATTRS = new Set(
  ["pwdLastSet", "lastLogon", "lastLogonTimestamp", "lastLogoff", "accountExpires", "badPasswordTime", "lockoutTime", "msDS-UserPasswordExpiryTimeComputed", "creationTime"].map((s) => s.toLowerCase()),
);

// AD stores these as negative 100ns intervals.
const INTERVAL_ATTRS = new Set(["maxPwdAge", "minPwdAge", "lockoutDuration", "lockOutObservationWindow", "forceLogoff"].map((s) => s.toLowerCase()));

// Days since 1970 (shadowAccount).
const DAY_ATTRS = new Set(["shadowLastChange", "shadowExpire"].map((s) => s.toLowerCase()));

const UAC: [number, string][] = [
  [0x0001, "SCRIPT"],
  [0x0002, "ACCOUNTDISABLE"],
  [0x0008, "HOMEDIR_REQUIRED"],
  [0x0010, "LOCKOUT"],
  [0x0020, "PASSWD_NOTREQD"],
  [0x0040, "PASSWD_CANT_CHANGE"],
  [0x0080, "ENCRYPTED_TEXT_PWD_ALLOWED"],
  [0x0100, "TEMP_DUPLICATE_ACCOUNT"],
  [0x0200, "NORMAL_ACCOUNT"],
  [0x0800, "INTERDOMAIN_TRUST_ACCOUNT"],
  [0x1000, "WORKSTATION_TRUST_ACCOUNT"],
  [0x2000, "SERVER_TRUST_ACCOUNT"],
  [0x10000, "DONT_EXPIRE_PASSWORD"],
  [0x20000, "MNS_LOGON_ACCOUNT"],
  [0x40000, "SMARTCARD_REQUIRED"],
  [0x80000, "TRUSTED_FOR_DELEGATION"],
  [0x100000, "NOT_DELEGATED"],
  [0x200000, "USE_DES_KEY_ONLY"],
  [0x400000, "DONT_REQ_PREAUTH"],
  [0x800000, "PASSWORD_EXPIRED"],
  [0x1000000, "TRUSTED_TO_AUTH_FOR_DELEGATION"],
  [0x4000000, "PARTIAL_SECRETS_ACCOUNT"],
];

const GROUP_TYPE: [number, string][] = [
  [0x1, "BUILTIN"],
  [0x2, "GLOBAL"],
  [0x4, "DOMAIN_LOCAL"],
  [0x8, "UNIVERSAL"],
  [0x80000000, "SECURITY"],
];

const SAM_ACCOUNT_TYPE: Record<number, string> = {
  0x0: "DOMAIN_OBJECT",
  0x10000000: "GROUP_OBJECT",
  0x10000001: "NON_SECURITY_GROUP",
  0x20000000: "ALIAS_OBJECT",
  0x20000001: "NON_SECURITY_ALIAS",
  0x30000000: "USER_OBJECT",
  0x30000001: "MACHINE_ACCOUNT",
  0x30000002: "TRUST_ACCOUNT",
  0x40000000: "APP_BASIC_GROUP",
  0x40000001: "APP_QUERY_GROUP",
};

const dateFmt = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "medium" });

function date(d: Date): string {
  return isNaN(d.getTime()) ? "" : dateFmt.format(d);
}

function flags(v: number, table: [number, string][]): string {
  const out = table.filter(([bit]) => (v & bit) >>> 0 === bit).map(([, n]) => n);
  return out.length ? out.join(" | ") : "none";
}

function bytes(b64: string): Uint8Array {
  const s = atob(b64);
  const out = new Uint8Array(s.length);
  for (let i = 0; i < s.length; i++) out[i] = s.charCodeAt(i);
  return out;
}

function hex(b: Uint8Array, from: number, to: number, reverse = false): string {
  const part = Array.from(b.slice(from, to));
  if (reverse) part.reverse();
  return part.map((x) => x.toString(16).padStart(2, "0")).join("");
}

export function guid(b64: string): string | null {
  const b = bytes(b64);
  if (b.length !== 16) return null;
  return `${hex(b, 0, 4, true)}-${hex(b, 4, 6, true)}-${hex(b, 6, 8, true)}-${hex(b, 8, 10)}-${hex(b, 10, 16)}`;
}

export function sid(b64: string): string | null {
  const b = bytes(b64);
  if (b.length < 8 || b[0] !== 1) return null;
  const count = b[1];
  if (b.length !== 8 + count * 4) return null;
  let auth = 0;
  for (let i = 2; i < 8; i++) auth = auth * 256 + b[i];
  const parts = [`S-1-${auth}`];
  const view = new DataView(b.buffer);
  for (let i = 0; i < count; i++) parts.push(String(view.getUint32(8 + i * 4, true)));
  return parts.join("-");
}

const WELL_KNOWN_RID: Record<string, string> = {
  "500": "Administrator",
  "501": "Guest",
  "502": "krbtgt",
  "512": "Domain Admins",
  "513": "Domain Users",
  "514": "Domain Guests",
  "515": "Domain Computers",
  "516": "Domain Controllers",
  "518": "Schema Admins",
  "519": "Enterprise Admins",
  "520": "Group Policy Creator Owners",
};

function generalizedTime(v: string): Date | null {
  const m = /^(\d{4})(\d{2})(\d{2})(\d{2})(\d{2})(\d{2})?(?:[.,](\d+))?(Z|[+-]\d{2}(?:\d{2})?)$/.exec(v);
  if (!m) return null;
  const [, y, mo, d, h, mi, s = "00", frac = "0", tz] = m;
  const ms = Math.round(Number("0." + frac) * 1000);
  const iso = `${y}-${mo}-${d}T${h}:${mi}:${s}.${String(ms).padStart(3, "0")}${tz === "Z" ? "Z" : tz.length === 3 ? tz + ":00" : tz.slice(0, 3) + ":" + tz.slice(3)}`;
  const date = new Date(iso);
  return isNaN(date.getTime()) ? null : date;
}

function duration(ns100: bigint): string {
  let s = Number(-ns100 / 10_000_000n);
  if (s <= 0) return "none";
  const parts: string[] = [];
  for (const [n, u] of [[86400, "d"], [3600, "h"], [60, "m"], [1, "s"]] as const) {
    if (s >= n) {
      parts.push(`${Math.floor(s / n)}${u}`);
      s %= n;
    }
  }
  return parts.join(" ");
}

/**
 * Readable text for one value, or null when the raw value is already the best
 * form. Binary values arrive base64-encoded.
 */
export function readable(name: string, value: string, binary = false): string | null {
  const n = name.toLowerCase();
  try {
    if (binary) {
      if (n === "objectguid" || n.endsWith("guid")) return guid(value);
      if (n === "objectsid" || n === "securityidentifier" || n === "sidhistory" || n === "tokengroups") {
        const s = sid(value);
        if (!s) return null;
        const rid = s.split("-").pop()!;
        return WELL_KNOWN_RID[rid] ? `${s} (${WELL_KNOWN_RID[rid]})` : s;
      }
      return null;
    }

    if (FILETIME_ATTRS.has(n) && /^-?\d+$/.test(value)) {
      const v = BigInt(value);
      if (v === 0n || v === 9223372036854775807n) return "never";
      return date(new Date(Number(v / 10_000n - 11644473600000n)));
    }

    if (INTERVAL_ATTRS.has(n) && /^-\d+$/.test(value)) return duration(BigInt(value));

    if (DAY_ATTRS.has(n) && /^\d+$/.test(value)) return date(new Date(Number(value) * 86400000)).replace(/,? \d.*$/, "");

    if (n === "useraccountcontrol" || n === "msds-user-account-control-computed") return flags(Number(value), UAC);
    if (n === "grouptype") return flags(Number(value) >>> 0, GROUP_TYPE);
    if (n === "samaccounttype") return SAM_ACCOUNT_TYPE[Number(value)] ?? null;

    if (/^\d{12,14}(?:[.,]\d+)?(Z|[+-]\d{2,4})$/.test(value)) {
      const d = generalizedTime(value);
      if (d) return date(d);
    }
  } catch {
    return null;
  }
  return null;
}

/** AD accounts with ACCOUNTDISABLE set, shown dimmed in lists. */
export function disabled(attrs: { name: string; values: string[] }[]): boolean {
  const uac = attrs.find((a) => a.name.toLowerCase() === "useraccountcontrol")?.values[0];
  if (uac && (Number(uac) & 2) === 2) return true;
  const locked = attrs.find((a) => a.name.toLowerCase() === "pwdaccountlockedtime");
  return !!locked?.values.length;
}
