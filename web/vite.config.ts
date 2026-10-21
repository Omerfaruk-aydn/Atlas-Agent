import { readFileSync, writeFileSync } from "node:fs"
import { resolve } from "node:path"
import { defineConfig } from "vitest/config"
import solid from "vite-plugin-solid"
import tailwindcss from "@tailwindcss/vite"

// The production build goes straight into the Go package that embeds it.
// The directory's tracked placeholder is restored after Vite empties it,
// so a clean checkout still compiles without a frontend build.
const outDir = resolve(__dirname, "../internal/webui/build")

// In development the gateway runs on its own port (`atlas-agent web
// --port 7788 --dev-origin http://localhost:5173`) and Vite proxies the API
// to it, so the browser still talks to a single origin.
const gateway = process.env.ATLAS_WEB_GATEWAY ?? "http://127.0.0.1:7788"

// Atlas Agent's UI package is vendored unchanged under vendor/atlas-ui and
// imported by its published entry points (`@atlas/ui/button`, ...), so
// copied Atlas Agent markup keeps its imports.
const esc = (s: string) => s.replace(/[.*+?^${}()|[\]\\/]/g, "\\$&")
function packageAliases(pkg: string, dir: string) {
  const root = resolve(__dirname, dir)
  const exports: Record<string, string> = JSON.parse(readFileSync(resolve(root, "package.json"), "utf8")).exports
  return Object.entries(exports)
    .filter(([key]) => key !== "./package.json")
    .sort(([a], [b]) => b.length - a.length)
    .map(([key, target]) => {
      const name = pkg + key.slice(1)
      if (!key.includes("*")) return { find: new RegExp(`^${esc(name)}$`), replacement: resolve(root, target) }
      const [head, tail] = name.split("*")
      const [thead, ttail] = resolve(root, target).split("*")
      return { find: new RegExp(`^${esc(head)}(.+)${esc(tail)}$`), replacement: `${thead}$1${ttail}` }
    })
}
const uiAliases = [...packageAliases("@atlas/ui", "vendor/atlas-ui"), ...packageAliases("@atlas/session-ui", "vendor/atlas-session-ui")]

export default defineConfig({
  plugins: [
    tailwindcss(),
    solid(),
    {
      name: "atlas-keep-placeholder",
      closeBundle() {
        writeFileSync(resolve(outDir, ".keep"), "Placeholder. `task web:build` writes the production web client here.\n")
      },
    },
  ],
  // @pierre/diffs ships a code-split worker, which needs ES worker output.
  worker: { format: "es" },
  build: {
    outDir,
    emptyOutDir: true,
    target: "es2022",
    sourcemap: false,
    chunkSizeWarningLimit: 1200,
  },
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      "/api": { target: gateway, changeOrigin: true, ws: false },
      "/auth": { target: gateway, changeOrigin: true },
    },
  },
  test: {
    environment: "jsdom",
    include: ["src/**/*.test.ts", "src/**/*.test.tsx"],
    server: { deps: { inline: [/solid-js/, /@solidjs/] } },
  },
  resolve: {
    conditions: ["development", "browser"],
    alias: [
      { find: /^@\/(.*)$/, replacement: resolve(__dirname, "src/oc/app/$1") },
      { find: /^@atlas\/util\/(.*)$/, replacement: resolve(__dirname, "vendor/atlas-util/src/$1.ts") },
      { find: /^@atlas\/schema\/(.*)$/, replacement: resolve(__dirname, "src/oc/shims/schema/$1.ts") },
      { find: /^@atlas\/gui-extensions\/sdk$/, replacement: resolve(__dirname, "src/oc/shims/gui-extensions/sdk.ts") },
      { find: /^@atlas\/client\/promise$/, replacement: resolve(__dirname, "src/oc/shims/client.ts") },
      ...uiAliases,
    ],
  },
})
