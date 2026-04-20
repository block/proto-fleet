import { defaultTestConfig, type TestConfig } from "./test.config.defaults";

let localConfig: Partial<TestConfig> = {};
try {
  // Try to import local config if it exists (file is gitignored)
  // To create: copy test.config.local.example.ts to test.config.local.ts
  const module = await import("./test.config.local");
  localConfig = module.localTestConfig || {};
} catch {
  // Local config doesn't exist, use defaults only
}

// Merge default config with local overrides
export const testConfig: TestConfig = {
  ...defaultTestConfig,
  ...localConfig,
  admin: {
    ...defaultTestConfig.admin,
    ...localConfig.admin,
  },
};

export const DEFAULT_TIMEOUT = testConfig.actionTimeout;
export const DEFAULT_INTERVAL = testConfig.interval;
