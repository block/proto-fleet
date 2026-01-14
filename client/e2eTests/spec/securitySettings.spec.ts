/* eslint-disable playwright/expect-expect */
import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";
import { generateRandomText, generateRandomUsername } from "../helpers/testDataHelper";

test.describe("Proto Fleet - Security Settings", () => {
  test.beforeEach(async ({ page }) => {
    await page.goto("/");
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

    await test.step("Navigate to Security Settings", async () => {
      await settingsPage.navigateToSecuritySettings();
    });

    await test.step("Revert admin password", async () => {
      await settingsSecurityPage.clickUpdatePassword();
      await settingsSecurityPage.inputCurrentPassword(newPassword);
      await settingsSecurityPage.clickConfirm();
      await settingsSecurityPage.inputNewPassword(password);
      await settingsSecurityPage.inputConfirmPassword(password);
      await settingsSecurityPage.clickConfirmPassword();
      await settingsSecurityPage.validatePasswordChangeToast();
    });

    await test.step("Revert admin username", async () => {
      await settingsSecurityPage.clickUpdateUsername();
      await settingsSecurityPage.inputCurrentPassword(password);
      await settingsSecurityPage.clickConfirm();
      await settingsSecurityPage.inputNewUsername(username);
      await settingsSecurityPage.clickConfirmUsername();
      await settingsSecurityPage.validateUsernameChangeToast();
      await settingsSecurityPage.validateUsername(username);
    });

    await test.step("Log out", async () => {
      await authPage.logout();
      await authPage.validateRedirectedToAuth();
    });

    await test.step("Log in with reverted credentials", async () => {
      await authPage.inputUsername(username);
      await authPage.inputPassword(password);
      await authPage.clickLogin();
      await authPage.validateLoggedIn();
    });
  });
});
