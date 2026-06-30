import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";

test.describe("Proto Fleet - Activity Login", () => {
  test.use({ storageState: { cookies: [], origins: [] } });

  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test("Failed login activity is visible after correcting invalid credentials and signing in", async ({
    authPage,
    activityPage,
  }) => {
    await test.step("Log in as admin", async () => {
      await authPage.inputUsername(testConfig.users.admin.username);
      await authPage.inputPassword(testConfig.users.admin.password);
      await authPage.clickLogin();
      await authPage.validateLoggedIn();
    });

    await test.step("Confirm the successful login activity is present before testing a failed login", async () => {
      await activityPage.navigateToActivityPage();
      await activityPage.waitForActivityListToLoad();
      await activityPage.selectTypeFilter("Login");
      await activityPage.selectUserFilter(testConfig.users.admin.username);
      await activityPage.validateLatestActivityDescription("Login");
      await activityPage.validateLatestActivityUser(testConfig.users.admin.username);
      await activityPage.validateLatestActivityNotMarkedFailed();
    });

    await test.step("Log out", async () => {
      await authPage.logout();
      await authPage.validateRedirectedToAuth();
    });

    await test.step("Attempt login with an invalid password and validate the error", async () => {
      await authPage.inputUsername(testConfig.users.admin.username);
      await authPage.inputPassword(`${testConfig.users.admin.password}-invalid`);
      await authPage.clickLogin();
      await authPage.validateInvalidCredentials();
    });

    await test.step("Rewrite the correct password and validate the error clears", async () => {
      await authPage.inputPassword(testConfig.users.admin.password);
      await authPage.validateInvalidCredentialsNotVisible();
    });

    await test.step("Log in successfully with corrected credentials", async () => {
      await authPage.clickLogin();
      await authPage.validateLoggedIn();
    });

    await test.step("Validate the failed login attempt appears in Activity", async () => {
      await activityPage.navigateToActivityPage();
      await activityPage.waitForActivityListToLoad();
      await activityPage.searchActivity("Login failed");
      await activityPage.selectUserFilter(testConfig.users.admin.username);
      await activityPage.validateLatestActivityDescription("Login failed");
      await activityPage.validateLatestActivityUser(testConfig.users.admin.username);
      await activityPage.validateLatestActivityMarkedFailed();
    });
  });
});
