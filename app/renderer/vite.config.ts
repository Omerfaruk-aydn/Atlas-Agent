import react from "@vitejs/plugin-react";
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
  },
});
