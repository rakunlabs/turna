import type { Extension } from "@codemirror/state";
import { StreamLanguage, type StreamParser } from "@codemirror/language";
import { baseName, extOf } from "./format";

type Lang = {
  label: string;
  ext?: string[];
  /** Exact lower-case file names, e.g. "dockerfile". */
  names?: string[];
  /** Matches lower-case file names, e.g. /^\.env/. */
  pattern?: RegExp;
  load: () => Promise<Extension>;
};

const legacy = async (p: Promise<StreamParser<unknown>>) => StreamLanguage.define(await p);

/** Ordered as shown in the syntax picker. */
export const languages: Record<string, Lang> = {
  text: { label: "Plain text", ext: ["txt", "text", "log", "out"], load: async () => [] },

  json: {
    label: "JSON",
    ext: ["json", "jsonc", "json5", "map", "har", "webmanifest", "geojson", "ipynb"],
    names: [".babelrc", ".eslintrc", ".prettierrc", "composer.lock", ".jshintrc"],
    load: async () => (await import("@codemirror/lang-json")).json(),
  },
  yaml: {
    label: "YAML",
    ext: ["yaml", "yml"],
    names: [".clang-format", ".gitlab-ci.yml"],
    load: async () => (await import("@codemirror/lang-yaml")).yaml(),
  },
  toml: {
    label: "TOML",
    ext: ["toml"],
    names: ["cargo.lock", "poetry.lock", "pipfile"],
    load: () => legacy(import("@codemirror/legacy-modes/mode/toml").then((m) => m.toml)),
  },
  properties: {
    label: "INI / Properties",
    ext: ["ini", "cfg", "conf", "properties", "env", "editorconfig", "gitconfig", "npmrc", "desktop", "service", "timer", "socket"],
    names: [".gitattributes", ".gitmodules"],
    pattern: /^\.env(\..+)?$/,
    load: () => legacy(import("@codemirror/legacy-modes/mode/properties").then((m) => m.properties)),
  },
  xml: {
    label: "XML",
    ext: ["xml", "svg", "plist", "xsd", "xsl", "xslt", "rss", "atom", "csproj", "fsproj", "vbproj", "props", "targets", "resx", "wsdl", "kml", "gpx", "pom"],
    load: async () => (await import("@codemirror/lang-xml")).xml(),
  },
  markdown: {
    label: "Markdown",
    ext: ["md", "markdown", "mdx", "mkd", "mdown"],
    load: async () => (await import("@codemirror/lang-markdown")).markdown(),
  },

  javascript: {
    label: "JavaScript",
    ext: ["js", "mjs", "cjs", "jsx"],
    load: async () => (await import("@codemirror/lang-javascript")).javascript({ jsx: true }),
  },
  typescript: {
    label: "TypeScript",
    ext: ["ts", "mts", "cts", "tsx"],
    load: async () => (await import("@codemirror/lang-javascript")).javascript({ typescript: true, jsx: true }),
  },
  html: {
    label: "HTML",
    ext: ["html", "htm", "xhtml", "shtml", "tmpl", "gohtml", "hbs", "handlebars", "ejs", "njk"],
    load: async () => (await import("@codemirror/lang-html")).html(),
  },
  vue: { label: "Vue", ext: ["vue"], load: async () => (await import("@codemirror/lang-vue")).vue() },
  svelte: { label: "Svelte", ext: ["svelte"], load: async () => (await import("@codemirror/lang-html")).html() },
  css: { label: "CSS", ext: ["css", "pcss", "postcss"], load: async () => (await import("@codemirror/lang-css")).css() },
  scss: {
    label: "SCSS / Sass",
    ext: ["scss", "sass"],
    load: async () => (await import("@codemirror/lang-sass")).sass({ indented: false }),
  },
  less: { label: "Less", ext: ["less"], load: async () => (await import("@codemirror/lang-less")).less() },

  go: {
    label: "Go",
    ext: ["go"],
    names: ["go.mod", "go.sum", "go.work"],
    load: async () => (await import("@codemirror/lang-go")).go(),
  },
  python: {
    label: "Python",
    ext: ["py", "pyw", "pyi", "pyx", "gyp"],
    names: ["sconstruct", "sconscript", "buck", "build.bazel", "workspace"],
    load: async () => (await import("@codemirror/lang-python")).python(),
  },
  rust: { label: "Rust", ext: ["rs"], load: async () => (await import("@codemirror/lang-rust")).rust() },
  java: { label: "Java", ext: ["java", "jav"], load: async () => (await import("@codemirror/lang-java")).java() },
  kotlin: {
    label: "Kotlin",
    ext: ["kt", "kts"],
    load: () => legacy(import("@codemirror/legacy-modes/mode/clike").then((m) => m.kotlin)),
  },
  scala: {
    label: "Scala",
    ext: ["scala", "sc", "sbt"],
    load: () => legacy(import("@codemirror/legacy-modes/mode/clike").then((m) => m.scala)),
  },
  groovy: {
    label: "Groovy / Gradle",
    ext: ["groovy", "gradle", "gvy"],
    names: ["jenkinsfile"],
    load: () => legacy(import("@codemirror/legacy-modes/mode/groovy").then((m) => m.groovy)),
  },
  c: { label: "C", ext: ["c", "h"], load: async () => (await import("@codemirror/lang-cpp")).cpp() },
  cpp: {
    label: "C++",
    ext: ["cpp", "cc", "cxx", "c++", "hpp", "hh", "hxx", "h++", "ino", "ipp", "tpp"],
    load: async () => (await import("@codemirror/lang-cpp")).cpp(),
  },
  csharp: {
    label: "C#",
    ext: ["cs", "csx"],
    load: () => legacy(import("@codemirror/legacy-modes/mode/clike").then((m) => m.csharp)),
  },
  objectivec: {
    label: "Objective-C",
    ext: ["m", "mm"],
    load: () => legacy(import("@codemirror/legacy-modes/mode/clike").then((m) => m.objectiveC)),
  },
  swift: {
    label: "Swift",
    ext: ["swift"],
    load: () => legacy(import("@codemirror/legacy-modes/mode/swift").then((m) => m.swift)),
  },
  dart: {
    label: "Dart",
    ext: ["dart"],
    load: () => legacy(import("@codemirror/legacy-modes/mode/clike").then((m) => m.dart)),
  },
  php: { label: "PHP", ext: ["php", "phtml", "php3", "php4", "php5", "phps"], load: async () => (await import("@codemirror/lang-php")).php() },
  ruby: {
    label: "Ruby",
    ext: ["rb", "rake", "gemspec", "ru", "erb"],
    names: ["gemfile", "rakefile", "podfile", "vagrantfile", "brewfile", "fastfile"],
    load: () => legacy(import("@codemirror/legacy-modes/mode/ruby").then((m) => m.ruby)),
  },
  perl: { label: "Perl", ext: ["pl", "pm", "t", "pod"], load: () => legacy(import("@codemirror/legacy-modes/mode/perl").then((m) => m.perl)) },
  lua: { label: "Lua", ext: ["lua", "rockspec"], load: () => legacy(import("@codemirror/legacy-modes/mode/lua").then((m) => m.lua)) },
  r: { label: "R", ext: ["r", "rmd"], names: [".rprofile"], load: () => legacy(import("@codemirror/legacy-modes/mode/r").then((m) => m.r)) },
  julia: { label: "Julia", ext: ["jl"], load: () => legacy(import("@codemirror/legacy-modes/mode/julia").then((m) => m.julia)) },
  haskell: { label: "Haskell", ext: ["hs", "lhs"], load: () => legacy(import("@codemirror/legacy-modes/mode/haskell").then((m) => m.haskell)) },
  elixir: {
    label: "Elixir / Erlang",
    ext: ["ex", "exs", "erl", "hrl"],
    names: ["rebar.config"],
    load: () => legacy(import("@codemirror/legacy-modes/mode/erlang").then((m) => m.erlang)),
  },
  clojure: {
    label: "Clojure",
    ext: ["clj", "cljs", "cljc", "edn"],
    load: () => legacy(import("@codemirror/legacy-modes/mode/clojure").then((m) => m.clojure)),
  },
  ocaml: {
    label: "OCaml / F#",
    ext: ["ml", "mli", "fs", "fsi", "fsx"],
    load: () => legacy(import("@codemirror/legacy-modes/mode/mllike").then((m) => m.oCaml)),
  },
  elm: { label: "Elm", ext: ["elm"], load: () => legacy(import("@codemirror/legacy-modes/mode/elm").then((m) => m.elm)) },
  crystal: { label: "Crystal", ext: ["cr"], load: () => legacy(import("@codemirror/legacy-modes/mode/crystal").then((m) => m.crystal)) },
  d: { label: "D", ext: ["d", "di"], load: () => legacy(import("@codemirror/legacy-modes/mode/d").then((m) => m.d)) },
  pascal: { label: "Pascal", ext: ["pas", "pp", "dpr"], load: () => legacy(import("@codemirror/legacy-modes/mode/pascal").then((m) => m.pascal)) },
  fortran: { label: "Fortran", ext: ["f", "f90", "f95", "f03", "for"], load: () => legacy(import("@codemirror/legacy-modes/mode/fortran").then((m) => m.fortran)) },
  vb: { label: "Visual Basic", ext: ["vb", "bas", "vbs"], load: () => legacy(import("@codemirror/legacy-modes/mode/vb").then((m) => m.vb)) },
  asm: { label: "Assembly", ext: ["s", "asm", "nasm"], load: () => legacy(import("@codemirror/legacy-modes/mode/gas").then((m) => m.gas)) },
  verilog: { label: "Verilog", ext: ["v", "sv", "svh", "vh"], load: () => legacy(import("@codemirror/legacy-modes/mode/verilog").then((m) => m.verilog)) },
  vhdl: { label: "VHDL", ext: ["vhd", "vhdl"], load: () => legacy(import("@codemirror/legacy-modes/mode/vhdl").then((m) => m.vhdl)) },
  wasm: { label: "WebAssembly text", ext: ["wat", "wast"], load: () => legacy(import("@codemirror/legacy-modes/mode/wast").then((m) => m.wast)) },

  shell: {
    label: "Shell",
    ext: ["sh", "bash", "zsh", "ksh", "fish", "command", "bats"],
    names: [".bashrc", ".bash_profile", ".zshrc", ".profile", ".bash_aliases", "pkgbuild", "apkbuild"],
    load: () => legacy(import("@codemirror/legacy-modes/mode/shell").then((m) => m.shell)),
  },
  powershell: {
    label: "PowerShell",
    ext: ["ps1", "psm1", "psd1"],
    load: () => legacy(import("@codemirror/legacy-modes/mode/powershell").then((m) => m.powerShell)),
  },
  dockerfile: {
    label: "Dockerfile",
    ext: ["dockerfile", "containerfile"],
    names: ["dockerfile", "containerfile"],
    pattern: /^(docker|container)file[.-]/,
    load: () => legacy(import("@codemirror/legacy-modes/mode/dockerfile").then((m) => m.dockerFile)),
  },
  makefile: {
    label: "Makefile",
    ext: ["mk", "mak", "make"],
    names: ["makefile", "gnumakefile", "bsdmakefile"],
    load: () => legacy(import("@codemirror/legacy-modes/mode/shell").then((m) => m.shell)),
  },
  cmake: {
    label: "CMake",
    ext: ["cmake"],
    names: ["cmakelists.txt"],
    load: () => legacy(import("@codemirror/legacy-modes/mode/cmake").then((m) => m.cmake)),
  },
  nginx: {
    label: "Nginx",
    names: ["nginx.conf"],
    pattern: /^nginx.*\.conf$/,
    load: () => legacy(import("@codemirror/legacy-modes/mode/nginx").then((m) => m.nginx)),
  },
  hcl: {
    label: "Terraform / HCL",
    ext: ["tf", "tfvars", "hcl", "nomad"],
    load: () => legacy(import("@codemirror/legacy-modes/mode/ruby").then((m) => m.ruby)),
  },
  protobuf: { label: "Protocol Buffers", ext: ["proto"], load: () => legacy(import("@codemirror/legacy-modes/mode/protobuf").then((m) => m.protobuf)) },
  graphql: { label: "GraphQL", ext: ["graphql", "gql", "graphqls"], load: () => legacy(import("@codemirror/legacy-modes/mode/webidl").then((m) => m.webIDL)) },
  sql: {
    label: "SQL",
    ext: ["sql", "ddl", "dml", "pgsql", "psql", "mysql"],
    load: async () => (await import("@codemirror/lang-sql")).sql(),
  },
  cypher: { label: "Cypher", ext: ["cypher", "cql"], load: () => legacy(import("@codemirror/legacy-modes/mode/cypher").then((m) => m.cypher)) },
  sparql: { label: "SPARQL", ext: ["sparql", "rq"], load: () => legacy(import("@codemirror/legacy-modes/mode/sparql").then((m) => m.sparql)) },
  turtle: { label: "Turtle", ext: ["ttl"], load: () => legacy(import("@codemirror/legacy-modes/mode/turtle").then((m) => m.turtle)) },

  jinja: {
    label: "Jinja / Templates",
    ext: ["j2", "jinja", "jinja2", "twig", "liquid", "tpl"],
    load: () => legacy(import("@codemirror/legacy-modes/mode/jinja2").then((m) => m.jinja2)),
  },
  pug: { label: "Pug", ext: ["pug", "jade"], load: () => legacy(import("@codemirror/legacy-modes/mode/pug").then((m) => m.pug)) },
  stylus: { label: "Stylus", ext: ["styl", "stylus"], load: () => legacy(import("@codemirror/legacy-modes/mode/stylus").then((m) => m.stylus)) },
  latex: { label: "LaTeX", ext: ["tex", "sty", "cls", "bib", "ltx"], load: () => legacy(import("@codemirror/legacy-modes/mode/stex").then((m) => m.stex)) },
  diff: { label: "Diff / Patch", ext: ["diff", "patch", "rej"], load: () => legacy(import("@codemirror/legacy-modes/mode/diff").then((m) => m.diff)) },
  http: { label: "HTTP", ext: ["http", "rest"], load: () => legacy(import("@codemirror/legacy-modes/mode/http").then((m) => m.http)) },
  gherkin: { label: "Gherkin", ext: ["feature"], load: () => legacy(import("@codemirror/legacy-modes/mode/gherkin").then((m) => m.gherkin)) },
  csv: { label: "CSV / TSV", ext: ["csv", "tsv", "psv"], load: async () => [] },
  ignore: {
    label: "Ignore file",
    ext: ["gitignore", "dockerignore", "npmignore", "eslintignore", "prettierignore", "helmignore"],
    names: [".gitignore", ".dockerignore", ".npmignore", ".eslintignore", ".prettierignore", ".helmignore", "codeowners"],
    load: () => legacy(import("@codemirror/legacy-modes/mode/shell").then((m) => m.shell)),
  },
};

export const languageLabels: Record<string, string> = Object.fromEntries(
  Object.entries(languages).map(([k, v]) => [k, v.label]),
);

const byExt = new Map<string, string>();
const byName = new Map<string, string>();
for (const [id, l] of Object.entries(languages)) {
  for (const e of l.ext ?? []) if (!byExt.has(e)) byExt.set(e, id);
  for (const n of l.names ?? []) byName.set(n, id);
}

export function detectLanguage(path: string): string {
  const name = baseName(path).toLowerCase();
  const exact = byName.get(name);
  if (exact) return exact;
  for (const [id, l] of Object.entries(languages)) if (l.pattern?.test(name)) return id;
  // dotfiles like ".gitignore" have no extension but match a known kind
  const ext = extOf(path) || (name.startsWith(".") ? name.slice(1) : "");
  return byExt.get(ext) ?? "text";
}

export async function loadLanguage(lang: string): Promise<Extension> {
  return (languages[lang] ?? languages.text).load();
}
