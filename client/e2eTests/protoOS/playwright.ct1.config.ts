import { defineConfig } from "@playwright/test";
import { fileURLToPath } from "node:url";

const simulatorDirectory = fileURLToPath(new URL("../../../server/fake-ct1/", import.meta.url));
const webRoot = fileURLToPath(new URL("../../dist/protoOS/", import.meta.url));
const baseURL = "http://127.0.0.1:18081";
const reportDirectory = fileURLToPath(new URL("./playwright-report/ct1/", import.meta.url));

// This suite owns its simulator. Never reuse a running miner or a Vite proxy.
export default defineConfig({
  testDir: "./standalone",
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: 0,
  workers: 1,
  timeout: 60_000,
  expect: { timeout: 15_000 },
  reporter: [["list"], ["html", { outputFolder: reportDirectory, open: "never" }]],
  outputDir: "test-results/ct1",
  use: {
    baseURL,
    storageState: { cookies: [], origins: [] },
    browserName: "chromium",
    screenshot: "only-on-failure",
    trace: "retain-on-failure",
  },
  projects: [
    { name: "desktop", use: { viewport: { width: 1600, height: 900 } } },
    { name: "mobile", use: { viewport: { width: 393, height: 852 }, isMobile: true } },
  ],
  webServer: {
    command: `go run . --web-root "${webRoot}"`,
    cwd: simulatorDirectory,
    url: baseURL,
    reuseExistingServer: false,
    timeout: 120_000,
    env: {
      GOWORK: "off",
      HTTP_PORT: "18081",
      CT1_TANKS: "2",
      CT1_MODULES_PER_TANK: "2",
      CT1_SLOTS_PER_MODULE: "3",
      CT1_DEMO_MODULE_STATES: "false",
    },
  },
});
