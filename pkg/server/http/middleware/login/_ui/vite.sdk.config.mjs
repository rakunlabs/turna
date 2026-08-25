import * as path from "path";
import { defineConfig } from "vite";

// Second build pass: bundle the login SDK as one self-contained ES module
// with a stable, unhashed name. The login middleware serves it from the
// embedded dist at `{base}/auth/sdk.js` for external login pages.
export default defineConfig({
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "src"),
    },
  },
  build: {
    target: "es2020",
    lib: {
      entry: path.resolve(__dirname, "src/sdk/index.ts"),
      formats: ["es"],
      fileName: () => "sdk.js",
    },
    outDir: "dist",
    emptyOutDir: false,
    sourcemap: false,
  },
});
