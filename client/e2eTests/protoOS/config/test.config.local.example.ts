import type { TestConfig } from "./test.config.defaults";

/**
 * Local test configuration overrides.
 *
 * HOW TO USE:
 * 1. Copy this file as test.config.local.ts
 * 2. Customize values for your local environment
 * 3. The .local.ts file is gitignored and won't be committed
 *
 * You can override any property from the default config.
 * Your IDE will provide autocomplete for all available options.
 */
export const localTestConfig: Partial<TestConfig> = {
  // Uncomment and modify values as needed:
  // testTimeout: 60000,
  // actionTimeout: 15000,
  // interval: 500,
  // admin: {
  //   password: "your-local-admin-password",
  // },
  // pool: {
  //   name: "Your Pool Name",
  //   url: "stratum+tcp://your-pool.com:3333",
  //   username: "your-username",
  //   password: "your-password",
  // },
};
