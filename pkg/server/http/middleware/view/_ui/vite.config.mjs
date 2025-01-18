import * as path from "path";
import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import { createHtmlPlugin } from "vite-plugin-html";

let redirectConfig = {
  target: "http://localhost:8082/",
  changeOrigin: true,
  secure: true,
  ws: true,
  followRedirects: true,
}

// https://vitejs.dev/config/
export default defineConfig({
  base: "./", // set /view/ as base for dev server
  plugins: [
    svelte(),
    createHtmlPlugin({
      minify: process.env.NODE_ENV == "production",
    }),
  ],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "src"),
    },
  },
  build: {
    sourcemap: process.env.NODE_ENV == "production" ? false : true,
  },
  server: {
    proxy: {
      "/view/ui-info": redirectConfig,
      "/view/page": redirectConfig,
      "/view/grpc": redirectConfig,
    },
    port: process.env.PORT ?? 3000,
  },
});
