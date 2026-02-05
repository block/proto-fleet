import { defaultTestConfig, type TestConfig } from "./test.config.defaults";

let localConfig: Partial<TestConfig> = {};
try {
  // Try to import local config if it exists
  // @ts-expect-error - Local config file may not exist
  const module = await import("./test.config.local");
  localConfig = module.localTestConfig || {};
} catch {
  // Local config doesn't exist, use defaults only
}

// Merge default config with local overrides
export const testConfig: TestConfig = {
  ...defaultTestConfig,
  ...localConfig,
  users: {
    ...defaultTestConfig.users,
    ...localConfig.users,
  },
};

export const DEFAULT_TIMEOUT = testConfig.actionTimeout;
export const DEFAULT_INTERVAL = testConfig.interval;
