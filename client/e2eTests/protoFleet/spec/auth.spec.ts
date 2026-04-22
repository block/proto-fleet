/* eslint-disable playwright/expect-expect */
import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";

test.describe("Proto Fleet - Authentication", () => {
  // Validates the login flow itself, so opt out of the preloaded admin storageState.
  test.use({ storageState: { cookies: [], origins: [] } });

  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test("Sign in with admin", async ({ authPage, settingsPage, settingsTeamPage }) => {
    await test.step("Log in as admin user", async () => {
      await authPage.inputUsername(testConfig.users.admin.username);
      await authPage.inputPassword(testConfig.users.admin.password);
      await authPage.clickLogin();
      await authPage.validateLoggedIn();
    });

    await test.step("Navigate to Team Settings and validate admin access", async () => {
      await settingsPage.navigateToTeamSettings();
      await settingsTeamPage.validateTeamSettingsPageOpened();
      await settingsTeamPage.validateIsAdmin();
    });
  });
});
