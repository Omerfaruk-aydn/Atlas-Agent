import { defineConfig, devices } from "@playwright/test"
import { chromiumPath } from "./e2e/stack"

// End-to-end tests run the real binary (see e2e/stack.ts); there is no
// mocked API anywhere in this suite.
const executablePath = chromiumPath()

export default defineConfig({
  testDir: "./e2e",
  outputDir: "./test-results/artifacts",
  globalSetup: "./e2e/global-setup.ts",
  globalTeardown: "./e2e/global-teardown.ts",
  fullyParallel: false,
  workers: 1,
  timeout: 90_000,
  expect: { timeout: 20_000 },
  reporter: [["list"]],
  use: {
    ...devices["Desktop Chrome"],
    launchOptions: executablePath ? { executablePath } : {},
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
})
