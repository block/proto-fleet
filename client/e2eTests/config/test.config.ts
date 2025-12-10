export const testConfig = {
  baseUrl: "http://localhost:5173",

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
  expectedMinerCount: 12, // Default amount of virtual miners
};

export const DEFAULT_TIMEOUT = testConfig.actionTimeout;
export const DEFAULT_INTERVAL = testConfig.interval;
