export const defaultTestConfig = {
  baseUrl: "http://localhost:3000",

  admin: {
    username: "admin",
    password: "Pass123!",
  },

  pool: {
    url: "stratum+tcp://mine.ocean.xyz:3334",
  },

  testTimeout: 180000,
  actionTimeout: 30000,
  interval: 500,
};

export type TestConfig = typeof defaultTestConfig;
