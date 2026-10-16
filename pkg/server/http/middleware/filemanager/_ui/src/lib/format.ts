export function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  const units = ["KB", "MB", "GB", "TB"];
  let v = bytes / 1024;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v < 10 ? v.toFixed(1) : Math.round(v)} ${units[i]}`;
}

const rtf = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });

export function formatRelative(iso: string, now = Date.now()): string {
  const t = new Date(iso).getTime();
  if (Number.isNaN(t)) return "";
  const s = Math.round((t - now) / 1000);
  const abs = Math.abs(s);
  if (abs < 45) return "just now";
  if (abs < 3600) return rtf.format(Math.round(s / 60), "minute");
  if (abs < 86400) return rtf.format(Math.round(s / 3600), "hour");
  if (abs < 86400 * 30) return rtf.format(Math.round(s / 86400), "day");
  return new Date(t).toLocaleDateString();
}

export function formatDate(iso: string): string {
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? "" : d.toLocaleString();
}

export function parentOf(path: string): string {
  const i = path.lastIndexOf("/");
  return i <= 0 ? "/" : path.slice(0, i);
}

export function baseName(path: string): string {
  return path.slice(path.lastIndexOf("/") + 1);
}

export function joinPath(dir: string, name: string): string {
  const clean = name.replace(/^\/+/, "");
  return dir === "/" ? `/${clean}` : `${dir}/${clean}`;
}

export function extOf(path: string): string {
  const name = baseName(path).toLowerCase();
  const i = name.lastIndexOf(".");
  return i > 0 ? name.slice(i + 1) : "";
}

export function kindLabel(path: string, type?: string): string {
  const ext = extOf(path);
  if (ext) return ext.toUpperCase();
  if (type) return type.split(";")[0];
  return "File";
}

const IMAGE = new Set(["png", "jpg", "jpeg", "jpe", "jfif", "pjpeg", "gif", "webp", "avif", "apng", "svg", "ico", "cur", "bmp"]);
const VIDEO = new Set(["mp4", "m4v", "webm", "mov", "ogv", "mkv", "3gp", "3g2", "mpg", "mpeg"]);
const AUDIO = new Set(["mp3", "wav", "ogg", "oga", "m4a", "m4b", "aac", "flac", "opus", "weba", "mka", "caf"]);
const FONT = new Set(["woff", "woff2", "ttf", "otf"]);
const CSV = new Set(["csv", "tsv", "psv"]);

export type BinaryGroup = "archive" | "document" | "spreadsheet" | "presentation" | "executable" | "database" | "font" | "image" | "data";

const BINARY_GROUPS: Record<BinaryGroup, string[]> = {
  archive: ["zip", "gz", "tgz", "bz2", "tbz2", "xz", "txz", "zst", "lz", "lz4", "lzma", "7z", "rar", "tar", "jar", "war", "ear", "apk", "aab", "ipa", "deb", "rpm", "cab", "whl", "nupkg", "vsix", "crx", "xpi", "snap", "appimage"],
  document: ["doc", "docx", "odt", "rtf", "pages", "epub", "mobi", "azw3", "djvu", "xps"],
  spreadsheet: ["xls", "xlsx", "xlsm", "ods", "numbers"],
  presentation: ["ppt", "pptx", "odp", "key"],
  executable: ["exe", "msi", "dll", "so", "dylib", "bin", "elf", "o", "a", "lib", "obj", "class", "pyc", "pyo", "wasm", "com", "sys", "ko"],
  database: ["db", "sqlite", "sqlite3", "mdb", "accdb", "parquet", "avro", "orc", "feather", "arrow", "h5", "hdf5", "npy", "npz", "pkl", "pickle", "rdb", "dump"],
  font: ["eot", "pfb", "pfm"],
  image: ["heic", "heif", "tif", "tiff", "psd", "ai", "eps", "raw", "cr2", "nef", "arw", "dng", "xcf", "sketch", "fig", "blend", "fbx", "obj3d", "glb", "stl"],
  data: ["dat", "iso", "img", "dmg", "vmdk", "qcow2", "vhd", "vdi", "pem.bin", "der", "p12", "pfx", "jks", "keystore", "gpg", "sig", "swp", "lock.bin"],
};

const BINARY = new Map<string, BinaryGroup>();
for (const [group, exts] of Object.entries(BINARY_GROUPS) as [BinaryGroup, string[]][]) {
  for (const e of exts) BINARY.set(e, group);
}

/** Text files above this size open as a download instead of in the editor. */
export const MAX_EDIT_SIZE = 5 * 1024 * 1024;

/** CSV files up to this size render as a table. */
export const MAX_TABLE_SIZE = 2 * 1024 * 1024;

export type Preview = "text" | "csv" | "image" | "video" | "audio" | "pdf" | "font" | "binary" | "unknown";

/** Decide how to show a file from its name alone; "unknown" needs a content sniff. */
export function previewKind(path: string): Preview {
  const ext = extOf(path);
  if (IMAGE.has(ext)) return "image";
  if (VIDEO.has(ext)) return "video";
  if (AUDIO.has(ext)) return "audio";
  if (ext === "pdf") return "pdf";
  if (FONT.has(ext)) return "font";
  if (CSV.has(ext)) return "csv";
  if (BINARY.has(ext)) return "binary";
  return "unknown";
}

export function binaryGroup(path: string): BinaryGroup | null {
  return BINARY.get(extOf(path)) ?? null;
}

export function isImage(path: string): boolean {
  return IMAGE.has(extOf(path));
}

/** True when the sampled bytes look like text. A cut multi-byte character at the end is fine. */
export function looksLikeText(data: ArrayBuffer): boolean {
  const bytes = new Uint8Array(data);
  if (bytes.includes(0)) return false;
  try {
    new TextDecoder("utf-8", { fatal: true }).decode(bytes.subarray(0, Math.max(0, bytes.length - 4)));
    return true;
  } catch {
    return false;
  }
}

/** Decode bytes as text; null when the content is binary. */
export function decodeText(data: ArrayBuffer): string | null {
  const bytes = new Uint8Array(data);
  const probe = bytes.subarray(0, Math.min(bytes.length, 8000));
  if (probe.includes(0)) return null;
  try {
    return new TextDecoder("utf-8", { fatal: true }).decode(bytes);
  } catch {
    return null;
  }
}
