/** RFC 4180 style parser: quoted fields, doubled quotes, newlines inside quotes. */
export function parseDelimited(text: string, delimiter: string, maxRows: number): { rows: string[][]; truncated: boolean } {
  const rows: string[][] = [];
  let row: string[] = [];
  let field = "";
  let quoted = false;
  let i = 0;
  const n = text.length;
  if (text.charCodeAt(0) === 0xfeff) i = 1;

  const endRow = () => {
    row.push(field);
    field = "";
    if (!(row.length === 1 && row[0] === "")) rows.push(row);
    row = [];
  };

  while (i < n) {
    const c = text[i];
    if (quoted) {
      if (c === '"') {
        if (text[i + 1] === '"') {
          field += '"';
          i += 2;
          continue;
        }
        quoted = false;
        i++;
        continue;
      }
      field += c;
      i++;
      continue;
    }
    if (c === '"' && field === "") {
      quoted = true;
      i++;
    } else if (c === delimiter) {
      row.push(field);
      field = "";
      i++;
    } else if (c === "\n" || c === "\r") {
      endRow();
      if (c === "\r" && text[i + 1] === "\n") i++;
      i++;
      if (rows.length >= maxRows) return { rows, truncated: i < n };
    } else {
      field += c;
      i++;
    }
  }
  if (field !== "" || row.length) endRow();
  return { rows, truncated: false };
}

export function delimiterFor(path: string, sample: string): string {
  const ext = path.slice(path.lastIndexOf(".") + 1).toLowerCase();
  if (ext === "tsv") return "\t";
  if (ext === "psv") return "|";
  // a ";" separated .csv is common in locales that use "," as the decimal mark
  const firstLine = sample.slice(0, sample.indexOf("\n") >>> 0);
  const count = (d: string) => firstLine.split(d).length - 1;
  return count(";") > count(",") ? ";" : ",";
}
