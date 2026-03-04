import { defaultTestConfig, type TestConfig } from "./test.config.defaults";

type E2ETarget = TestConfig["target"];

const fakeBaseUrl = "http://localhost:5173";
const realBaseUrl = "http://localhost:8080";

function parseOptionalTarget(value: string | undefined): E2ETarget | undefined {
  if (!value) {
    return undefined;
  }

  const normalized = value.trim().toLowerCase();
  if (normalized === "fake") {
    return "fake";
  }
  if (normalized === "real") {
    return "real";
  }

  return undefined;
}

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
const mergedConfig: TestConfig = {
  ...defaultTestConfig,
  ...localConfig,
  users: {
    ...defaultTestConfig.users,
    ...localConfig.users,
  },
  miners: {
    ...defaultTestConfig.miners,
    ...localConfig.miners,
  },
};

const rawTarget = process.env.E2E_TARGET;
const targetFromEnv = parseOptionalTarget(rawTarget);
if (rawTarget && !targetFromEnv) {
  throw new Error(`Invalid E2E_TARGET value "${rawTarget}". Expected "fake" or "real".`);
}
const resolvedTarget: E2ETarget = targetFromEnv ?? mergedConfig.target;

// Intentionally derived from target only (no base URL overrides).
const resolvedBaseUrl = resolvedTarget === "real" ? realBaseUrl : fakeBaseUrl;

export const testConfig: TestConfig = {
  ...mergedConfig,
  baseUrl: resolvedBaseUrl,
  target: resolvedTarget,
};

export const DEFAULT_TIMEOUT = testConfig.actionTimeout;
export const DEFAULT_INTERVAL = testConfig.interval;
