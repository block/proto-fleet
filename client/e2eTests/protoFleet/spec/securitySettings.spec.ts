/* eslint-disable playwright/expect-expect */
import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";
import { generateRandomText, generateRandomUsername } from "../helpers/testDataHelper";
import { AuthPage } from "../pages/auth";
import { SettingsPage } from "../pages/settings";
import { SettingsSecurityPage } from "../pages/settingsSecurity";

test.describe("Proto Fleet - Security Settings", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test.afterAll("CLEANUP: Ensure default admin credentials", async ({ browser }, testInfo) => {
    const isMobile = testInfo.project.use?.isMobile ?? false;
    const context = await browser.newContext({ baseURL: testConfig.baseUrl });
    try {
      const page = await context.newPage();
      const authPage = new AuthPage(page, isMobile);
      const settingsPage = new SettingsPage(page, isMobile);
      const settingsSecurityPage = new SettingsSecurityPage(page, isMobile);

      const tryLogin = async (candidateUsername: string, candidatePassword: string) => {
        await page.goto("/auth");
        await authPage.inputUsername(candidateUsername);
        await authPage.inputPassword(candidatePassword);
        await authPage.clickLogin();

        try {
          await authPage.validateLoggedIn(3000);
          return true;
        } catch {
          return false;
        }
      };

      let loggedIn = await tryLogin(username, password);

      if (!loggedIn) {
        // Default credentials failed
        loggedIn = await tryLogin(newUsername, password);

        if (loggedIn) {
          // Only username needs to be reverted
          await settingsPage.navigateToSecuritySettings();
          await settingsSecurityPage.clickUpdateUsername();
          await settingsSecurityPage.inputCurrentPassword(password);
          await settingsSecurityPage.clickConfirm();
          await settingsSecurityPage.inputNewUsername(username);
          await settingsSecurityPage.clickConfirmUsername();
          await settingsSecurityPage.validateUsernameChangeToast();
        } else {
          // Both username and password need to be reverted
          loggedIn = await tryLogin(newUsername, newPassword);
          if (!loggedIn) {
            throw new Error("Unable to log in with updated admin credentials during cleanup.");
          }

          await settingsPage.navigateToSecuritySettings();
          await settingsSecurityPage.clickUpdatePassword();
          await settingsSecurityPage.inputCurrentPassword(newPassword);
          await settingsSecurityPage.clickConfirm();
          await settingsSecurityPage.inputNewPassword(password);
          await settingsSecurityPage.inputConfirmPassword(password);
          await settingsSecurityPage.clickConfirmPassword();
          await settingsSecurityPage.validatePasswordChangeToast();

          await settingsSecurityPage.clickUpdateUsername();
          await settingsSecurityPage.inputCurrentPassword(password);
          await settingsSecurityPage.clickConfirm();
          await settingsSecurityPage.inputNewUsername(username);
          await settingsSecurityPage.clickConfirmUsername();
          await settingsSecurityPage.validateUsernameChangeToast();
        }
      }
    } finally {
      await context.close();
    }
  });

  const username = testConfig.users.admin.username;
  const password = testConfig.users.admin.password;

  const newUsername = generateRandomUsername();
  const newPassword = generateRandomText("A1!");

  test("Update admin username and password", async ({ authPage, settingsPage, settingsSecurityPage }) => {
    await test.step("Log in as admin", async () => {
      await authPage.inputUsername(username);
      await authPage.inputPassword(password);
      await authPage.clickLogin();
      await authPage.validateLoggedIn();
    });

    await test.step("Navigate to Security Settings", async () => {
      await settingsPage.navigateToSecuritySettings();
    });

    await test.step("Change admin username", async () => {
      await settingsSecurityPage.clickUpdateUsername();
      await settingsSecurityPage.inputCurrentPassword(password);
      await settingsSecurityPage.clickConfirm();
      await settingsSecurityPage.inputNewUsername(newUsername);
      await settingsSecurityPage.clickConfirmUsername();
      await settingsSecurityPage.validateUsernameChangeToast();
      await settingsSecurityPage.validateUsername(newUsername);
    });

    await test.step("Log out", async () => {
      await authPage.logout();
      await authPage.validateRedirectedToAuth();
    });

    await test.step("Log in with new username", async () => {
      await authPage.inputUsername(newUsername);
      await authPage.inputPassword(password);
      await authPage.clickLogin();
      await authPage.validateLoggedIn();
    });

    await test.step("Navigate to Security Settings", async () => {
      await settingsPage.navigateToSecuritySettings();
    });

    await test.step("Change admin password", async () => {
      await settingsSecurityPage.clickUpdatePassword();
      await settingsSecurityPage.inputCurrentPassword(password);
      await settingsSecurityPage.clickConfirm();
      await settingsSecurityPage.inputNewPassword(newPassword);
      await settingsSecurityPage.inputConfirmPassword(newPassword);
      await settingsSecurityPage.clickConfirmPassword();
      await settingsSecurityPage.validatePasswordChangeToast();
    });

    await test.step("Log out", async () => {
      await authPage.logout();
      await authPage.validateRedirectedToAuth();
    });

    await test.step("Log in with new password", async () => {
      await authPage.inputUsername(newUsername);
      await authPage.inputPassword(newPassword);
      await authPage.clickLogin();
      await authPage.validateLoggedIn();
    });
  });
});
