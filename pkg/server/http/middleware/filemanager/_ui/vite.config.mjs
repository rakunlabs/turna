import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import tailwindcss from "@tailwindcss/vite";

const target = process.env.TURNA_URL ?? "http://localhost:8080";

export default defineConfig({
  base: "./",
  plugins: [tailwindcss(), svelte()],
  build: {
    sourcemap: false,
    chunkSizeWarningLimit: 1000,
  },
  server: {
    proxy: {
      "/api": { target: `${target}/files`, changeOrigin: true },
    },
    port: Number(process.env.PORT ?? 3000),
  },
});
