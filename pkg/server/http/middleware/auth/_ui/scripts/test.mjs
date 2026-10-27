import { build } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import { existsSync } from "node:fs";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";

// Compile actual client-side Svelte runes, rather than testing SSR substitutes.
// This uses the existing build toolchain and Node's test runner: no test dependencies.
const root = fileURLToPath(new URL("../", import.meta.url));
const temporary = process.env.TMPDIR || (existsSync("/tmp/opencode") ? "/tmp/opencode" : tmpdir());
const output = await mkdtemp(join(temporary, "turna-auth-ui-tests-"));
try {
  await build({
    root,
    configFile: false,
    plugins: [svelte()],
    logLevel: "warn",
    build: {
      outDir: output,
      emptyOutDir: false,
      minify: false,
      lib: { entry: join(root, "tests/state.test.ts"), formats: ["es"], fileName: () => "tests.mjs" },
      rollupOptions: { external: (id) => id.startsWith("node:") },
    },
  });
  const result = spawnSync(process.execPath, ["--test", join(output, "tests.mjs")], { stdio: "inherit" });
  if (result.error) throw result.error;
  process.exitCode = result.status ?? 1;
} finally {
  await rm(output, { recursive: true, force: true });
}
