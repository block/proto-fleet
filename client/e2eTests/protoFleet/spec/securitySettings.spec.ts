import { test } from "../fixtures/pageFixtures";
import {
  cleanupSecurityMember,
  MEMBER_PASSWORD,
  provisionRoleAndLoginViaStoredAdminContext,
} from "../helpers/rbacTestSetup";
import { generateRandomText } from "../helpers/testDataHelper";

test.describe("Proto Fleet - Security Settings", () => {
  // Credential changes belong to a disposable login, never the shared admin.
  test.use({ storageState: { cookies: [], origins: [] } });

  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test(
    "Update username and password",
    { tag: "@smoke" },
    async ({ browser, authPage, commonSteps, settingsPage, settingsSecurityPage }, testInfo) => {
      const member = await provisionRoleAndLoginViaStoredAdminContext(browser, testInfo, commonSteps, {
        permissionKeys: ["fleet:read"],
        roleDescription: "Security credential test",
      });
      const password = MEMBER_PASSWORD;
      const newUsername = generateRandomText("security_user");
      const newPassword = generateRandomText("A1!");
      try {
        await test.step("Navigate to Security Settings", async () => {
          await settingsPage.navigateToSecuritySettings();
        });

        await test.step("Change username", async () => {
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
          await authPage.gotoAuthPage();
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

        await test.step("Change password", async () => {
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
          await authPage.gotoAuthPage();
        });

        await test.step("Log in with new password", async () => {
          await authPage.inputUsername(newUsername);
          await authPage.inputPassword(newPassword);
          await authPage.clickLogin();
          await authPage.validateLoggedIn();
        });
      } finally {
        await cleanupSecurityMember(browser, testInfo, [member.username, newUsername], member.roleName);
      }
    },
  );
});
