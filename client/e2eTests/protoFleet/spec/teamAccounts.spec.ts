import { testConfig } from "../config/test.config";
import { test } from "../fixtures/pageFixtures";
import { CommonSteps } from "../helpers/commonSteps";
import { joinTeamInFreshContext, MEMBER_PASSWORD } from "../helpers/rbacTestSetup";
import { generateRandomText, generateRandomUsername } from "../helpers/testDataHelper";
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

  test(
    "Invite, revoke and delete a team invitation",
    { tag: "@smoke" },
    async ({ settingsPage, settingsTeamPage, commonSteps }) => {
      // A role owned by this test keeps its invitation apart from other runs' rows.
      const roleName = generateRandomText("invitation_role");

      await test.step("Log in as admin", async () => {
        await commonSteps.loginAsAdmin();
      });

      await test.step("Navigate to Team Settings and create a role for this test", async () => {
        await settingsPage.navigateToTeamSettings();
        await settingsTeamPage.validateTeamSettingsPageOpened();
        await settingsTeamPage.openRolesTab();
        await settingsTeamPage.createCustomRole(roleName, "Scopes the invitation smoke test.", ["pool:read"]);
      });

      try {
        await test.step("Create an invitation and show its code once", async () => {
          await settingsTeamPage.openInvitationsTab();
          await settingsTeamPage.createInvitationAndGetCode(roleName);
          await settingsTeamPage.validateInvitationCount(roleName, "Pending", 1);
        });

        await test.step("Revoke the pending invitation", async () => {
          await settingsTeamPage.revokeInvitation(roleName);
          await settingsTeamPage.validateInvitationCount(roleName, "Pending", 0);
          await settingsTeamPage.validateInvitationCount(roleName, "Revoked", 1);
        });

        await test.step("Delete the revoked invitation", async () => {
          await settingsTeamPage.deleteInvitation(roleName, "Revoked");
          await settingsTeamPage.validateNoInvitationsForRole(roleName);
        });
      } finally {
        await test.step("CLEANUP: Remove the test role and any invitation left for it", async () => {
          await settingsTeamPage.removeInvitationForRole(roleName);
          await settingsTeamPage.deleteCustomRole(roleName);
        });
      }
    },
  );

  test("Edit team member role", async ({ browser, settingsPage, settingsTeamPage, commonSteps }) => {
    const username = generateRandomUsername();

    await test.step("Log in as admin", async () => {
      await commonSteps.loginAsAdmin();
    });

    await test.step("Invite a Field Tech member to promote", async () => {
      await settingsPage.navigateToTeamSettings();
      await settingsTeamPage.validateTeamSettingsPageOpened();
      const invitationCode = await settingsTeamPage.createInvitationAndGetCode("Field Tech");
      await joinTeamInFreshContext(browser, test.info(), { invitationCode, username });
      await settingsTeamPage.reloadPage();
      await settingsTeamPage.openMembersTab();
      await settingsTeamPage.validateMemberRole(username, "Field Tech");
    });

    await test.step("Promote the member to Admin via Edit role", async () => {
      await settingsTeamPage.clickMemberActionsMenu(username);
      await settingsTeamPage.clickEditRole();
      await settingsTeamPage.selectEditedRole("Admin");
      await settingsTeamPage.clickSaveEditedRole();
    });

    await test.step("Member row reflects the new role", async () => {
      await settingsTeamPage.validateMemberRole(username, "Admin");
    });
  });

  test(
    "New member joins with an invitation code",
    { tag: "@smoke" },
    async ({ authPage, settingsPage, settingsTeamPage, commonSteps }) => {
      const username = generateRandomUsername();
      let invitationCode: string;

      await test.step("Log in as admin and create a Field Tech invitation", async () => {
        await commonSteps.loginAsAdmin();
        await settingsPage.navigateToTeamSettings();
        await settingsTeamPage.validateTeamSettingsPageOpened();
        invitationCode = await settingsTeamPage.createInvitationAndGetCode("Field Tech");
      });

      await test.step("Log out as admin", async () => {
        await authPage.logout();
        await authPage.validateRedirectedToAuth();
      });

      await test.step("Enter the code from the login page", async () => {
        await authPage.openInvitationEntry();
        await authPage.enterInvitationCode(invitationCode.toLowerCase().replaceAll("-", " "));
        await authPage.validateTeamInvitationRole("Field Tech");
      });

      await test.step("Create a login and join", async () => {
        await authPage.registerInvitedLogin(username, MEMBER_PASSWORD);
        await authPage.validateLoggedIn();
      });

      await test.step("Verify no admin rights", async () => {
        // Field Tech doesn't hold user:read, so the Team submenu link is
        // hidden in Settings → secondary nav (gated in navItems.ts).
        await settingsPage.navigateToSettingsPage();
        await settingsTeamPage.validateTeamSubmenuHidden();
      });
    },
  );

  test("New member password reset", async ({ authPage, browser, settingsPage, settingsTeamPage, commonSteps }) => {
    const username = generateRandomUsername();
    let temporaryPassword: string;

    await commonSteps.loginAsAdmin();

    await test.step("Invite a team member who joins with their own password", async () => {
      await settingsPage.navigateToTeamSettings();
      await settingsTeamPage.validateTeamSettingsPageOpened();
      const invitationCode = await settingsTeamPage.createInvitationAndGetCode("Field Tech");
      await joinTeamInFreshContext(browser, test.info(), { invitationCode, username });
      await settingsTeamPage.reloadPage();
      await settingsTeamPage.openMembersTab();
      await settingsTeamPage.validateMemberVisible(username);
    });

    await test.step("Reset member password", async () => {
      await settingsTeamPage.clickMemberActionsMenu(username);
      await settingsTeamPage.clickResetPassword();
      await settingsTeamPage.clickResetMemberPasswordConfirm();
      await settingsTeamPage.validatePasswordReset();
      temporaryPassword = await settingsTeamPage.getTemporaryPassword();
      await settingsTeamPage.clickDone();
    });

    await test.step("Log out as admin", async () => {
      await authPage.logout();
      await authPage.validateRedirectedToAuth();
    });

    await test.step("Attempt login with the member's original password", async () => {
      await authPage.inputUsername(username);
      await authPage.clickPasswordVisibilityToggle();
      await authPage.inputPassword(MEMBER_PASSWORD);
      await authPage.clickLogin();
      await authPage.validateInvalidCredentials();
    });

    await test.step("Log in with the temporary password", async () => {
      await authPage.inputUsername(username);
      await authPage.inputPassword(temporaryPassword);
      await authPage.clickLogin();
      await authPage.validateUpdatePasswordTitle();
    });

    await test.step("Set new password", async () => {
      await authPage.inputNewPassword("Password456!");
      await authPage.inputConfirmPassword("Password456!");
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

  test("Deactivate team member", async ({ authPage, browser, settingsPage, settingsTeamPage, commonSteps }) => {
    const username = generateRandomUsername();

    await commonSteps.loginAsAdmin();

    await test.step("Invite a team member who joins", async () => {
      await settingsPage.navigateToTeamSettings();
      await settingsTeamPage.validateTeamSettingsPageOpened();
      const invitationCode = await settingsTeamPage.createInvitationAndGetCode("Field Tech");
      await joinTeamInFreshContext(browser, test.info(), { invitationCode, username });
      await settingsTeamPage.reloadPage();
      await settingsTeamPage.openMembersTab();
      await settingsTeamPage.validateMemberVisible(username);
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

    await test.step("Attempt login as the deactivated member", async () => {
      await authPage.inputUsername(username);
      await authPage.clickPasswordVisibilityToggle();
      await authPage.inputPassword(MEMBER_PASSWORD);
      await authPage.clickLogin();
      await authPage.validateInvalidCredentials();
    });
  });
});
