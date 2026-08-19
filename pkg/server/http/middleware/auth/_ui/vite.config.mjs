import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  base: "./",
  plugins: [tailwindcss(), svelte()],
  build: {
    sourcemap: false,
  },
  server: {
    proxy: {
      "/auth/v1": {
        target: "http://localhost:8080",
        changeOrigin: true,
        secure: false,
      },
    },
    port: process.env.PORT ?? 3000,
  },
});
