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

  timeouts: 30000,
  expectedMinerCount: 12, // Default amount of virtual miners
};
