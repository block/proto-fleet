export const defaultTestConfig = {
  baseUrl: "http://localhost:5173",

  /**
   * Execution target for environment-specific behavior.
   * - fake: local/dev environment using fake miners (default)
   * - real: environment backed by real miners
   */
  target: "fake" as "fake" | "real",

  users: {
    admin: {
      username: "admin",
      password: "Pass123!",
    },
  },

  miners: {
    username: "root",
    password: "root",
  },

  testTimeout: 180000,
  actionTimeout: 30000,
  interval: 500,
};

export type TestConfig = typeof defaultTestConfig;
