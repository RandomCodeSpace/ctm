import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "node:path";

const target = "http://127.0.0.1:37778";

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      "/api": {
        target,
        changeOrigin: true,
      },
      "/healthz": {
        target,
        changeOrigin: true,
      },
      "/health": {
        target,
        changeOrigin: true,
      },
      "/events": {
        target,
        changeOrigin: true,
        // SSE is HTTP/1.1 keep-alive, not websockets — but the proxy
        // must not buffer. Vite's http-proxy passes streaming responses
        // through by default; we just need to make sure it doesn't try
        // to gzip/buffer.
        ws: false,
      },
    },
  },
});
