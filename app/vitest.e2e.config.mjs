import { defineConfig } from "vitest/config";

// One Electron at a time, and enough time for it to start.
export default defineConfig({
  test: {
    include: ["test/e2e.test.mjs"],
    fileParallelism: false,
    testTimeout: 60_000,
    hookTimeout: 60_000,
  },
});
