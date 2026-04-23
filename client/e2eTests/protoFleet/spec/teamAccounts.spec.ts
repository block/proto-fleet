/* eslint-disable playwright/expect-expect */
import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";
import { CommonSteps } from "../helpers/commonSteps";
import { generateRandomUsername } from "../helpers/testDataHelper";
import { AuthPage } from "../pages/auth";
import { MinersPage } from "../pages/miners";
import { SettingsPage } from "../pages/settings";
import { SettingsTeamPage } from "../pages/settingsTeam";

test.describe("Proto Fleet - Team Accounts", () => {
  // Tests here exercise logout + re-login as different users; opt out of the
  // preloaded admin storageState so each test starts from a clean session.
  test.use({ storageState: { cookies: [], origins: [] } });

  test.beforeEach(async ({ page }) => {
    await page.goto("/");
  });

  test.afterAll("CLEANUP: Deactivate any team members created during tests", async ({ browser }, testInfo) => {
    const isMobile = testInfo.project.use?.isMobile ?? false;
    const context = await browser.newContext({ baseURL: testConfig.baseUrl });

    try {
      const page = await context.newPage();
      await page.goto("/");

      const authPage = new AuthPage(page, isMobile);
      const minersPage = new MinersPage(page, isMobile);
      const settingsPage = new SettingsPage(page, isMobile);
      const settingsTeamPage = new SettingsTeamPage(page, isMobile);
      const commonSteps = new CommonSteps(authPage, minersPage);

      await commonSteps.loginAsAdmin();

      await settingsPage.navigateToTeamSettings();
      await settingsTeamPage.validateTeamSettingsPageOpened();
      await settingsTeamPage.validateMemberVisible("admin");

      const teamMemberRows = await page.getByTestId("list-row").all();
      const usernamesToDeactivate: string[] = [];

      for (const row of teamMemberRows) {
        const usernameElement = row.locator(`//td[@data-testid='username']//span`);
        const username = await usernameElement.textContent();

        const trimmedUsername = username?.trim();
        if (trimmedUsername && trimmedUsername.startsWith("username_")) {
          usernamesToDeactivate.push(trimmedUsername);
        }
      }

      for (const username of usernamesToDeactivate) {
        await settingsTeamPage.clickMemberActionsMenu(username);
        await settingsTeamPage.clickDeactivate();
        await settingsTeamPage.clickConfirmDeactivation();
        await settingsTeamPage.validateMemberNotInList(username);
      }
    } finally {
      await context.close();
    }
  });

  test("Add team member", async ({ settingsPage, settingsTeamPage, commonSteps }) => {
    await test.step("Log in as admin", async () => {
      await commonSteps.loginAsAdmin();
    });

    await test.step("Navigate to Team Settings", async () => {
      await settingsPage.navigateToTeamSettings();
      await settingsTeamPage.validateTeamSettingsPageOpened();
    });

    const username = generateRandomUsername();

    await test.step("Add a new team member", async () => {
      await settingsTeamPage.clickAddTeamMember();
      await settingsTeamPage.inputMemberUsername(username);
      await settingsTeamPage.clickSaveTeamMember();
    });

    await test.step("Validate member was added", async () => {
      await settingsTeamPage.validateMemberAdded();
      await settingsTeamPage.validateCopyPasswordButtonVisible();
      await settingsTeamPage.clickDone();
    });

    await test.step("Validate member appears in list with correct role and login status", async () => {
      await settingsTeamPage.validateMemberRole(username, "Admin");
      await settingsTeamPage.validateMemberLastLogin(username, "Never");
    });
  });

  test("New member log in", async ({ authPage, settingsPage, settingsTeamPage, commonSteps }) => {
    let username = generateRandomUsername();
    let tempPassword: string;

    await test.step("Log in as admin and navigate to team settings", async () => {
      await commonSteps.loginAsAdmin();
      await settingsPage.navigateToTeamSettings();
      await settingsTeamPage.validateTeamSettingsPageOpened();
    });

    await test.step("Add a new team member", async () => {
      await settingsTeamPage.clickAddTeamMember();
      await settingsTeamPage.inputMemberUsername(username);
      await settingsTeamPage.clickSaveTeamMember();
      await settingsTeamPage.validateMemberAdded();
      tempPassword = await settingsTeamPage.getTemporaryPassword();
      await settingsTeamPage.clickDone();
      await settingsTeamPage.validateMemberVisible(username);
    });

    await test.step("Log out as admin", async () => {
      await authPage.logout();
      await authPage.validateRedirectedToAuth();
    });

    await test.step("Log in as new member with temporary password", async () => {
      await authPage.inputUsername(username);
      await authPage.inputPassword(tempPassword);
      await authPage.clickLogin();
    });

    await test.step("Set new password", async () => {
      await authPage.inputNewPassword("Password123!");
      await authPage.inputConfirmPassword("Password123!");
      await authPage.clickContinue();
      await authPage.clickLoginButton();
      await authPage.validateLoggedIn();
    });

    await test.step("Verify no admin rights", async () => {
      await settingsPage.navigateToTeamSettings();
      await settingsTeamPage.validateTeamSettingsPageOpened();
      await settingsTeamPage.validateNoAdminRights();
    });
  });

  test("New member password reset", async ({ authPage, settingsPage, settingsTeamPage, commonSteps }) => {
    let username = generateRandomUsername();
    let tempPassword1: string;
    let tempPassword2: string;

    await commonSteps.loginAsAdmin();

    await test.step("Navigate to team settings", async () => {
      await settingsPage.navigateToTeamSettings();
      await settingsTeamPage.validateTeamSettingsPageOpened();
    });

    await test.step("Add team member", async () => {
      await settingsTeamPage.clickAddTeamMember();
      await settingsTeamPage.inputMemberUsername(username);
      await settingsTeamPage.clickSaveTeamMember();
      await settingsTeamPage.validateMemberAdded();
      tempPassword1 = await settingsTeamPage.getTemporaryPassword();
      await settingsTeamPage.clickDone();
    });

    await test.step("Reset member password", async () => {
      await settingsTeamPage.clickMemberActionsMenu(username);
      await settingsTeamPage.clickResetPassword();
      await settingsTeamPage.clickResetMemberPasswordConfirm();
      await settingsTeamPage.validatePasswordReset();
      tempPassword2 = await settingsTeamPage.getTemporaryPassword();
      await settingsTeamPage.clickDone();
    });

    await test.step("Log out as admin", async () => {
      await authPage.logout();
      await authPage.validateRedirectedToAuth();
    });

    await test.step("Attempt login with initial (wrong) temp password", async () => {
      await authPage.inputUsername(username);
      await authPage.clickPasswordVisibilityToggle();
      await authPage.inputPassword(tempPassword1);
      await authPage.clickLogin();
      await authPage.validateInvalidCredentials();
    });

    await test.step("Log in with new temp password", async () => {
      await authPage.inputUsername(username);
      await authPage.inputPassword(tempPassword2);
      await authPage.clickLogin();
      await authPage.validateUpdatePasswordTitle();
    });

    await test.step("Set new password", async () => {
      await authPage.inputNewPassword("Password123!");
      await authPage.inputConfirmPassword("Password123!");
      await authPage.clickContinue();
      await authPage.validatePasswordSaved();
    });

    await test.step("Complete login", async () => {
      await authPage.clickLoginButton();
      await authPage.validateLoggedIn();
    });

    await test.step("Log out", async () => {
      await authPage.logout();
      await authPage.validateRedirectedToAuth();
    });
  });

  test("Deactivate team member", async ({ authPage, settingsPage, settingsTeamPage, commonSteps }) => {
    let username = generateRandomUsername();
    let tempPassword: string;

    await commonSteps.loginAsAdmin();

    await test.step("Navigate to team settings", async () => {
      await settingsPage.navigateToTeamSettings();
      await settingsTeamPage.validateTeamSettingsPageOpened();
    });

    await test.step("Add team member", async () => {
      await settingsTeamPage.clickAddTeamMember();
      await settingsTeamPage.inputMemberUsername(username);
      await settingsTeamPage.clickSaveTeamMember();
      await settingsTeamPage.validateMemberAdded();
      tempPassword = await settingsTeamPage.getTemporaryPassword();
      await settingsTeamPage.clickDone();
    });

    await test.step("Deactivate the newly added team member", async () => {
      await settingsTeamPage.clickMemberActionsMenu(username);
      await settingsTeamPage.clickDeactivate();
      await settingsTeamPage.clickConfirmDeactivation();
      await settingsTeamPage.validateMemberDeactivatedMessage(username);
      await settingsTeamPage.validateMemberNotInList(username);
    });

    await test.step("Log out as admin", async () => {
      await authPage.logout();
      await authPage.validateRedirectedToAuth();
    });

    await test.step("Attempt login with temp password", async () => {
      await authPage.inputUsername(username);
      await authPage.clickPasswordVisibilityToggle();
      await authPage.inputPassword(tempPassword);
      await authPage.clickLogin();
      await authPage.validateInvalidCredentials();
    });
  });
});
