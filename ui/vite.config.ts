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
    // Force one copy of react/react-dom across the bundle. Without this,
    // a `link:`-installed sibling package (e.g. @ossrandom/design-system
    // resolved against its own node_modules) brings a second react,
    // which React 19 rejects with "A React Element from an older
    // version of React was rendered". Dedupe makes Vite resolve these
    // modules to ctm/ui's node_modules regardless of where the import
    // originates.
    dedupe: ["react", "react-dom", "react/jsx-runtime", "react/jsx-dev-runtime"],
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
