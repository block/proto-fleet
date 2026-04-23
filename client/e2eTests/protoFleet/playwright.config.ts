import { defineConfig } from "@playwright/test";
import path from "path";
import { fileURLToPath } from "url";
import { testConfig } from "./config/test.config";

const configDir = path.dirname(fileURLToPath(import.meta.url));
const adminStorageState = path.join(configDir, "playwright", ".auth", "admin.json");
const SETUP_FILE_GLOB = "**/[0-9][0-9]-*.spec.ts";

/**
 * See https://playwright.dev/docs/test-configuration.
 */

export default defineConfig({
  testDir: "./spec",
  globalSetup: "./globalSetup.ts",
  /* Run tests in serial order (one at a time) */
  fullyParallel: false,
  /* Fail the build on CI if you accidentally left test.only in the source code. */
  forbidOnly: !!process.env.CI,
  /* Retry on CI only */
  retries: 0,
  /* Opt out of parallel tests on CI for more stability */
  workers: 1,
  /* Reporter to use. See https://playwright.dev/docs/test-reporters */
  reporter: process.env.CI
    ? [
        ["html", { outputFolder: "playwright-report", open: "never" }],
        ["github"],
        ["junit", { outputFile: "test-results/results.xml" }],
      ]
    : "html",
  /* Global timeout for each test */
  timeout: testConfig.testTimeout,
  /* Set default timeout for all expect() assertions */
  expect: {
    timeout: testConfig.actionTimeout,
  },
  /* Shared settings for all the projects below. See https://playwright.dev/docs/api/class-testoptions. */
  use: {
    /* Base URL to use in actions like `await page.goto('/')`. */
    baseURL: testConfig.baseUrl,

    /* Set a consistent viewport size for all tests */
    viewport: { width: 1600, height: 900 },

    /* Set default timeout for actions like click, fill, etc. */
    actionTimeout: testConfig.actionTimeout,

    /* Capture screenshots (only on failure) and video (retain on failure) so they appear in the HTML report */
    screenshot: "only-on-failure",
    video: "retain-on-failure",

    /* Collect trace when retrying the failed test. See https://playwright.dev/docs/trace-viewer */
    trace: "on-first-retry",
  },

  // E.g.:  npx playwright test --project=desktop
  projects: [
    {
      name: "setup-desktop",
      testMatch: SETUP_FILE_GLOB,
      use: {
        viewport: { width: 1600, height: 900 },
        isMobile: false,
      },
    },
    {
      name: "setup-mobile",
      testMatch: SETUP_FILE_GLOB,
      use: {
        viewport: { width: 393, height: 852 },
        isMobile: true,
      },
    },
    {
      name: "desktop",
      testMatch: /.*\.spec\.ts$/,
      testIgnore: SETUP_FILE_GLOB,
      dependencies: ["setup-desktop"],
      use: {
        viewport: { width: 1600, height: 900 },
        isMobile: false,
        storageState: adminStorageState,
      },
    },
    // Resolution of the iPhone 14 Pro / 15 Pro / 16
    {
      name: "mobile",
      testMatch: /.*\.spec\.ts$/,
      testIgnore: SETUP_FILE_GLOB,
      dependencies: ["setup-mobile"],
      use: {
        viewport: { width: 393, height: 852 },
        isMobile: true,
        storageState: adminStorageState,
      },
    },
  ],
});
