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
    coverage: {
      // v8 keeps the dep footprint small (built into Node). lcov is
      // what SonarCloud's javascript.lcov.reportPaths consumes; text
      // and html keep local CI debuggable.
      provider: "v8",
      reporter: ["text", "lcov", "html"],
      reportsDirectory: "./coverage",
      include: ["src/**/*.{ts,tsx}"],
      exclude: [
        "src/**/*.test.{ts,tsx}",
        "src/test-setup.ts",
        "src/main.tsx",
        "src/vite-env.d.ts",
      ],
    },
  },
} as never);
