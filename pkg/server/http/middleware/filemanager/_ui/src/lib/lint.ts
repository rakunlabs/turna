import type { Text } from "@codemirror/state";

export type Problem = {
  from: number;
  to: number;
  severity: "error" | "warning";
  message: string;
};

/** Languages with a parser behind them; the editor lints and formats only these. */
export const checkable = new Set(["json", "yaml"]);

/** JSON with comments is common in jsonc/json5 files, so they are not linted as strict JSON. */
export function lintLanguage(lang: string, path: string): string | null {
  if (lang === "json" && /\.(jsonc|json5)$/i.test(path)) return null;
  return checkable.has(lang) ? lang : null;
}

export async function diagnose(lang: string | null, text: string, doc?: Text): Promise<Problem[]> {
  if (!lang || !text.trim()) return [];
  if (lang === "json") return lintJSON(text, doc);
  if (lang === "yaml") return lintYAML(text);
  return [];
}

/** Returns the formatted text, or throws with a readable message. */
export async function formatText(lang: string, text: string): Promise<string> {
  if (lang === "json") return formatJSON(text);
  if (lang === "yaml") {
    const { parseAllDocuments } = await import("yaml");
    // failsafe keeps every scalar as written: 010, 1.10, 0x1F and big numbers stay untouched
    const docs = parseAllDocuments(text, { prettyErrors: false, schema: "failsafe" });
    const list = Array.isArray(docs) ? docs : [docs];
    const bad = list.flatMap((d) => d.errors);
    if (bad.length) throw new Error(bad[0].message.split("\n")[0]);
    // lineWidth 0 crashes on !!binary; a huge width turns folding off just the same
    return list.map((d) => d.toString({ indent: 2, lineWidth: Number.MAX_SAFE_INTEGER, minContentWidth: 0 })).join("");
  }
  throw new Error("No formatter for this language");
}

/**
 * Re-indents JSON without parsing values, so numbers keep their exact digits
 * (no precision loss on big IDs, 1.0 stays 1.0) and duplicate keys survive.
 */
export function formatJSON(text: string): string {
  const err = scanJSON(text);
  if (err) throw new Error(err.message);
  let out = "";
  let depth = 0;
  let i = 0;
  const nl = () => "\n" + "  ".repeat(depth);
  while (i < text.length) {
    const c = text[i];
    if (c === " " || c === "\t" || c === "\n" || c === "\r") {
      i++;
      continue;
    }
    if (c === '"') {
      let j = i + 1;
      while (text[j] !== '"') j += text[j] === "\\" ? 2 : 1;
      out += text.slice(i, j + 1);
      i = j + 1;
      continue;
    }
    if (c === "{" || c === "[") {
      const close = c === "{" ? "}" : "]";
      let j = i + 1;
      while (/\s/.test(text[j] ?? "")) j++;
      if (text[j] === close) {
        out += c + close;
        i = j + 1;
        continue;
      }
      depth++;
      out += c + nl();
      i++;
      continue;
    }
    if (c === "}" || c === "]") {
      depth--;
      out += nl() + c;
      i++;
      continue;
    }
    if (c === ",") {
      out += "," + nl();
      i++;
      continue;
    }
    if (c === ":") {
      out += ": ";
      i++;
      continue;
    }
    let j = i;
    while (j < text.length && !/[\s,:{}[\]"]/.test(text[j])) j++;
    out += text.slice(i, j);
    i = j;
  }
  return out + "\n";
}

function lintJSON(text: string, doc?: Text): Problem[] {
  try {
    JSON.parse(text);
    return [];
  } catch (e) {
    if (!(e instanceof SyntaxError)) throw e;
    // V8 often names no position, so find it ourselves; fall back to the engine's hints
    const found = scanJSON(text);
    if (found) return [{ ...found, severity: "error" }];
    const pos = jsonErrorPosition(e.message, text, doc);
    return [{ from: pos, to: Math.min(pos + 1, text.length), severity: "error", message: cleanJSONMessage(e.message) }];
  }
}

/** Finds the first syntax error in JSON text, or null when it parses. */
function scanJSON(s: string): { from: number; to: number; message: string } | null {
  let i = 0;
  type Fail = { fail: true; at: number; message: string };
  const fail = (message: string, at = i): never => {
    throw { fail: true, at, message } satisfies Fail;
  };
  const ws = () => {
    while (i < s.length && (s[i] === " " || s[i] === "\t" || s[i] === "\n" || s[i] === "\r")) i++;
  };
  const describe = () => (i >= s.length ? "Unexpected end of input" : `Unexpected ${JSON.stringify(s[i])}`);
  const value = (): void => {
    ws();
    const c = s[i];
    if (c === "{") {
      i++;
      ws();
      if (s[i] === "}") return void i++;
      for (;;) {
        ws();
        if (s[i] !== '"') fail(s[i] === "}" ? "Trailing comma before }" : `${describe()}, expected a property name in double quotes`);
        string();
        ws();
        if (s[i] !== ":") fail(`${describe()}, expected ':' after property name`);
        i++;
        value();
        ws();
        if (s[i] === ",") {
          i++;
          continue;
        }
        if (s[i] === "}") return void i++;
        fail(`${describe()}, expected ',' or '}'`);
      }
    }
    if (c === "[") {
      i++;
      ws();
      if (s[i] === "]") return void i++;
      for (;;) {
        ws();
        if (s[i] === "]") fail("Trailing comma before ]");
        value();
        ws();
        if (s[i] === ",") {
          i++;
          continue;
        }
        if (s[i] === "]") return void i++;
        fail(`${describe()}, expected ',' or ']'`);
      }
    }
    if (c === '"') return string();
    if (c === "-" || (c >= "0" && c <= "9")) return number();
    for (const word of ["true", "false", "null"]) {
      if (s.startsWith(word, i)) {
        i += word.length;
        return;
      }
    }
    if (c === "'") fail("Strings must use double quotes");
    if (c === "/") fail("Comments are not allowed in JSON");
    fail(i >= s.length ? "Unexpected end of input, expected a value" : `${describe()}, expected a value`);
  };
  const string = () => {
    const start = i++;
    while (i < s.length) {
      const c = s[i];
      if (c === '"') return void i++;
      if (c === "\\") {
        const n = s[i + 1];
        if (n === "u") {
          if (!/^[0-9a-fA-F]{4}$/.test(s.slice(i + 2, i + 6))) fail("Invalid \\u escape");
          i += 6;
        } else if (n !== undefined && '"\\/bfnrt'.includes(n)) i += 2;
        else fail("Invalid escape in string");
        continue;
      }
      if (c < " ") fail(c === "\n" ? "Unterminated string" : "Control character in string");
      i++;
    }
    fail("Unterminated string", start);
  };
  const number = () => {
    const m = /^-?(0|[1-9]\d*)(\.\d+)?([eE][+-]?\d+)?/.exec(s.slice(i));
    if (!m) fail("Invalid number");
    i += m![0].length;
  };
  try {
    value();
    ws();
    if (i < s.length) fail(`${describe()} after the end of the value`);
    return null;
  } catch (e) {
    if (!(e as Fail)?.fail) throw e;
    const from = Math.min((e as Fail).at, s.length);
    return { from, to: Math.min(from + 1, s.length), message: (e as Fail).message };
  }
}

function jsonErrorPosition(message: string, text: string, doc?: Text): number {
  let m = message.match(/at position (\d+)/);
  if (m) return Math.min(+m[1], text.length);
  m = message.match(/line (\d+) column (\d+)/);
  if (m) {
    const line = +m[1];
    const col = +m[2];
    if (doc && line <= doc.lines) return Math.min(doc.line(line).from + col - 1, text.length);
    const lines = text.split("\n");
    let pos = 0;
    for (let i = 0; i < line - 1 && i < lines.length; i++) pos += lines[i].length + 1;
    return Math.min(pos + col - 1, text.length);
  }
  if (/end of (JSON|data) input/i.test(message)) return text.length;
  return 0;
}

function cleanJSONMessage(message: string): string {
  return message
    .replace(/^JSON\.parse: /, "")
    .replace(/, ".*" is not valid JSON$/s, "")
    .replace(/ in JSON at position \d+.*$/, "")
    .replace(/ at line \d+ column \d+ of the JSON data$/, "")
    .replace(/ \(line \d+ column \d+\)$/, "");
}

async function lintYAML(text: string): Promise<Problem[]> {
  const { parseAllDocuments } = await import("yaml");
  const docs = parseAllDocuments(text, { prettyErrors: false, uniqueKeys: true });
  const list = Array.isArray(docs) ? docs : [docs];
  const out: Problem[] = [];
  for (const d of list) {
    for (const e of d.errors) out.push(yamlProblem(e, "error", text.length));
    // custom tags (!Ref, !GetAtt, !vault) are valid YAML that only the consuming tool resolves
    for (const w of d.warnings) if (!w.message.startsWith("Unresolved tag")) out.push(yamlProblem(w, "warning", text.length));
  }
  return out;
}

function yamlProblem(e: { pos: [number, number]; message: string }, severity: Problem["severity"], len: number): Problem {
  const from = Math.min(e.pos[0], len);
  const to = Math.min(Math.max(e.pos[1], from), len);
  return { from, to, severity, message: e.message.split("\n")[0] };
}
