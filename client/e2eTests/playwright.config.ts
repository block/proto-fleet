import { defineConfig } from "@playwright/test";
import { testConfig } from "./config/test.config";

/**
 * See https://playwright.dev/docs/test-configuration.
 */

export default defineConfig({
  testDir: "./spec",
  /* Run tests in serial order (one at a time) */
  fullyParallel: false,
  /* Fail the build on CI if you accidentally left test.only in the source code. */
  forbidOnly: !!process.env.CI,
  /* Retry on CI only */
  retries: 0,
  /* Opt out of parallel tests on CI for more stability */
  workers: 1,
  /* Reporter to use. See https://playwright.dev/docs/test-reporters */
  reporter: process.env.CI ? [["html"], ["github"]] : "html",
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
    viewport: { width: 1920, height: 1080 },

    /* Set default timeout for actions like click, fill, etc. */
    actionTimeout: testConfig.actionTimeout,

    /* Capture screenshots (only on failure) and video (retain on failure) so they appear in the HTML report */
    screenshot: "only-on-failure",
    video: "retain-on-failure",

    /* Collect trace when retrying the failed test. See https://playwright.dev/docs/trace-viewer */
    trace: "on-first-retry",
  },

  projects: [
    {
      name: "chromium",
      use: {},
    },
  ],
});
