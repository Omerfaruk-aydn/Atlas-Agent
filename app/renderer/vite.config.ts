import react from "@vitejs/plugin-react";
import { resolve } from "node:path";
import { defineConfig } from "vite";

export default defineConfig({
  root: __dirname,
  base: "/",
  plugins: [react()],
  build: {
    outDir: "dist",
    emptyOutDir: true,
    target: "chrome140",
    // The gateway's CSP allows no inline scripts, and hashed files under
    // assets/ are cached for good.
    assetsDir: "assets",
    modulePreload: { polyfill: false },
    // The design editor on its own page, which the agent's studio server
    // (internal/designstudio) serves for a browser it drives.
    rollupOptions: { input: { main: resolve(__dirname, "index.html"), design: resolve(__dirname, "design.html") } },
  },
});
