export const defaultTestConfig = {
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

  testTimeout: 120000,
  actionTimeout: 15000,
  interval: 500,
};

export type TestConfig = typeof defaultTestConfig;
