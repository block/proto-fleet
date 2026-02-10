import type { TestConfig } from "./test.config.defaults";

// Declare optional local config module
// This file may not exist (it's gitignored for local development)
declare module "./test.config.local" {
  export const localTestConfig: Partial<TestConfig> | undefined;
}
