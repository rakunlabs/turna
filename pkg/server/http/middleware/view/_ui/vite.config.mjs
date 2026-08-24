import { fileURLToPath } from "node:url";
import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import tailwindcss from "@tailwindcss/vite";

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
  plugins: [tailwindcss(), svelte()],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  build: {
    sourcemap: false,
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
