/// <reference types="vitest" />
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import path from "node:path";

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
    // See vite.config.ts — dedupe react across linked packages.
    dedupe: ["react", "react-dom", "react/jsx-runtime", "react/jsx-dev-runtime"],
  },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/test-setup.ts"],
    // e2e/ holds the Playwright suite — it has its own runner and must
    // not be collected by vitest (different @playwright/test globals).
    exclude: ["**/node_modules/**", "**/dist/**", "e2e/**"],
  },
} as never);
