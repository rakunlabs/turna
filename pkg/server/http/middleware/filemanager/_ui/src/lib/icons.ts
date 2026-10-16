import {
  File,
  FileArchive,
  FileAudio,
  FileBox,
  FileChartColumn,
  FileCode,
  FileCog,
  FileImage,
  FileJson,
  FileKey,
  FileLock,
  FileSliders,
  FileSpreadsheet,
  FileTerminal,
  FileText,
  FileType,
  FileVideo,
  Database,
} from "@lucide/svelte";
import { binaryGroup, extOf, previewKind } from "./format";
import { detectLanguage } from "./languages";

const CODE = new Set([
  "javascript", "typescript", "html", "vue", "svelte", "css", "scss", "less", "go", "python", "rust", "java",
  "kotlin", "scala", "groovy", "c", "cpp", "csharp", "objectivec", "swift", "dart", "php", "ruby", "perl",
  "lua", "r", "julia", "haskell", "elixir", "clojure", "ocaml", "elm", "crystal", "d", "pascal", "fortran",
  "vb", "asm", "verilog", "vhdl", "wasm", "sql", "graphql", "protobuf", "cypher", "sparql", "jinja", "pug",
  "stylus", "latex", "xml",
]);

const CONFIG = new Set(["yaml", "toml", "properties", "nginx", "hcl", "ignore", "makefile", "cmake", "dockerfile"]);
const KEYS = new Set(["pem", "key", "crt", "cer", "csr", "pub", "der", "p12", "pfx", "jks", "keystore", "asc", "gpg", "sig"]);
const LOCKS = /(^|\.)(lock|lockb)$|^(package-lock\.json|yarn\.lock|pnpm-lock\.yaml|go\.sum|cargo\.lock|poetry\.lock|composer\.lock|gemfile\.lock)$/;

/** One icon per kind of file, shared by the tree and the folder table. */
export function fileIcon(name: string) {
  const lower = name.toLowerCase();
  const ext = extOf(name);
  if (LOCKS.test(lower)) return FileLock;
  if (KEYS.has(ext)) return FileKey;

  switch (previewKind(name)) {
    case "image":
      return FileImage;
    case "video":
      return FileVideo;
    case "audio":
      return FileAudio;
    case "pdf":
      return FileText;
    case "font":
      return FileType;
    case "csv":
      return FileSpreadsheet;
  }

  switch (binaryGroup(name)) {
    case "archive":
      return FileArchive;
    case "spreadsheet":
      return FileSpreadsheet;
    case "presentation":
      return FileChartColumn;
    case "document":
      return FileText;
    case "executable":
      return FileBox;
    case "database":
      return Database;
    case "font":
      return FileType;
    case "image":
      return FileImage;
    case "data":
      return File;
  }

  const lang = detectLanguage(name);
  if (lang === "json") return FileJson;
  if (lang === "shell" || lang === "powershell") return FileTerminal;
  if (lang === "properties" || lower.startsWith(".env")) return FileSliders;
  if (CONFIG.has(lang)) return FileCog;
  if (CODE.has(lang)) return FileCode;
  if (lang === "markdown" || lang === "text") return FileText;
  return File;
}
