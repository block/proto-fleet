import type { TestConfig } from "./test.config.defaults";

/**
 * Example local test configuration.
 * HOW TO USE:
 * Copy this file as test.config.local.ts and customize for your local environment.
 * The local config file is gitignored and will not be committed.
 *
 * You can override any property from the default config.
 * Your IDE will provide autocomplete for all available options.
 */
export const localTestConfig: Partial<TestConfig> = {
  // Example: Switch environment target (real miners skip auth flows)
  // target: "real",

  // Example: Force the local fake-miners environment explicitly
  // target: "fake",

  // Example: Override admin credentials
  users: {
    admin: {
      username: "your-username",
      password: "your-password",
    },
  },
};
