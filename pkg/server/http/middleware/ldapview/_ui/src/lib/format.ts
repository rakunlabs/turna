import type { Entry } from "./api";

// LDIF (RFC 2849) needs base64 for values that start unsafe or hold non ASCII.
function safe(v: string): boolean {
  if (v === "") return true;
  if (/^[ :<]/.test(v) || v.endsWith(" ")) return false;
  return /^[\x01-\x09\x0b\x0c\x0e-\x7f]*$/.test(v);
}

function b64(v: string): string {
  const bytes = new TextEncoder().encode(v);
  let s = "";
  for (const b of bytes) s += String.fromCharCode(b);
  return btoa(s);
}

function line(name: string, value: string, binary = false): string {
  if (binary) return `${name}:: ${value}`;
  return safe(value) ? `${name}: ${value}` : `${name}:: ${b64(value)}`;
}

export function toLDIF(entries: Entry[]): string {
  return entries
    .map((e) => {
      const lines = [line("dn", e.dn)];
      for (const a of e.attributes) {
        if (a.values.length === 0) lines.push(`${a.name}:`);
        for (const v of a.values) lines.push(line(a.name, v, a.binary));
      }
      return lines.join("\n");
    })
    .join("\n\n");
}

export function toJSON(entries: Entry[]): string {
  return JSON.stringify(
    entries.map((e) => ({
      dn: e.dn,
      attributes: Object.fromEntries(e.attributes.map((a) => [a.name, a.values])),
    })),
    null,
    2,
  );
}

export function toCSV(entries: Entry[]): string {
  const cols: string[] = [];
  const seen = new Set<string>();
  for (const e of entries)
    for (const a of e.attributes)
      if (!seen.has(a.name.toLowerCase())) {
        seen.add(a.name.toLowerCase());
        cols.push(a.name);
      }
  const esc = (v: string) => (/[",\n\r]/.test(v) ? `"${v.replaceAll('"', '""')}"` : v);
  const rows = entries.map((e) => {
    const by = new Map(e.attributes.map((a) => [a.name.toLowerCase(), a.values.join("; ")]));
    return [e.dn, ...cols.map((c) => by.get(c.toLowerCase()) ?? "")].map(esc).join(",");
  });
  return [["dn", ...cols].map(esc).join(","), ...rows].join("\n");
}

export function download(name: string, text: string, type: string) {
  const a = document.createElement("a");
  a.href = URL.createObjectURL(new Blob([text], { type }));
  a.download = name;
  a.click();
  setTimeout(() => URL.revokeObjectURL(a.href), 1000);
}

/** Parent DN, or "" for a naming context. Splits on unescaped commas. */
export function parentDN(dn: string): string {
  for (let i = 0; i < dn.length; i++) {
    if (dn[i] === "\\") i++;
    else if (dn[i] === ",") return dn.slice(i + 1).trim();
  }
  return "";
}

export function rdn(dn: string): string {
  const p = parentDN(dn);
  return p ? dn.slice(0, dn.length - p.length).replace(/,\s*$/, "") : dn;
}

export function escapeFilter(v: string): string {
  return v.replace(/[\\*()\0]/g, (c) => "\\" + c.charCodeAt(0).toString(16).padStart(2, "0"));
}

export function isDN(v: string): boolean {
  return /^[A-Za-z][\w-]*=[^,]+(,\s*[A-Za-z][\w-]*=[^,]+)*$/.test(v) && v.includes("=");
}

export function timeAgo(ts: number): string {
  const s = Math.round((Date.now() - ts) / 1000);
  if (s < 60) return "just now";
  if (s < 3600) return `${Math.floor(s / 60)}m ago`;
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`;
  return new Date(ts).toLocaleDateString();
}

/** Pretty prints a filter with one clause per line, as moribito does. */
export function prettyFilter(f: string): string {
  f = compactFilter(f);
  let out = "";
  let depth = 0;
  for (let i = 0; i < f.length; i++) {
    const c = f[i];
    if (c === "\\") {
      out += c + (f[++i] ?? "");
    } else if (c === "(") {
      const op = f[i + 1];
      if (out) out += "\n" + "  ".repeat(depth);
      out += c;
      if (op === "&" || op === "|" || op === "!") {
        out += op;
        i++;
        depth++;
      } else {
        // a plain clause stays on one line
        let j = i + 1;
        for (; j < f.length && f[j] !== ")"; j++) if (f[j] === "\\") j++;
        out += f.slice(i + 1, j + 1);
        i = j;
      }
    } else if (c === ")") {
      depth = Math.max(0, depth - 1);
      out += "\n" + "  ".repeat(depth) + c;
    } else {
      out += c;
    }
  }
  return out;
}

export function compactFilter(f: string): string {
  return f.replace(/\s*\n\s*/g, "").trim();
}
