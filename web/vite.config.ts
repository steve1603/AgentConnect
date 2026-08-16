import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// The build output is embedded into the Go binary and served from the root of
// the same origin, so no third-party hosts are ever referenced.
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: "dist",
    emptyOutDir: true,
    sourcemap: false,
  },
  server: {
    port: 5173,
    // `npm run dev` proxies the API to the Go server on its default port.
    proxy: {
      "/api": "http://127.0.0.1:8000",
      "/healthz": "http://127.0.0.1:8000",
    },
  },
});
